// Package connected runs the managed port-forward and the production Argo
// client together against a fake kubectl and a synthetic Argo server. It
// never touches a real cluster: every endpoint is loopback-only.
package connected

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeKubectl installs an executable named kubectl on PATH for one test. The
// script emits the supplied lines in order, one per invocation, so a test can
// stage a failed first attempt followed by a successful one.
//
// The script is POSIX sh, so the suite runs on every machine that runs Go.
//
// Each element of scripts is the body for one invocation. A body that does not
// exit keeps the fake process alive, which is what a healthy forward looks
// like; the manager kills it on Close.
func fakeKubectl(t *testing.T, scripts ...string) (dir string, argsFile string) {
	t.Helper()
	dir = t.TempDir()
	argsFile = filepath.Join(dir, "args.log")
	counter := filepath.Join(dir, "count")
	var cases strings.Builder
	for i, body := range scripts {
		// The last staged body repeats for any further attempt, so a
		// recovery loop does not fall off the end of the script.
		pattern := fmt.Sprintf("%d", i+1)
		if i == len(scripts)-1 {
			pattern = "*"
		}
		cases.WriteString(fmt.Sprintf("%s)\n%s\n;;\n", pattern, body))
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
n=1
if [ -f %q ]; then
    n=$(( $(cat %q) + 1 ))
fi
echo $n > %q
case $n in
%sesac
`, argsFile, counter, counter, counter, cases.String())
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, argsFile
}

// announce is a shell body that prints a kubectl readiness line for port and
// then blocks, imitating a healthy forward.
func announce(port string) string {
	return fmt.Sprintf("echo \"Forwarding from 127.0.0.1:%s -> 2746\"\nwhile true; do sleep 0.05; done", port)
}

// syntheticArgo is a loopback Argo Workflows server that answers every
// request with a pinned workflow and records the requests it receives.
type syntheticArgo struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newSyntheticArgo(t *testing.T) *syntheticArgo {
	t.Helper()
	detail, err := os.ReadFile("../../internal/argo/testdata/workflow_detail.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &syntheticArgo{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(detail)
	}))
	t.Cleanup(s.Close)
	return s
}

// received is every request the server has seen, as "METHOD path".
func (s *syntheticArgo) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}
