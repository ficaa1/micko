package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/ui/profiles"
)

// A probe finds what the server needs: plain HTTP, no token, a token, or
// trust in its certificate, and says why a token or certificate was refused.
func TestProbe(t *testing.T) {
	t.Setenv("GOOD_TOKEN", "good")
	t.Setenv("BAD_TOKEN", "bad")
	t.Setenv("EMPTY_TOKEN", "")
	open := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"metadata":{},"items":[]}`)) }
	tokened := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Error(w, `{"code":16,"message":"token not valid"}`, http.StatusUnauthorized)
			return
		}
		open(w, r)
	}
	forbidden := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":7,"message":"cannot list workflows"}`, http.StatusForbidden)
	}
	cases := []struct {
		name    string
		tls     bool
		handler http.HandlerFunc
		draft   profiles.Draft
		want    profiles.Outcome
		http    bool
		server  bool
		detail  string
	}{
		{"plain http, server auth", false, open, profiles.Draft{}, profiles.Reachable, true, true, ""},
		{"self-signed", true, open, profiles.Draft{}, profiles.Untrusted, false, true, "unknown authority"},
		{"self-signed, trusted", true, open, profiles.Draft{Insecure: true}, profiles.Reachable, false, true, ""},
		{"needs a token", true, tokened, profiles.Draft{Insecure: true}, profiles.NeedsToken, false, true, ""},
		{"good token", true, tokened, profiles.Draft{Insecure: true, TokenEnv: "GOOD_TOKEN"}, profiles.Reachable, false, false, ""},
		{"rejected token", false, tokened, profiles.Draft{TokenEnv: "BAD_TOKEN"}, profiles.NeedsToken, true, false, "rejected the token from $BAD_TOKEN"},
		{"unset token", false, tokened, profiles.Draft{TokenEnv: "EMPTY_TOKEN"}, profiles.NeedsToken, false, false, "$EMPTY_TOKEN is not set"},
		{"forbidden", false, forbidden, profiles.Draft{}, profiles.Failed, true, true, "refused to list workflows in workflows"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(c.handler)
			if c.tls {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			t.Cleanup(srv.Close)
			connector, err := NewConnector(Options{ConfigPath: writeConfig(t, ""), Skins: testSkins})
			if err != nil {
				t.Fatal(err)
			}
			forwards := 0
			connector.forward = func(_ context.Context, cfg config.Config) (string, func(), error) {
				if cfg.Target.Context != "kind-dev" || cfg.Target.Namespace != "argo" || cfg.Target.Service != "argo-server" {
					t.Errorf("forwarded to %+v", cfg.Target)
				}
				forwards++
				return srv.URL, func() { forwards-- }, nil
			}
			d := c.draft
			d.Context, d.Server, d.Namespace = "kind-dev", profiles.Server{Namespace: "argo", Name: "argo-server", Port: 2746}, "workflows"
			got := connector.Probe(context.Background(), d)
			if got.Outcome != c.want || got.Draft.HTTP != c.http || got.Draft.ServerAuth != c.server {
				t.Errorf("Probe = outcome %v http %v server auth %v (%q), want %v %v %v",
					got.Outcome, got.Draft.HTTP, got.Draft.ServerAuth, got.Detail, c.want, c.http, c.server)
			}
			if !strings.Contains(got.Detail, c.detail) || (c.detail == "" && got.Detail != "") {
				t.Errorf("detail = %q, want %q", got.Detail, c.detail)
			}
			if forwards != 0 {
				t.Errorf("%d port-forwards left running", forwards)
			}
		})
	}
}

// Saving appends a profile that loads as a kubectl-forwarded one, keeps the
// file's other profiles and comments, offers it at once and refuses a taken
// name.
func TestSave(t *testing.T) {
	path := writeConfig(t, "# my clusters\nprofiles:\n  dev:\n    server: https://argo.example.com\n    namespace: argo\n    tokenEnv: T\n")
	connector, err := NewConnector(Options{ConfigPath: path, Skins: testSkins})
	if err != nil {
		t.Fatal(err)
	}
	d := profiles.Draft{
		Context: "kind-dev", Server: profiles.Server{Namespace: "argo", Name: "argo-server", Port: 2746},
		Namespace: "workflows", ServerAuth: true, Insecure: true,
	}
	items, err := connector.Save("kind", d)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].Name != "kind" || len(connector.ProfileList().Items) != 2 {
		t.Fatalf("items = %+v, picker list = %+v", items, connector.ProfileList().Items)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# my clusters") {
		t.Errorf("the file's comment was lost:\n%s", data)
	}
	cfg, err := config.Load(data, config.Options{Profile: "kind"})
	if err != nil {
		t.Fatal(err)
	}
	want := config.Target{Context: "kind-dev", Namespace: "argo", Service: "argo-server", RemotePort: 2746}
	if cfg.Target != want || cfg.Namespace != "workflows" || cfg.AuthMode != "server" || !cfg.InsecureSkipTLSVerify || !strings.HasPrefix(cfg.Server, "https://") {
		t.Errorf("saved profile = %+v", cfg)
	}
	if _, err := connector.Save("dev", d); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("saving over dev = %v, want a refusal", err)
	}
}
