//go:build e2e

// Package e2e contains opt-in disposable-cluster tests. The journey requires
// MICKO_E2E=1 and an endpoint/namespace allowlist with allowActions=true.
// See docs/development.md for invocation and cleanup limits.
package e2e

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// e2eConfig is the explicit allowlist: a disposable endpoint and namespace.
// The environment flag alone is not permission to mutate a cluster.
type e2eConfig struct {
	// Server is the base URL of the allowlisted Argo Server.
	Server string
	// Namespace is the single allowlisted test namespace.
	Namespace string
	// Token is an optional bearer token.
	Token string
	// AllowActions must be true because the journey submits and deletes
	// synthetic workflows; it is never inferred from MICKO_E2E.
	AllowActions bool
}

// requireGate returns the allowlisted config, or skips the test with the
// reason the gate is closed: an explicit skip, never a silent pass.
func requireGate(t *testing.T) e2eConfig {
	t.Helper()
	cfg, closed := openGate()
	if closed != "" {
		t.Skipf("E2E gate closed: %s", closed)
	}
	return cfg
}

// openGate reads the allowlist MICKO_E2E_CONFIG names, e2e-config.yaml by
// default, and returns it with an empty reason, or why the gate stays closed.
func openGate() (e2eConfig, string) {
	if os.Getenv("MICKO_E2E") != "1" {
		return e2eConfig{}, "MICKO_E2E != 1 (REAL-tier tests are opt-in; environment flag alone is not permission)"
	}
	path := os.Getenv("MICKO_E2E_CONFIG")
	if path == "" {
		path = "e2e-config.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return e2eConfig{}, "e2e config " + path + " not found: " + err.Error() +
			" (provide MICKO_E2E_CONFIG with endpoint/namespace allowlist)"
	}
	cfg := parseAllowlist(string(data))
	if err := validateE2EConfig(cfg); err != nil {
		return e2eConfig{}, "e2e config " + path + " rejected: " + err.Error()
	}
	return cfg, ""
}

// parseAllowlist reads key=value lines, skipping blanks and # comments. The
// flat format keeps the harness free of a YAML parser for its own config.
func parseAllowlist(text string) e2eConfig {
	var cfg e2eConfig
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "server":
			cfg.Server = v
		case "namespace":
			cfg.Namespace = v
		case "token":
			cfg.Token = v
		case "allowActions":
			cfg.AllowActions = v == "true"
		}
	}
	return cfg
}

// validateE2EConfig fails closed on a missing endpoint or namespace, an
// unsafe endpoint, and a missing mutation opt-in.
func validateE2EConfig(cfg e2eConfig) error {
	if cfg.Server == "" || cfg.Namespace == "" {
		return fmt.Errorf("server= and namespace= are required")
	}
	u, err := url.Parse(cfg.Server)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return fmt.Errorf("server must be an absolute URL without userinfo")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return fmt.Errorf("server must use HTTPS or loopback HTTP")
	}
	if !cfg.AllowActions {
		return fmt.Errorf("allowActions=true is required for the submit/delete journey")
	}
	return nil
}
