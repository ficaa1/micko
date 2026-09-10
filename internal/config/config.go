// Package config loads and validates argo-tui connection configuration
// (plan §3 "Connection and authentication contract").
//
// Precedence: explicit CLI flag > selected profile value > config default;
// defaults apply last. Validation happens before the TUI starts: missing
// endpoint/namespace, ambiguous secret sources, unsafe URLs and out-of-bounds
// durations are rejected with errors that name the offending key — never a
// secret value.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Default values applied last (plan §3 example config).
const (
	DefaultRefreshInterval = 5 * time.Second
	// MinRefreshInterval is the sensible lower bound for polling
	// (plan §5: "configurable with a sensible lower bound").
	MinRefreshInterval = time.Second
	// MaxRefreshInterval guards against effectively-disabled polling.
	MaxRefreshInterval = 10 * time.Minute
)

// Profile is one named server connection. Token material is never stored
// here: tokens come from the environment variable named by TokenEnv or the
// file at TokenFile (mutually exclusive), and are read per request to
// support rotation (plan §3).
type Profile struct {
	KubeContext string `yaml:"kubeContext"`
	Service     string `yaml:"service"`
	// ServiceNamespace is where the Argo Server service runs. It is often a
	// different namespace from the workflows being watched, so it defaults to
	// Namespace only when it is not set.
	ServiceNamespace string   `yaml:"serviceNamespace,omitempty"`
	RemotePort       int      `yaml:"remotePort"`
	Server           string   `yaml:"server"`
	Namespace        string   `yaml:"namespace"`
	TokenEnv         string   `yaml:"tokenEnv,omitempty"`
	TokenFile        string   `yaml:"tokenFile,omitempty"`
	CAFile           string   `yaml:"caFile,omitempty"`
	Namespaces       []string `yaml:"namespaces,omitempty"`
	// WebURL is the browser address of this cluster's Argo UI, for example
	// "https://argo.example.com". It is used only to build a link to open or
	// copy; argo-tui never sends a request to it. Leave it out and the open
	// key says the profile has no web address instead of guessing one, since
	// the forwarded loopback port is not reachable from a browser session
	// that outlives the program.
	WebURL string `yaml:"webURL,omitempty"`
}

// File mirrors the on-disk config (plan §3 example).
type File struct {
	CurrentProfile string             `yaml:"currentProfile"`
	Profiles       map[string]Profile `yaml:"profiles"`
	// RefreshIntervalRaw keeps the raw string so parse errors can name the
	// key ("refreshInterval: ..."), which yaml's own unmarshal errors do not.
	RefreshIntervalRaw string `yaml:"refreshInterval,omitempty"`
}

// refreshInterval parses RefreshIntervalRaw; empty means "not set".
func (f File) refreshInterval() (time.Duration, error) {
	if f.RefreshIntervalRaw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(f.RefreshIntervalRaw)
	if err != nil {
		return 0, fmt.Errorf("config: refreshInterval %q: invalid duration (want e.g. \"5s\", \"500ms\")", f.RefreshIntervalRaw)
	}
	return d, nil
}

// Config is the validated, UI-independent connection configuration
// (plan §4: "Connection validated config is independent of UI, with
// credential-source callbacks injected into A's client").
type Config struct {
	ProfileName string
	Server      string
	// WebURL is the Argo UI address for links; empty means the profile did
	// not configure one.
	WebURL          string
	Namespace       string
	TokenEnv        string
	TokenFile       string
	CAFile          string
	RefreshInterval time.Duration
	// InsecureSkipTLSVerify comes only from an explicit flag/config value;
	// when set, the UI must render a permanent warning (plan §3).
	InsecureSkipTLSVerify bool
	Target                Target
	Debug                 Debug
	Demo                  bool
}

// Target identifies the exact Kubernetes service used by a forwarder.
type Target struct {
	Context, Namespace, Service string
	RemotePort                  int
}

// Debug controls optional sanitized diagnostics.
type Debug struct{ Enabled bool }

// Options carries the explicit CLI layer. Empty fields mean "flag absent".
type Options struct {
	ConfigPath            string
	Profile               string
	Server                string
	Namespace             string
	TokenFile             string
	CAFile                string
	RefreshInterval       time.Duration
	InsecureSkipTLSVerify bool
	Demo                  bool
	Debug                 bool
}

// Load reads, merges and validates configuration. cfgData may be empty
// (no config file). opts carry the CLI layer.
func Load(cfgData []byte, opts Options) (Config, error) {
	var cfg Config

	f := File{}
	if len(cfgData) > 0 {
		if err := yaml.Unmarshal(cfgData, &f); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", displayPath(opts.ConfigPath), err)
		}
	}

	// Profile selection: CLI --profile > currentProfile.
	name := ""
	if opts.Profile != "" {
		name = opts.Profile
	} else if f.CurrentProfile != "" {
		name = f.CurrentProfile
	}
	var prof Profile
	if name != "" {
		p, ok := f.Profiles[name]
		if !ok {
			return Config{}, fmt.Errorf("config: profile %q not found", name)
		}
		prof = p
		cfg.ProfileName = name
	}

	// Merge: CLI > profile > config default > built-in default.
	cfg.Server = firstNonEmpty(opts.Server, prof.Server)
	cfg.WebURL = strings.TrimSuffix(prof.WebURL, "/")
	cfg.Namespace = firstNonEmpty(opts.Namespace, prof.Namespace)
	cfg.TokenEnv = prof.TokenEnv
	cfg.TokenFile = firstNonEmpty(opts.TokenFile, prof.TokenFile)
	cfg.CAFile = firstNonEmpty(opts.CAFile, prof.CAFile)
	cfg.InsecureSkipTLSVerify = opts.InsecureSkipTLSVerify
	targetNS := firstNonEmpty(prof.ServiceNamespace, cfg.Namespace)
	cfg.Target = Target{Context: prof.KubeContext, Namespace: targetNS, Service: prof.Service, RemotePort: prof.RemotePort}
	cfg.Debug = Debug{Enabled: opts.Debug}
	if opts.Demo {
		if name != "" || opts.Server != "" || opts.Namespace != "" || opts.TokenFile != "" || opts.CAFile != "" || opts.InsecureSkipTLSVerify || opts.Debug {
			return Config{}, fmt.Errorf("config: --demo cannot be combined with profile, connection, TLS, or debug options")
		}
		return Config{Demo: true}, nil
	}

	// Secret-source exclusivity (plan §3): tokenEnv and tokenFile together
	// are rejected (CONN-19).
	if cfg.TokenEnv != "" && cfg.TokenFile != "" {
		return Config{}, fmt.Errorf(
			"config: profile %q: tokenEnv and tokenFile are mutually exclusive (tokenEnv=%q, tokenFile path set)",
			cfg.ProfileName, cfg.TokenEnv)
	}

	// Refresh interval: CLI > config default > built-in 5s; bounds checked.
	cfg.RefreshInterval = DefaultRefreshInterval
	if fileInt, err := f.refreshInterval(); err != nil {
		return Config{}, err
	} else if fileInt != 0 {
		cfg.RefreshInterval = fileInt
	}
	if opts.RefreshInterval != 0 {
		cfg.RefreshInterval = opts.RefreshInterval
	}
	if cfg.RefreshInterval < MinRefreshInterval || cfg.RefreshInterval > MaxRefreshInterval {
		return Config{}, fmt.Errorf(
			"config: refreshInterval %s out of bounds [%s, %s]",
			cfg.RefreshInterval, MinRefreshInterval, MaxRefreshInterval)
	}

	if err := cfg.validateURL(); err != nil {
		return Config{}, err
	}
	if cfg.Server == "" {
		return Config{}, fmt.Errorf("config: server endpoint missing (set server in profile %q or --server)", cfg.ProfileName)
	}
	if cfg.Namespace == "" {
		return Config{}, fmt.Errorf("config: namespace missing (set namespace in profile %q or --namespace)", cfg.ProfileName)
	}
	if cfg.ProfileName != "" && (prof.Service != "" || prof.KubeContext != "" || prof.RemotePort != 0) {
		if prof.KubeContext == "" || prof.Service == "" || prof.RemotePort < 1 || prof.RemotePort > 65535 {
			return Config{}, fmt.Errorf("config: profile %q target requires kubeContext, service, and remotePort 1-65535", cfg.ProfileName)
		}
	}
	if cfg.TokenEnv == "" && cfg.TokenFile == "" {
		return Config{}, fmt.Errorf("config: token source missing: set tokenEnv or tokenFile in profile %q", cfg.ProfileName)
	}
	if err := validateTokenSource(cfg.TokenEnv, cfg.TokenFile); err != nil {
		return Config{}, err
	}
	if cfg.CAFile != "" {
		if st, err := os.Stat(cfg.CAFile); err != nil {
			return Config{}, fmt.Errorf("config: caFile %q: %w", cfg.CAFile, err)
		} else if st.IsDir() {
			return Config{}, fmt.Errorf("config: caFile %q is a directory", cfg.CAFile)
		}
	}
	return cfg, nil
}

// validateURL enforces the plan §3 URL contract:
//   - http/https schemes only (CONN-08)
//   - no userinfo
//   - no credential-looking query parameters
//   - non-loopback plain HTTP rejected unless explicitly allowed (CONN-06;
//     loopback HTTP allowed for dev/demo)
func (c Config) validateURL() error {
	if c.Server == "" {
		return nil // missing-endpoint case handled by Load
	}
	u, err := url.Parse(c.Server)
	if err != nil {
		return fmt.Errorf("config: server URL parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: server URL scheme %q rejected: only http/https allowed", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("config: server URL userinfo rejected (user:pass@host form is not allowed)")
	}
	if u.Host == "" {
		return fmt.Errorf("config: server URL host missing")
	}
	for k := range u.Query() {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") || strings.Contains(lk, "password") ||
			strings.Contains(lk, "secret") || lk == "api_key" || lk == "apikey" ||
			lk == "authorization" || strings.Contains(lk, "credential") {
			return fmt.Errorf("config: server URL query parameter %q looks like a credential and is rejected", k)
		}
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf(
			"config: plain HTTP to non-loopback host %q rejected (use https; loopback http is allowed for dev/demo)", u.Hostname())
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	// loopback IPv4/IPv6 literals
	for _, suffix := range []string{".127.0.0.1", "::1"} {
		_ = suffix
	}
	if host == "[::1]" || host == "::1" {
		return true
	}
	// 127.x.x.x
	parts := strings.Split(host, ".")
	if len(parts) == 4 && parts[0] == "127" {
		return true
	}
	return false
}

func validateTokenSource(env, file string) error {
	if env != "" {
		if strings.ContainsAny(env, "\n\r") {
			return fmt.Errorf("config: tokenEnv name %q contains a newline", env)
		}
		return nil
	}
	if file != "" {
		if !filepath.IsAbs(file) {
			return fmt.Errorf("config: tokenFile %q must be an absolute path", file)
		}
		if st, err := os.Stat(file); err != nil {
			return fmt.Errorf("config: tokenFile %q: %w", file, err)
		} else if st.IsDir() {
			return fmt.Errorf("config: tokenFile %q is a directory", file)
		}
	}
	return nil
}

// displayPath renders the config path in errors without leaking content.
func displayPath(p string) string {
	if p == "" {
		return "config file"
	}
	return "config file " + p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DefaultConfigPath resolves the configuration file location. It returns the
// first candidate that exists, so an existing file is always found:
//
//  1. $XDG_CONFIG_HOME/argo-tui/config.yaml, when that variable is set
//  2. ~/.config/argo-tui/config.yaml, the conventional location on every
//     platform and the one this tool documents
//  3. os.UserConfigDir()/argo-tui/config.yaml, which on macOS is
//     ~/Library/Application Support
//
// When none exists it returns the first candidate, which is where a new file
// should be written.
func DefaultConfigPath() (string, error) {
	var candidates []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "argo-tui", "config.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "argo-tui", "config.yaml"))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "argo-tui", "config.yaml"))
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("config: resolve user config dir: no home or config directory")
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return candidates[0], nil
}
