package main

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Checks use bounded GETs, classify failures and close transports without printing upstream text.
func TestConnectionCheck(t *testing.T) {
	for _, tc := range []struct {
		name, mode, body, stage                         string
		status                                          int
		tls, ca, hang, dead, redirect, direct, insecure bool
	}{
		{name: "direct CLI server auth", mode: "server", status: 200, body: `{"items":[]}`, stage: "ready", direct: true},
		{name: "server auth", mode: "server", status: 200, body: `{"items":[]}`, stage: "ready"},
		{name: "legacy environment source", mode: "env", status: 200, body: `{"items":[]}`, stage: "ready"},
		{name: "legacy file source", mode: "file", status: 200, body: `{"items":[]}`, stage: "ready"},
		{name: "unauthorized", mode: "env", status: 401, body: `{"message":"check-secret private upstream message"}`, stage: "authentication"},
		{name: "forbidden", mode: "server", status: 403, body: `{"message":"check-secret private upstream message"}`, stage: "permission"},
		{name: "forbidden HTML", mode: "server", status: 403, body: `<html>check-secret private upstream message</html>`, stage: "permission"},
		{name: "login interception", mode: "server", status: 200, body: `<html>check-secret private upstream message</html>`, stage: "authentication"},
		{name: "API error", mode: "server", status: 503, body: `{"message":"check-secret private upstream message"}`, stage: "API"},
		{name: "invalid response", mode: "server", status: 200, body: `check-secret private upstream message`, stage: "API"},
		{name: "redirect refused", mode: "server", status: 302, redirect: true, stage: "API"},
		{name: "TLS untrusted", mode: "server", tls: true, status: 200, body: `{"items":[]}`, stage: "TLS"},
		{name: "explicit insecure TLS", mode: "server", tls: true, insecure: true, status: 200, body: `{"items":[]}`, stage: "ready"},
		{name: "TLS trusted CA", mode: "server", tls: true, ca: true, status: 200, body: `{"items":[]}`, stage: "ready"},
		{name: "timeout", mode: "server", hang: true, stage: "endpoint"},
		{name: "endpoint refused", mode: "server", dead: true, stage: "endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MICKO_CHECK_TOKEN", "check-secret")
			var mu sync.Mutex
			var requests []string
			var badAuth atomic.Bool
			var connections atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
				mu.Unlock()
				want := ""
				if tc.mode != "server" {
					want = "Bearer check-secret"
				}
				if r.Header.Get("Authorization") != want {
					badAuth.Store(true)
				}
				if tc.hang {
					<-r.Context().Done()
					return
				}
				if tc.redirect {
					w.Header().Set("Location", "/must-not-follow")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
				if state == http.StateClosed {
					connections.Add(-1)
				}
			}
			if tc.tls {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			if tc.dead {
				srv.Close()
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			auth := "    authMode: server\n"
			if tc.mode == "env" {
				auth = "    tokenEnv: MICKO_CHECK_TOKEN\n"
			}
			if tc.mode == "file" {
				token := filepath.Join(dir, "token")
				if err := os.WriteFile(token, []byte("check-secret\n"), 0600); err != nil {
					t.Fatal(err)
				}
				auth = "    tokenFile: " + token + "\n"
			}
			if tc.ca {
				ca := filepath.Join(dir, "ca.pem")
				if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
					t.Fatal(err)
				}
				auth += "    caFile: " + ca + "\n"
			}
			configData := "profiles:\n  fixture:\n    server: " + srv.URL + "/argo\n    namespace: workflows\n" + auth
			if tc.direct {
				configData = ""
			}
			if err := os.WriteFile(path, []byte(configData), 0600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			timeout := "2s"
			if tc.hang {
				timeout = "100ms"
			}
			started := time.Now()
			args := []string{"--check", "--config", path, "--check-timeout", timeout}
			if tc.direct {
				args = append(args, "--server", srv.URL+"/argo", "--namespace", "workflows", "--auth-mode", "server")
			} else {
				args = append(args, "--profile", "fixture")
			}
			if tc.insecure {
				args = append(args, "--insecure-skip-tls-verify")
			}
			code := runIO(args, strings.NewReader(""), &out, &errOut)
			wantCode := 1
			if tc.stage == "ready" {
				wantCode = 0
			}
			if code != wantCode || !strings.HasPrefix(out.String(), "check "+tc.stage+":") {
				t.Fatalf("exit=%d, out=%q err=%q; want %d and %s", code, &out, &errOut, wantCode, tc.stage)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("check exceeded its deadline")
			}
			if strings.Contains(out.String()+errOut.String(), "check-secret") || strings.Contains(out.String()+errOut.String(), "private upstream message") {
				t.Fatal("check exposed credential or upstream body")
			}
			if tc.insecure && !strings.Contains(errOut.String(), "TLS certificate verification is disabled") {
				t.Fatal("missing insecure TLS warning")
			}
			if badAuth.Load() {
				t.Fatal("wrong authorization header")
			}
			mu.Lock()
			got := append([]string(nil), requests...)
			mu.Unlock()
			if tc.dead || tc.tls && !tc.ca && !tc.insecure {
				if len(got) != 0 {
					t.Fatalf("unexpected requests: %v", got)
				}
			} else {
				if len(got) != 1 || !strings.HasPrefix(got[0], "GET /argo/api/v1/workflows/workflows?") || !strings.Contains(got[0], "listOptions.limit=1") {
					t.Fatalf("check sent unexpected requests: %v", got)
				}
			}
			deadline := time.Now().Add(time.Second)
			for connections.Load() != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if connections.Load() != 0 {
				t.Fatal("check left transport connections open")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != configData {
				t.Fatal("check changed config")
			}
		})
	}
}
