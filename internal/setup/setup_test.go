package setup

import (
	"bytes"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/config"
)

// Setup saves only confirmed valid profiles and retains existing configuration.
func TestRun(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
	defer tlsServer.Close()
	for _, tc := range []struct {
		name, input, existing, wantAuth, wantSource string
		forward, ca, save, wantErr                  bool
	}{
		{name: "direct server auth", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", wantAuth: "server", save: true},
		{name: "forward environment auth", input: "dev\nforward\n\nworkflows\nfixture-context\nargo-system\nargo-server\n2746\ntoken\nenv\nMICKO_SETUP_TOKEN\n\nyes\n", wantAuth: "token", wantSource: "env", forward: true, save: true},
		{name: "token file and CA", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\ntoken\nfile\nTOKEN_PATH\nCA_PATH\nyes\n", wantAuth: "token", wantSource: "file", ca: true, save: true},
		{name: "retain fields and comments", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", existing: "# personal settings\ncurrentProfile: old # selected\nextra: {keep: this} # custom field\nprofiles:\n  old: # existing connection\n    server: https://old.example.test\n    namespace: original\n    tokenEnv: ORIGINAL_TOKEN\n    extraProfileField: retained\n", wantAuth: "server", save: true},
		{name: "duplicate refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", existing: "profiles:\n  dev:\n    server: https://old.example.test\n", wantErr: true},
		{name: "malformed config refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", existing: "profiles: [", wantErr: true},
		{name: "merged profiles refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", existing: "defaults: &defaults\n  dev: {server: https://old.example.test}\nprofiles:\n  <<: *defaults\n", wantErr: true},
		{name: "multiple documents refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n", existing: "profiles: {}\n---\nextra: kept\n", wantErr: true},
		{name: "unsafe endpoint refused", input: "dev\ndirect\nhttp://argo.example.test\nworkflows\nserver\n\nyes\n", wantErr: true},
		{name: "URL credential refused without echo", input: "dev\ndirect\nhttps://user:setup-secret@argo.example.test\nworkflows\nserver\n\nyes\n", wantErr: true},
		{name: "bad environment source", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\ntoken\nenv\nBearer setup-secret\n", wantErr: true},
		{name: "relative token file refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\ntoken\nfile\nrelative-token\n\nyes\n", wantErr: true},
		{name: "bad CA refused", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\nTOKEN_PATH\nyes\n", wantErr: true},
		{name: "incomplete forward refused", input: "dev\nforward\n\nworkflows\n\nargo\nargo-server\n2746\nserver\n\nyes\n", wantErr: true},
		{name: "explicit cancel", input: "dev\n:cancel\n"},
		{name: "confirmation defaults to no", input: "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "private", "config.yaml")
			if tc.existing != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.existing), 0600); err != nil {
					t.Fatal(err)
				}
			}
			token := filepath.Join(dir, "token")
			if err := os.WriteFile(token, []byte("setup-secret"), 0600); err != nil {
				t.Fatal(err)
			}
			ca := filepath.Join(dir, "ca.pem")
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MICKO_SETUP_TOKEN", "setup-secret")
			input := strings.NewReplacer("TOKEN_PATH", token, "CA_PATH", ca).Replace(tc.input)
			var out bytes.Buffer
			err := Run(strings.NewReader(input), &out, path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run error=%v, want error=%v; output=%s", err, tc.wantErr, &out)
			}
			if strings.Contains(out.String(), "setup-secret") || err != nil && strings.Contains(err.Error(), "setup-secret") {
				t.Fatal("setup echoed credential material")
			}
			data, readErr := os.ReadFile(path)
			if !tc.save {
				if tc.existing != "" && string(data) != tc.existing {
					t.Fatalf("existing file changed: %s", data)
				}
				if tc.existing == "" && !os.IsNotExist(readErr) {
					t.Fatalf("unconfirmed/invalid setup created config: %v %s", readErr, data)
				}
				return
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.Contains(string(data), "setup-secret") {
				t.Fatal("saved token value")
			}
			cfg, err := config.Load(data, config.Options{Profile: "dev"})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AuthMode != tc.wantAuth || cfg.Namespace != "workflows" {
				t.Fatalf("unexpected saved config: %+v", cfg)
			}
			if tc.forward && cfg.Target != (config.Target{Context: "fixture-context", Namespace: "argo-system", Service: "argo-server", RemotePort: 2746}) {
				t.Fatalf("forward target=%+v", cfg.Target)
			}
			if tc.wantSource == "env" && cfg.TokenEnv != "MICKO_SETUP_TOKEN" || tc.wantSource == "file" && cfg.TokenFile != token || tc.ca && cfg.CAFile != ca {
				t.Fatal("lost token source or CA")
			}
			if !strings.Contains(out.String(), "--check --profile dev") {
				t.Fatal("missing next step")
			}
			if tc.existing != "" {
				for _, keep := range []string{"# personal settings", "currentProfile: old # selected", "extra: {keep: this} # custom field", "old: # existing connection", "extraProfileField: retained", "tokenEnv: ORIGINAL_TOKEN"} {
					if !strings.Contains(string(data), keep) {
						t.Fatalf("lost %q in %s", keep, data)
					}
				}
			}
			if runtime.GOOS != "windows" {
				for _, entry := range []struct {
					path string
					mode os.FileMode
				}{{path, 0600}, {filepath.Dir(path), 0700}} {
					st, err := os.Stat(entry.path)
					if err != nil {
						t.Fatal(err)
					}
					if st.Mode().Perm() != entry.mode {
						t.Fatalf("%s permissions=%o, want %o", entry.path, st.Mode().Perm(), entry.mode)
					}
				}
			}
		})
	}
	complete := "dev\ndirect\nhttps://argo.example.test\nworkflows\nserver\n\nyes\n"
	lines := strings.SplitAfter(complete, "\n")
	for i := 0; i < len(lines)-1; i++ {
		t.Run("EOF at prompt "+strconv.Itoa(i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			var out bytes.Buffer
			if err := Run(strings.NewReader(strings.Join(lines[:i], "")), &out, path); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "canceled") {
				t.Fatal("EOF did not cancel")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("EOF wrote config")
			}
		})
	}
}
