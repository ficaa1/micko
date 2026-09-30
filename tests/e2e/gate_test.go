//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The gate opens only on MICKO_E2E=1 and a readable allowlist that names a
// safe endpoint, a namespace and the mutation opt-in; otherwise it says why.
func TestOpenGate(t *testing.T) {
	cases := []struct {
		name    string
		flag    string
		file    string
		want    e2eConfig
		wantWhy string
	}{
		{"flag off", "", "server=https://argo.example.test\nnamespace=e2e\nallowActions=true", e2eConfig{}, "MICKO_E2E != 1"},
		{"no allowlist", "1", "", e2eConfig{}, "not found"},
		{"no namespace", "1", "server=https://argo.example.test\nallowActions=true", e2eConfig{}, "server= and namespace= are required"},
		{"plain http off loopback", "1", "server=http://argo.example.test\nnamespace=e2e\nallowActions=true", e2eConfig{}, "HTTPS or loopback HTTP"},
		{"credentials in the URL", "1", "server=https://user:secret@argo.example.test\nnamespace=e2e\nallowActions=true", e2eConfig{}, "without userinfo"},
		{"no mutation opt-in", "1", "server=https://argo.example.test\nnamespace=e2e", e2eConfig{}, "allowActions=true is required"},
		{"loopback http", "1", "server=http://127.0.0.1:2746\nnamespace=e2e\nallowActions=true",
			e2eConfig{Server: "http://127.0.0.1:2746", Namespace: "e2e", AllowActions: true}, ""},
		{"https with a token and comments", "1", "# disposable cluster\n\nserver = https://argo.example.test\nnamespace=e2e\ntoken=t0k\nallowActions=true\n",
			e2eConfig{Server: "https://argo.example.test", Namespace: "e2e", Token: "t0k", AllowActions: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "e2e-config.yaml")
			if c.file != "" {
				if err := os.WriteFile(path, []byte(c.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MICKO_E2E", c.flag)
			t.Setenv("MICKO_E2E_CONFIG", path)
			got, why := openGate()
			if got != c.want {
				t.Errorf("config = %+v, want %+v", got, c.want)
			}
			if (why == "") != (c.wantWhy == "") || !strings.Contains(why, c.wantWhy) {
				t.Errorf("reason = %q, want one containing %q", why, c.wantWhy)
			}
		})
	}
}
