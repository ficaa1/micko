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
	if cfg.TokenEnv != "MICKO_TOKEN" {
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
    tokenEnv: MICKO_TOKEN
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
    tokenEnv: MICKO_TOKEN
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
    tokenEnv: MICKO_TOKEN
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
		y := "currentProfile: dev\nprofiles:\n  dev:\n    server: \"" + c.server + "\"\n    namespace: workflows\n    tokenEnv: MICKO_TOKEN\n"
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
    tokenEnv: MICKO_TOKEN
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
    tokenEnv: MICKO_TOKEN
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
    tokenEnv: MICKO_TOKEN
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
    tokenEnv: MICKO_TOKEN
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
	want := filepath.Join(home, ".config", "micko", "config.yaml")
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

// A config file written under argo-tui/, the tool's former name, is still
// found. A micko/ file beats it wherever the two live.
func TestDefaultConfigPathFindsALegacyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	legacy := filepath.Join(home, ".config", "argo-tui", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("currentProfile: p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := DefaultConfigPath(); err != nil || got != legacy {
		t.Fatalf("DefaultConfigPath = %q, %v; want the legacy %q", got, err, legacy)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	current := filepath.Join(xdg, "micko", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("currentProfile: p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := DefaultConfigPath(); err != nil || got != current {
		t.Fatalf("DefaultConfigPath = %q, %v; want the micko file %q", got, err, current)
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
	if want := filepath.Join(xdg, "micko", "config.yaml"); got != want {
		t.Fatalf("DefaultConfigPath = %q, want %q", got, want)
	}
}

// The picker lists every profile the file names, sorted, so the order on
// screen does not depend on Go's map iteration.
func TestListProfilesIsSortedAndNamesCurrent(t *testing.T) {
	y := `currentProfile: prod
profiles:
  prod:
    server: https://prod.example.com
    namespace: argo
  dev:
    server: https://dev.example.com
    namespace: workflows
`
	got, current, err := ListProfiles([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if current != "prod" {
		t.Errorf("current = %q, want prod", current)
	}
	if len(got) != 2 || got[0].Name != "dev" || got[1].Name != "prod" {
		t.Fatalf("profiles = %+v, want dev then prod", got)
	}
	if got[0].Server != "https://dev.example.com" || got[0].Namespace != "workflows" {
		t.Errorf("dev = %+v, want the server and namespace from the file", got[0])
	}
}

// A profile that Load would reject is still listed. The reader needs to see
// the name to learn that choosing it fails, and one broken profile must not
// hide the good ones.
func TestListProfilesKeepsAProfileLoadWouldReject(t *testing.T) {
	y := `profiles:
  broken:
    namespace: argo
  good:
    server: https://argo.example.com
    namespace: argo
    tokenEnv: ARGO_TOKEN
`
	got, _, err := ListProfiles([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("profiles = %+v, want both listed", got)
	}
	if _, err := Load([]byte(y), Options{Profile: "broken"}); err == nil {
		t.Error("Load accepted the profile the picker only lists")
	}
}

// No config file is not an error. The picker opens empty and says where a file
// should go, which is more use than a refusal to start.
func TestListProfilesAcceptsAnEmptyFile(t *testing.T) {
	got, current, err := ListProfiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || current != "" {
		t.Errorf("ListProfiles(nil) = %+v, %q; want nothing", got, current)
	}
}

// The journal is on unless the file turns it off. A missing key, a missing
// file and a file that does not parse all leave it on.
func TestJournalIsOnUnlessTurnedOff(t *testing.T) {
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
		if got := JournalEnabled([]byte(tc.data)); got != tc.want {
			t.Errorf("%s: JournalEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Micko is off unless the file turns him on. A missing file and a file that
// does not parse leave him off, and the key must not trip profile loading.
func TestMickoIsOffUnlessTurnedOn(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"no file", "", false},
		{"key absent", "currentProfile: dev\n", false},
		{"turned on", "mascot: true\n", true},
		{"turned off", "mascot: false\n", false},
		{"broken file", "mascot: [\n", false},
	}
	for _, tc := range cases {
		if got := FileMascot([]byte(tc.data)); got != tc.want {
			t.Errorf("%s: FileMascot = %v, want %v", tc.name, got, tc.want)
		}
	}
	data := []byte("mascot: true\ncurrentProfile: dev\nprofiles:\n  dev:\n    server: https://argo.example.com\n    namespace: ns\n    tokenEnv: MICKO_TOKEN_MASCOT\n")
	if _, err := Load(data, Options{}); err != nil {
		t.Fatalf("Load with mascot set: %v", err)
	}
}

// The journal key is a top-level setting and must not trip profile loading.
func TestJournalKeyLoadsAlongsideProfiles(t *testing.T) {
	data := []byte("journal: false\ncurrentProfile: dev\nprofiles:\n  dev:\n    server: https://argo.example.com\n    namespace: ns\n    tokenEnv: MICKO_TOKEN_JOURNAL\n")
	if _, err := Load(data, Options{}); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// Values are shown unless the file, the chosen profile or the flag asks for
// redaction. A profile's own setting beats the top-level one in both
// directions, and the flag can only turn redaction on.
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
		cfg, err := Load([]byte(c.yaml), Options{RedactValues: c.flag})
		if err != nil {
			t.Fatalf("%s: Load: %v", c.name, err)
		}
		if cfg.RedactValues != c.want {
			t.Errorf("%s: RedactValues = %v, want %v", c.name, cfg.RedactValues, c.want)
		}
	}
	demo, err := Load(nil, Options{Demo: true, RedactValues: true})
	if err != nil || !demo.RedactValues {
		t.Errorf("demo with --redact-values: RedactValues = %v, err = %v", demo.RedactValues, err)
	}
}
