// Package connected holds the minimal connected-path regression suite: the
// managed port-forward, the production HTTP adapter and the root action
// gates exercised together against a fake kubectl and a synthetic Argo
// server. It is deliberately small. It is not a general test framework, and
// it never touches a real cluster: every endpoint is loopback-only.
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
// stage a failed first attempt followed by a successful one. The owner's shell is
// fish, so scripts written to disk are fish.
//
// Each element of scripts is the body for one invocation. A body that does not
// exit keeps the fake process alive, which is what a healthy forward looks
// like; the manager kills it on Close.
func fakeKubectl(t *testing.T, scripts ...string) (dir string, argsFile string) {
	t.Helper()
	if _, err := os.Stat("/opt/homebrew/bin/fish"); err != nil {
		t.Skip("fish is not installed; the fake kubectl needs it")
	}
	dir = t.TempDir()
	argsFile = filepath.Join(dir, "args.log")
	counter := filepath.Join(dir, "count")
	var cases strings.Builder
	for i, body := range scripts {
		cases.WriteString(fmt.Sprintf("case %d\n%s\n", i+1, body))
	}
	// The last staged body repeats for any further attempt, so a recovery loop
	// does not fall off the end of the script.
	script := fmt.Sprintf(`#!/usr/bin/env fish
echo $argv >> %q
set -l n 1
if test -f %q
    set n (math (cat %q) + 1)
end
echo $n > %q
switch $n
%send
`, argsFile, counter, counter, counter, cases.String())
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, argsFile
}

// announce is a fish body that prints a kubectl readiness line for port and
// then blocks, imitating a healthy forward.
func announce(port string) string {
	return fmt.Sprintf("echo \"Forwarding from 127.0.0.1:%s -> 2746\"\nwhile true\nsleep 0.05\nend", port)
}

// syntheticArgo is a loopback Argo Workflows server good enough for the read
// and action paths under test. It records every request so a test can prove
// exactly one mutation was sent.
type syntheticArgo struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	// phase is what Get reports; a Stop test flips it to imitate an exit
	// handler that finishes after the action was accepted.
	phase string
	// stopCalls counts accepted PUT .../stop requests.
	stopCalls, resumeCalls int
	// authHeaders records the Authorization header of every request, so a
	// test can prove an empty credential sends no header at all.
	authHeaders []string
}

func newSyntheticArgo(t *testing.T, detail []byte, list []byte) *syntheticArgo {
	return newSynthetic(t, detail, list, false)
}

// newSyntheticArgoTLS serves the same surface over TLS, so a test can prove a
// configured https endpoint is not downgraded when the forward moves.
func newSyntheticArgoTLS(t *testing.T, detail []byte, list []byte) *syntheticArgo {
	return newSynthetic(t, detail, list, true)
}

func newSynthetic(t *testing.T, detail []byte, list []byte, tls bool) *syntheticArgo {
	t.Helper()
	s := &syntheticArgo{phase: "Running"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		s.authHeaders = append(s.authHeaders, r.Header.Get("Authorization"))
		phase := s.phase
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/stop"):
			s.stopCalls++
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/resume"):
			s.resumeCalls++
		}
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Count(strings.Trim(r.URL.Path, "/"), "/") == 3 {
			_, _ = w.Write(withPhase(list, phase))
			return
		}
		_, _ = w.Write(withPhase(detail, phase))
	})
	if tls {
		s.Server = httptest.NewTLSServer(handler)
	} else {
		s.Server = httptest.NewServer(handler)
	}
	t.Cleanup(s.Close)
	return s
}

func (s *syntheticArgo) setPhase(p string) { s.mu.Lock(); s.phase = p; s.mu.Unlock() }
func (s *syntheticArgo) snapshot() (reqs []string, auth []string, stops, resumes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...), append([]string(nil), s.authHeaders...), s.stopCalls, s.resumeCalls
}

// withPhase rewrites the fixture's status phase without a full re-encode, so
// the pinned wire fixtures stay byte-faithful everywhere else.
func withPhase(body []byte, phase string) []byte {
	s := string(body)
	for _, old := range []string{`"phase":"Running"`, `"phase": "Running"`} {
		s = strings.ReplaceAll(s, old, `"phase":"`+phase+`"`)
	}
	return []byte(s)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
