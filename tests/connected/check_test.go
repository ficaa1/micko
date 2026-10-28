//go:build !windows

package connected

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/session"
)

// Checks share managed forwarding, preserve base paths and close the child on success and failure.
func TestManagedConnectionCheck(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
		status      int
		ready       bool
	}{
		{"ready", "ready", 200, true},
		{"unauthorized", "authentication", 401, true},
		{"forbidden", "permission", 403, true},
		{"forward readiness timeout", "forwarding", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/prefix/api/v1/workflows/workflows" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				reads.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"items":[]}`)
			}))
			defer srv.Close()
			script := "while true; do sleep 0.05; done"
			if tc.ready {
				script = announce(portOf(t, srv.URL))
			}
			dir, argsFile := fakeKubectl(t, "echo $$ > \"$0.pid\"\n"+script)
			path := filepath.Join(t.TempDir(), "config.yaml")
			body := "profiles:\n  fixture:\n    server: http://127.0.0.1:1/prefix\n    namespace: workflows\n    authMode: server\n    kubeContext: fixture-only\n    serviceNamespace: argo-system\n    service: argo-server\n    remotePort: 2746\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			connector, err := session.NewConnector(session.Options{ConfigPath: path})
			if err != nil {
				t.Fatal(err)
			}
			defer connector.Close()
			result := connector.Check(context.Background(), "fixture", 300*time.Millisecond)
			if result.Stage != tc.stage || result.OK != (tc.stage == "ready") {
				t.Fatalf("Check=%+v, want %s", result, tc.stage)
			}
			if tc.ready && reads.Load() != 1 || !tc.ready && reads.Load() != 0 {
				t.Fatalf("reads=%d", reads.Load())
			}
			args, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"--context fixture-only", "--namespace argo-system", "service/argo-server", "0:2746"} {
				if !strings.Contains(string(args), want) {
					t.Fatalf("kubectl args lack %q: %s", want, args)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "kubectl.pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "check to reap kubectl", func() bool { return syscall.Kill(pid, 0) != nil })
		})
	}
}
