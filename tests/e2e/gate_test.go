//go:build e2e

package e2e

import "testing"

func TestE2EAuthorizationRequiresSafeEndpoint(t *testing.T) {
	for _, server := range []string{"http://argo.example.test", "https://user:secret@argo.example.test"} {
		if err := validateE2EConfig(e2eConfig{Server: server, Namespace: "argo-tui-e2e"}, false); err == nil {
			t.Fatalf("validateE2EConfig(%q) accepted unsafe endpoint", server)
		}
	}
}

func TestE2EAuthorizationRequiresExactMutationOptIn(t *testing.T) {
	cfg := e2eConfig{Server: "https://argo.example.test", Namespace: "argo-tui-e2e"}
	if err := validateE2EConfig(cfg, true); err == nil {
		t.Fatal("mutation run accepted without allowActions=true")
	}
}

func TestE2EAuthorizationAcceptsScopedReadOnlyConfig(t *testing.T) {
	cfg := e2eConfig{Server: "https://argo.example.test", Namespace: "argo-tui-e2e"}
	if err := validateE2EConfig(cfg, false); err != nil {
		t.Fatalf("validateE2EConfig() error = %v", err)
	}
}
