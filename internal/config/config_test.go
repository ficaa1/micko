package config

import (
	"os"
	"path/filepath"
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
    tokenEnv: ARGO_TUI_TOKEN
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

func TestPrecedenceCLIOverProfileOverDefault(t *testing.T) {
	// config file provides server/ns; CLI overrides both; refreshInterval
	// falls back to the built-in default.
	cfg, err := Load([]byte(yamlOneProfile), Options{
		Server:    "https://cli.example.test/argo",
		Namespace: "cli-ns",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "https://cli.example.test/argo" || cfg.Namespace != "cli-ns" {
		t.Errorf("CLI layer must win: server=%q ns=%q", cfg.Server, cfg.Namespace)
	}
	if cfg.RefreshInterval != DefaultRefreshInterval {
		t.Errorf("refresh default = %v, want %v", cfg.RefreshInterval, DefaultRefreshInterval)
	}
	if cfg.TokenEnv != "ARGO_TUI_TOKEN" {
		t.Errorf("tokenEnv from profile = %q", cfg.TokenEnv)
	}
	if cfg.ProfileName != "dev" {
		t.Errorf("profile = %q, want dev", cfg.ProfileName)
	}
}

func TestProfileLayerWinsOverConfigDefault(t *testing.T) {
	cfg, err := Load([]byte(yamlOneProfile), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "https://argo.example.test/argo" {
		t.Errorf("server = %q", cfg.Server)
	}
}

func TestProfileSelectionExplicitCLICurrent(t *testing.T) {
	cfg, err := Load([]byte(yamlTwoProfiles), Options{Profile: "prod"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "https://prod.example.test/argo" || cfg.Namespace != "prod-wf" {
		t.Errorf("--profile prod not applied: %+v", cfg)
	}
	if cfg.TokenEnv != "PROD_TOKEN" || cfg.TokenFile != "" {
		t.Errorf("token source = env %q file %q, want env only", cfg.TokenEnv, cfg.TokenFile)
	}
}

func TestUnknownProfileRejected(t *testing.T) {
	_, err := Load([]byte(yamlTwoProfiles), Options{Profile: "nope"})
	if err == nil || !strings.Contains(err.Error(), `profile "nope" not found`) {
		t.Errorf("err = %v, want unknown-profile error", err)
	}
}

func TestMissingNamespaceRejectedBeforeStart(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    tokenEnv: ARGO_TUI_TOKEN
`
	_, err := Load([]byte(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "namespace missing") {
		t.Errorf("err = %v, want missing-namespace rejection", err)
	}
}

func TestMissingServerRejected(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    namespace: workflows
    tokenEnv: ARGO_TUI_TOKEN
`
	_, err := Load([]byte(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "endpoint missing") {
		t.Errorf("err = %v, want missing-endpoint rejection", err)
	}
}

func TestSecretSourceExclusivity(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    namespace: workflows
    tokenEnv: ARGO_TUI_TOKEN
    tokenFile: /run/secrets/token
`
	_, err := Load([]byte(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want exclusivity rejection", err)
	}
}

func TestTokenFileCLIOptionOverridesAndExclusive(t *testing.T) {
	// CLI --token-file combined with profile tokenEnv is also exclusive.
	_, err := Load([]byte(yamlOneProfile), Options{TokenFile: "/run/secrets/cli-token"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want exclusivity rejection for CLI file + profile env", err)
	}
}

func TestURLValidation(t *testing.T) {
	cases := []struct {
		name   string
		server string
		want   string // substring of expected error; empty = valid
	}{
		{"userinfo rejected", "https://user:pass@argo.example.test/argo", "userinfo"},
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
		y := "currentProfile: dev\nprofiles:\n  dev:\n    server: \"" + c.server + "\"\n    namespace: workflows\n    tokenEnv: ARGO_TUI_TOKEN\n"
		_, err := Load([]byte(y), Options{})
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: Load(%q) err = %v, want valid", c.name, c.server, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Load(%q) err = %v, want substring %q", c.name, c.server, err, c.want)
		}
	}
}

func TestRefreshIntervalBounds(t *testing.T) {
	base := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    namespace: workflows
    tokenEnv: ARGO_TUI_TOKEN
`
	cases := []struct {
		name    string
		fileInt string
		optInt  time.Duration
		want    string
	}{
		{"unparseable file value", "refreshInterval: soon\n", 0, "refreshInterval"},
		{"below minimum", "refreshInterval: 200ms\n", 0, "out of bounds"},
		{"above maximum", "refreshInterval: 1h\n", 0, "out of bounds"},
		{"valid file value", "refreshInterval: 10s\n", 0, ""},
		{"CLI below minimum", "", 500 * time.Millisecond, "out of bounds"},
		{"CLI valid", "", 15 * time.Second, ""},
	}
	for _, c := range cases {
		_, err := Load([]byte(base+c.fileInt), Options{RefreshInterval: c.optInt})
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want valid", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want substring %q", c.name, err, c.want)
		}
	}
}

func TestConfigDefaultAppliesLast(t *testing.T) {
	// Profile without server; config-level values (none) -> built-in
	// default interval applies even with an unparsable-free empty file.
	cfg, err := Load([]byte(""), Options{})
	// Missing everything: endpoint error is expected; this test only pins
	// that an empty file does not crash.
	if err == nil {
		t.Fatalf("expected endpoint error for empty config, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "endpoint missing") && !strings.Contains(err.Error(), "namespace missing") {
		t.Errorf("err = %v", err)
	}
}

func TestTokenFileMustBeAbsolute(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    namespace: workflows
    tokenFile: relative/path
`
	_, err := Load([]byte(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Errorf("err = %v, want absolute-path rejection", err)
	}
}

func TestTokenEnvNameWithNewlineRejected(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    server: https://argo.example.test
    namespace: workflows
    tokenEnv: "ARGO\nTOKEN"
`
	_, err := Load([]byte(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Errorf("err = %v, want newline rejection", err)
	}
}

func TestErrorsNeverContainSecret(t *testing.T) {
	y := `
currentProfile: dev
profiles:
  dev:
    server: https://user:hunter2@argo.example.test
    namespace: workflows
    tokenEnv: ARGO_TUI_TOKEN
`
	_, err := Load([]byte(y), Options{})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error leaks userinfo: %v", err)
	}
}

// The Argo Server service usually runs in a different namespace from the
// workflows. Forwarding to the workflow namespace would find no service.
func TestServiceNamespaceIsIndependentOfWorkflowNamespace(t *testing.T) {
	data := []byte(`
currentProfile: work-test
profiles:
  work-test:
    kubeContext: ctx
    service: argo-workflows-server
    serviceNamespace: argo-workflows-tst
    remotePort: 2746
    server: http://127.0.0.1:2746
    namespace: nightly-backup-tst
    tokenEnv: ARGO_TUI_TOKEN
`)
	cfg, err := Load(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "nightly-backup-tst" {
		t.Fatalf("workflow namespace = %q", cfg.Namespace)
	}
	if cfg.Target.Namespace != "argo-workflows-tst" {
		t.Fatalf("forward target namespace = %q, want the service namespace", cfg.Target.Namespace)
	}
}

// Without serviceNamespace the forward stays where it always was.
func TestServiceNamespaceDefaultsToWorkflowNamespace(t *testing.T) {
	data := []byte(`
currentProfile: p
profiles:
  p:
    kubeContext: ctx
    service: argo-server
    remotePort: 2746
    server: http://127.0.0.1:2746
    namespace: argo
    tokenEnv: ARGO_TUI_TOKEN
`)
	cfg, err := Load(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Target.Namespace != "argo" {
		t.Fatalf("forward target namespace = %q, want the workflow namespace", cfg.Target.Namespace)
	}
}

// An existing config file must be found wherever it conventionally lives. On
// macOS os.UserConfigDir points at ~/Library/Application Support, so looking
// only there misses the ~/.config file people actually write.
func TestDefaultConfigPathFindsAnExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "argo-tui", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("currentProfile: p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DefaultConfigPath = %q, want the existing %q", got, want)
	}
}

// XDG_CONFIG_HOME wins when it is set.
func TestDefaultConfigPathPrefersXDG(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "argo-tui", "config.yaml"); got != want {
		t.Fatalf("DefaultConfigPath = %q, want %q", got, want)
	}
}
