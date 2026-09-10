package connected

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"argo-tui/internal/argo"
	"argo-tui/internal/core"
	"argo-tui/internal/portforward"
)

func newManager(t *testing.T) *portforward.Manager {
	t.Helper()
	m, err := portforward.New(portforward.Target{Context: "test-ctx", Namespace: "argo-workflows-tst", Service: "argo-workflows-server", RemotePort: "2746"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func portOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

// waitFor polls cond until it holds or the deadline passes. Forwarding is
// asynchronous, so a test cannot assert on it synchronously.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A busy local port must never be contacted. The forward asks kubectl for an
// ephemeral port, and the endpoint comes only from the readiness line of the
// process we own. A first attempt that loses a port race must not fail
// startup either: readiness has to survive into the next attempt.
func TestStartupUsesOnlyTheAnnouncedPortAndSurvivesAPortCollision(t *testing.T) {
	argoSrv := newSyntheticArgo(t, mustRead(t, "../../internal/argo/testdata/workflow_detail.json"), mustRead(t, "../../internal/argo/testdata/list_page1.json"))
	realPort := portOf(t, argoSrv.URL)

	_, argsFile := fakeKubectl(t,
		"echo \"Unable to listen on port 2746: bind: address already in use\" >&2\nexit 1",
		announce(realPort),
	)

	m := newManager(t)
	m.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Ready(ctx); err != nil {
		t.Fatalf("readiness did not survive the failed first attempt: %v", err)
	}
	got := m.Endpoint()
	if want := "http://127.0.0.1:" + realPort; got != want {
		t.Fatalf("endpoint = %q, want the announced %q", got, want)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "0:2746") {
		t.Fatalf("kubectl was not asked for an ephemeral local port; args were:\n%s", args)
	}
	for _, guess := range []string{"2746:2746", "8080:2746"} {
		if strings.Contains(string(args), guess) {
			t.Fatalf("kubectl was asked for a guessed local port %q; args were:\n%s", guess, args)
		}
	}
}

// Every recovery binds a new ephemeral port. A client that captured the first
// endpoint would keep talking to a dead port forever, so reconnecting would
// never restore service.
func TestReconnectMovesTheClientToTheNewPort(t *testing.T) {
	detail := mustRead(t, "../../internal/argo/testdata/workflow_detail.json")
	list := mustRead(t, "../../internal/argo/testdata/list_page1.json")
	first := newSyntheticArgo(t, detail, list)
	second := newSyntheticArgo(t, detail, list)

	// The first attempt announces the first server, then exits, imitating a
	// forward that dies. The second attempt announces the second server.
	_, _ = fakeKubectl(t,
		"echo \"Forwarding from 127.0.0.1:"+portOf(t, first.URL)+" -> 2746\"\nsleep 0.4\nexit 1",
		announce(portOf(t, second.URL)),
	)

	m := newManager(t)
	m.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Ready(ctx); err != nil {
		t.Fatal(err)
	}

	client, err := argo.NewClient(argo.Options{
		Server:        m.Endpoint(),
		TokenFn:       func() (string, error) { return "", nil },
		ResolveServer: m.Endpoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := core.Ref{Namespace: "team-a", Name: "dag-complex", UID: "wf-uid-111"}
	if _, err := client.Get(ctx, ref); err != nil {
		t.Fatalf("first read before the loss failed: %v", err)
	}

	firstEndpoint := m.Endpoint()
	waitFor(t, "the forward to recover on a new port", func() bool {
		e := m.Endpoint()
		return e != "" && e != firstEndpoint
	})

	if _, err := client.Get(ctx, ref); err != nil {
		t.Fatalf("read after recovery failed; the client did not follow the new port: %v", err)
	}
	if reqs, _, _, _ := first.snapshot(); len(reqs) != 1 {
		t.Fatalf("first server saw %d requests, want exactly the one before the loss: %v", len(reqs), reqs)
	}
	if reqs, _, _, _ := second.snapshot(); len(reqs) != 1 {
		t.Fatalf("recovered server saw %d requests, want exactly one: %v", len(reqs), reqs)
	}
}

// Recovery may move the host and port. It must never move the scheme, drop a
// configured base path, or discard the TLS material that goes with them.
func TestRecoveryPreservesConfiguredSchemeAndPathPrefix(t *testing.T) {
	srv := newSyntheticArgoTLS(t, mustRead(t, "../../internal/argo/testdata/workflow_detail.json"), mustRead(t, "../../internal/argo/testdata/list_page1.json"))
	host := strings.TrimPrefix(srv.URL, "https://")

	client, err := argo.NewClient(argo.Options{
		Server:                "https://argo.example.invalid/argo",
		TokenFn:               func() (string, error) { return "", nil },
		InsecureSkipTLSVerify: true,
		// A recovered forward announces a plain-http loopback address. Only
		// its host and port may be adopted.
		ResolveServer: func() string { return "http://" + host },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Get(ctx, core.Ref{Namespace: "team-a", Name: "dag-complex", UID: "wf-uid-111"}); err != nil {
		t.Fatalf("request over the preserved https scheme failed: %v", err)
	}
	reqs, auth, _, _ := srv.snapshot()
	if len(reqs) != 1 || !strings.HasPrefix(reqs[0], "GET /argo/api/v1/workflows/") {
		t.Fatalf("configured path prefix was not preserved: %v", reqs)
	}
	// An empty credential must send no Authorization header at all, which is
	// what an Argo server run with --auth-mode=server expects.
	if len(auth) != 1 || auth[0] != "" {
		t.Fatalf("empty token still sent an Authorization header: %q", auth)
	}
}
