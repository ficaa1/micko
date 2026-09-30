package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const yamlOneProfile = `
currentProfile: dev
refreshInterval: 5s
profiles:
  dev:
    server: https://argo.example.test/argo
    namespace: workflows
    tokenEnv: MICKO_TOKEN
`

const yamlTwoProfiles = `
currentProfile: dev
profiles:
  dev:
    server: https://dev.example.test/argo
    namespace: workflows
    tokenFile: /run/secrets/argo-dev-token
  prod:
    server: https://prod.example.test/argo
    namespace: prod-wf
    tokenEnv: PROD_TOKEN
`

// Server URLs reject unsafe transports and credential forms without exposing their values.
func TestLoadServerURLs(t *testing.T) {
	cases := []struct {
		name   string
		server string
		want   string // substring of expected error; empty = valid
	}{
		{"userinfo rejected", "https://user:hunter2@argo.example.test/argo", "userinfo"},
		{"ftp scheme rejected", "ftp://argo.example.test", "scheme"},
		{"no host", "https:///argo", "host"},
		{"credential query rejected", "https://argo.example.test/?token=abc123", "credential"},
		{"password query rejected", "https://argo.example.test/?password=hunter2", "credential"},
		{"plain http non-loopback rejected", "http://argo.example.test", "plain HTTP"},
		{"valid https with base path", "https://argo.example.test/argo", ""},
		{"loopback http allowed", "http://127.0.0.1:2746", ""},
		{"localhost http allowed", "http://localhost:2746", ""},
		{"ipv6 loopback http allowed", "http://[::1]:2746", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			y := "currentProfile: dev\nprofiles:\n  dev:\n    server: \"" + c.server + "\"\n    namespace: workflows\n    tokenEnv: MICKO_TOKEN\n"
			_, err := Load([]byte(y), Options{})
			if c.want == "" {
				if err != nil {
					t.Errorf("%s: Load(%q) err = %v, want valid", c.name, c.server, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: Load(%q) err = %v, want substring %q", c.name, c.server, err, c.want)
			}
			if err != nil && (strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "abc123")) {
				t.Fatalf("error exposes credentials: %v", err)
			}
		})
	}
}

// Polling uses CLI, file or built-in defaults and rejects malformed or out-of-bounds durations.
func TestLoadRefreshInterval(t *testing.T) {
	base := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    namespace: workflows
    tokenEnv: MICKO_TOKEN
`
	cases := []struct {
		name     string
		fileInt  string
		optInt   time.Duration
		want     string
		duration time.Duration
	}{
		{"unparseable file value", "refreshInterval: soon\n", 0, "refreshInterval", 0},
		{"below minimum", "refreshInterval: 200ms\n", 0, "out of bounds", 0},
		{"above maximum", "refreshInterval: 1h\n", 0, "out of bounds", 0},
		{"valid file value", "refreshInterval: 10s\n", 0, "", 10 * time.Second},
		{"CLI below minimum", "", 500 * time.Millisecond, "out of bounds", 0},
		{"CLI valid", "", 15 * time.Second, "", 15 * time.Second},
		{"built-in default", "", 0, "", 5 * time.Second},
		{"CLI overrides file", "refreshInterval: 10s\n", 15 * time.Second, "", 15 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Load([]byte(base+c.fileInt), Options{RefreshInterval: c.optInt})
			if c.want == "" {
				if err != nil {
					t.Errorf("%s: err = %v, want valid", c.name, err)
				}
				if cfg.RefreshInterval != c.duration {
					t.Errorf("%s: refreshInterval = %v, want %v", c.name, cfg.RefreshInterval, c.duration)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, want substring %q", c.name, err, c.want)
			}
		})
	}
}

// JournalEnabled defaults on unless the file explicitly disables the journal.
func TestJournalEnabled(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"no file", "", true},
		{"key absent", "currentProfile: dev\n", true},
		{"turned on", "journal: true\n", true},
		{"turned off", "journal: false\nprofiles: {}\n", false},
		{"broken file", "journal: [\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := JournalEnabled([]byte(tc.data)); got != tc.want {
				t.Errorf("%s: JournalEnabled = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	if _, err := Load([]byte("journal: false\n"+yamlOneProfile), Options{}); err != nil {
		t.Fatalf("Load with journal key: %v", err)
	}
}

// FileMascot defaults off; Load accepts known positions and rejects unknown ones.
func TestFileMascot(t *testing.T) {
	cases := []struct {
		name string
		data string
		want Mascot
	}{
		{"no file", "", MascotOff},
		{"key absent", "currentProfile: dev\n", MascotOff},
		{"turned on", "mascot: true\n", MascotPerch},
		{"perched", "mascot: perch\n", MascotPerch},
		{"on the floor", "mascot: floor\n", MascotFloor},
		{"turned off", "mascot: false\n", MascotOff},
		{"unknown spot", "mascot: ceiling\n", MascotOff},
		{"broken file", "mascot: [\n", MascotOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FileMascot([]byte(tc.data)); got != tc.want {
				t.Errorf("%s: FileMascot = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	data := []byte("mascot: true\ncurrentProfile: dev\nprofiles:\n  dev:\n    server: https://argo.example.com\n    namespace: ns\n    tokenEnv: MICKO_TOKEN_MASCOT\n")
	if _, err := Load(data, Options{}); err != nil {
		t.Fatalf("Load with mascot set: %v", err)
	}
	bad := []byte(strings.Replace(string(data), "mascot: true", "mascot: ceiling", 1))
	if _, err := Load(bad, Options{}); err == nil || !strings.Contains(err.Error(), "floor") {
		t.Fatalf("Load with mascot: ceiling = %v, want an error naming the choices", err)
	}
}

// Profile redaction overrides the file; the flag only enables it, including in demo mode.
func TestRedactValuesLayers(t *testing.T) {
	const top = "redactValues: true\n"
	const optOut = "    redactValues: false\n"
	const optIn = "    redactValues: true\n"
	cases := []struct {
		name string
		yaml string
		flag bool
		want bool
	}{
		{"unset", yamlOneProfile, false, false},
		{"top level", top + yamlOneProfile, false, true},
		{"profile opts in", yamlOneProfile + optIn, false, true},
		{"profile opts out of the top level", top + yamlOneProfile + optOut, false, false},
		{"flag", yamlOneProfile, true, true},
		{"flag beats a profile opt-out", top + yamlOneProfile + optOut, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Load([]byte(c.yaml), Options{RedactValues: c.flag})
			if err != nil {
				t.Fatalf("%s: Load: %v", c.name, err)
			}
			if cfg.RedactValues != c.want {
				t.Errorf("%s: RedactValues = %v, want %v", c.name, cfg.RedactValues, c.want)
			}
		})
	}
	demo, err := Load(nil, Options{Demo: true, RedactValues: true})
	if err != nil || !demo.RedactValues {
		t.Errorf("demo with --redact-values: RedactValues = %v, err = %v", demo.RedactValues, err)
	}
}

// Connection flags override the selected profile; an explicit profile overrides currentProfile.
func TestLoadLayers(t *testing.T) {
	cases := []struct {
		name, data                        string
		opts                              Options
		server, namespace, profile, token string
	}{
		{"profile", yamlOneProfile, Options{}, "https://argo.example.test/argo", "workflows", "dev", "MICKO_TOKEN"},
		{"connection flags", yamlOneProfile, Options{Server: "https://cli.example.test/argo", Namespace: "cli-ns"}, "https://cli.example.test/argo", "cli-ns", "dev", "MICKO_TOKEN"},
		{"profile flag", yamlTwoProfiles, Options{Profile: "prod"}, "https://prod.example.test/argo", "prod-wf", "prod", "PROD_TOKEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Load([]byte(c.data), c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.Server != c.server || got.Namespace != c.namespace || got.ProfileName != c.profile || got.TokenEnv != c.token || got.TokenFile != "" {
				t.Fatalf("connection = %+v, want server=%q namespace=%q profile=%q tokenEnv=%q and no tokenFile", got, c.server, c.namespace, c.profile, c.token)
			}
		})
	}
}

// Invalid profile and credential inputs fail before a session starts.
func TestLoadRejectedInputs(t *testing.T) {
	cases := []struct {
		name, data string
		opts       Options
		want       string
	}{
		{"unknown profile", yamlTwoProfiles, Options{Profile: "nope"}, `profile "nope" not found`},
		{"missing namespace", strings.Replace(yamlOneProfile, "    namespace: workflows\n", "", 1), Options{}, "namespace missing"},
		{"missing server", strings.Replace(yamlOneProfile, "    server: https://argo.example.test/argo\n", "", 1), Options{}, "endpoint missing"},
		{"no file", "", Options{}, "endpoint missing"},
		{"profile secret conflict", yamlOneProfile + "    tokenFile: /run/secrets/token\n", Options{}, "mutually exclusive"},
		{"flag secret conflict", yamlOneProfile, Options{TokenFile: "/run/secrets/cli-token"}, "mutually exclusive"},
		{"relative token file", strings.Replace(yamlOneProfile, "tokenEnv: MICKO_TOKEN", "tokenFile: relative/path", 1), Options{}, "absolute path"},
		{"newline env", strings.Replace(yamlOneProfile, "MICKO_TOKEN", `"ARGO\nTOKEN"`, 1), Options{}, "newline"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load([]byte(c.data), c.opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load error = %v, want %q", err, c.want)
			}
		})
	}
}

// Forwarding targets the service namespace while the session stays in the workflow namespace.
func TestLoadForwardTarget(t *testing.T) {
	for _, c := range []struct{ name, serviceNamespace, want string }{
		{"separate service namespace", "    serviceNamespace: argo-server-ns\n", "argo-server-ns"},
		{"workflow namespace fallback", "", "workflows"},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := yamlOneProfile + "    kubeContext: ctx\n    service: argo-server\n    remotePort: 2746\n" + c.serviceNamespace
			got, err := Load([]byte(data), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Namespace != "workflows" || got.Target != (Target{Context: "ctx", Namespace: c.want, Service: "argo-server", RemotePort: 2746}) {
				t.Fatalf("workflow namespace=%q target=%+v, want workflows and ctx/%s/argo-server:2746", got.Namespace, got.Target, c.want)
			}
		})
	}
}

// Existing conventional files are found; XDG supplies the default when no file exists.
func TestDefaultConfigPath(t *testing.T) {
	for _, c := range []struct {
		name          string
		xdg, existing bool
	}{
		{"existing conventional file", false, true}, {"XDG default", true, false},
		{"existing home file after missing XDG candidate", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			base := filepath.Join(home, ".config")
			if c.xdg {
				xdg := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", xdg)
				if !c.existing {
					base = xdg
				}
			}
			want := filepath.Join(base, "micko", "config.yaml")
			if c.existing {
				if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(want, []byte("currentProfile: p\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := DefaultConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("DefaultConfigPath = %q, want %q", got, want)
			}
		})
	}
}

// The picker lists sorted summaries without validating connections, and accepts an absent file.
func TestListProfiles(t *testing.T) {
	cases := []struct {
		name, data, current string
		want                []ProfileSummary
	}{
		{"sorted profiles", `currentProfile: prod
profiles:
  prod:
    server: https://prod.example.com
    namespace: argo
  dev:
    server: https://dev.example.com
    namespace: workflows
`, "prod", []ProfileSummary{{"dev", "https://dev.example.com", "workflows"}, {"prod", "https://prod.example.com", "argo"}}},
		{"invalid connection remains visible", `profiles:
  broken:
    namespace: argo
  good:
    server: https://argo.example.com
    namespace: argo
    tokenEnv: ARGO_TOKEN
`, "", []ProfileSummary{{"broken", "", "argo"}, {"good", "https://argo.example.com", "argo"}}},
		{"absent file", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, current, err := ListProfiles([]byte(c.data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) || current != c.current {
				t.Fatalf("ListProfiles = %+v, %q; want %+v, %q", got, current, c.want, c.current)
			}
			if c.name == "invalid connection remains visible" {
				if _, err := Load([]byte(c.data), Options{Profile: "broken"}); err == nil {
					t.Fatal("Load accepted a profile missing its server")
				}
			}
		})
	}
}
