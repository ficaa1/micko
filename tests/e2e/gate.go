// Package e2e hosts the REAL-cluster tier (ET-4/ET-5 in
// docs/test-environment.md): tests that require an explicitly provisioned
// disposable Argo Workflows v4.1.2 environment (kind cluster or an
// owner-authorized endpoint).
//
// Two gates are REQUIRED before any test here runs (plan §9; this card's
// protocol):
//
//	ARGO_TUI_E2E=1                      — the environment flag
//	config file named by ARGO_TUI_E2E_CONFIG (or ./e2e-config.yaml)
//
// The config must carry an explicit allowlist (see e2eConfig below).
// The environment flag ALONE is not permission to touch a production
// cluster. Pre-registered BLOCKED state: no Docker/kind on this host
// (docs/acceptance-matrix.md §6) — provisioned tests are documented in
// README.md and stay opt-in.
//go:build e2e

package e2e

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// e2eConfig is the explicit allowlist (plan §9: "an explicit
// disposable-context/endpoint allowlist and namespace; the environment
// flag alone is not permission to mutate a production cluster").
type e2eConfig struct {
	// Server is the base URL of the allowlisted Argo Server (loopback
	// port-forward per test-environment.md §4.2).
	Server string
	// Namespace is the single allowlisted test namespace
	// (argo-tui-e2e per §4.4).
	Namespace string
	// Token is an optional bearer token (server-mode SA token from a
	// temp file path instead — never a literal in config).
	Token string
	// AllowActions must be true because the workflow journey submits and
	// deletes synthetic resources; it is never inferred from ARGO_TUI_E2E.
	AllowActions bool
}

// loadE2EConfig reads and validates the allowlist file.
func loadE2EConfig(path string) (e2eConfig, error) {
	// Minimal flat parsing (key=value lines) keeps this file free of a
	// YAML dependency; the file is a test allowlist, not app config.
	data, err := os.ReadFile(path)
	if err != nil {
		return e2eConfig{}, err
	}
	var cfg e2eConfig
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "server":
			cfg.Server = strings.TrimSpace(v)
		case "namespace":
			cfg.Namespace = strings.TrimSpace(v)
		case "token":
			cfg.Token = strings.TrimSpace(v)
		case "allowActions":
			cfg.AllowActions = strings.TrimSpace(v) == "true"
		}
	}
	return cfg, nil
}

// e2eGate reports whether the REAL-tier harness may run, with the reason
// it may not.
func e2eGate() (allowed bool, reason string) {
	if os.Getenv("ARGO_TUI_E2E") != "1" {
		return false, "ARGO_TUI_E2E != 1 (REAL-tier tests are opt-in; environment flag alone is not permission)"
	}
	cfg := os.Getenv("ARGO_TUI_E2E_CONFIG")
	if cfg == "" {
		cfg = "e2e-config.yaml"
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return false, "e2e config " + cfg + " not found: " + err.Error() +
			" (provide ARGO_TUI_E2E_CONFIG with endpoint/namespace allowlist)"
	}
	parsed := parseAllowlist(string(data))
	if parsed.Server == "" || parsed.Namespace == "" {
		return false, "e2e config " + cfg + " must define server= and namespace= " +
			"(explicit endpoint/namespace allowlist required; got server=" + parsed.Server +
			" namespace=" + parsed.Namespace + ")"
	}
	if err := validateE2EConfig(parsed, true); err != nil {
		return false, "e2e config rejected: " + err.Error()
	}
	return true, ""
}

// requireGate skips the test with the recorded cause when the gate is
// closed — an explicit skip with a recorded cause, never a silent pass
// (acceptance matrix rule of trust).
func requireGate(t *testing.T) e2eConfig {
	t.Helper()
	allowed, reason := e2eGate()
	if !allowed {
		t.Skipf("E2E gate closed: %s", reason)
	}
	cfgPath := os.Getenv("ARGO_TUI_E2E_CONFIG")
	if cfgPath == "" {
		cfgPath = "e2e-config.yaml"
	}
	cfg, err := loadE2EConfig(cfgPath)
	if err != nil {
		t.Skipf("E2E gate closed: config unreadable: %v", err)
	}
	if cfg.Server == "" || cfg.Namespace == "" {
		t.Skipf("E2E gate closed: allowlist incomplete (server=%q namespace=%q)", cfg.Server, cfg.Namespace)
	}
	return cfg
}

// parseAllowlist extracts key=value pairs from allowlist text.
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
		switch strings.TrimSpace(k) {
		case "server":
			cfg.Server = strings.TrimSpace(v)
		case "namespace":
			cfg.Namespace = strings.TrimSpace(v)
		case "token":
			cfg.Token = strings.TrimSpace(v)
		case "allowActions":
			cfg.AllowActions = strings.TrimSpace(v) == "true"
		}
	}
	return cfg
}

// validateE2EConfig fails closed on unsafe endpoints and missing explicit
// mutation authorization. Endpoint and namespace are kept exact by callers.
func validateE2EConfig(cfg e2eConfig, mutation bool) error {
	if cfg.Server == "" || cfg.Namespace == "" {
		return fmt.Errorf("server and namespace are required")
	}
	u, err := url.Parse(cfg.Server)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return fmt.Errorf("server must be an absolute URL without userinfo")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return fmt.Errorf("server must use HTTPS or loopback HTTP")
	}
	if mutation && !cfg.AllowActions {
		return fmt.Errorf("allowActions=true is required for the submit/delete journey")
	}
	return nil
}
