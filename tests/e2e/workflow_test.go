// Package e2e hosts the REAL-cluster tier (see
// docs/development.md): tests that require an explicitly provisioned
// disposable Argo Workflows v4.1.2 environment (kind cluster or an
// owner-authorized endpoint). Gate + fixtures live in gate.go; this file
// implements the workflow journey against whatever endpoint the gate
// allowlisted.
//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// e2eWorkflowSpec identifies one testdata fixture for the journey.
type e2eWorkflowSpec struct {
	// File is the testdata YAML name (also the submitted manifest).
	File string
	// Name is the generateName prefix in the fixture. The server appends a
	// suffix, so it is a prefix to check, never the name to address.
	Name string
	// Namespace is the allowlisted test namespace.
	Namespace string
}

// runE2EWorkflowJourney is the shared real-cluster journey used by the test below.
// It is deliberately sequential: submit → wait phase → list → detail →
// version check → delete → verify deletion, with the timeout discipline
// from docs/development.md (poll, never busy-loop forever).
func runE2EWorkflowJourney(t *testing.T, client *e2eClient, spec e2eWorkflowSpec) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Submit the pinned synthetic fixture. The fixture uses
	// generateName, so the server names the workflow and every later step
	// addresses it by the name it answers with.
	name, err := client.submitWorkflow(ctx, spec.Namespace, spec.File)
	if err != nil {
		t.Fatalf("submit %s: %v", spec.File, err)
	}
	if !strings.HasPrefix(name, spec.Name) {
		t.Fatalf("the server named the workflow %q, which does not start with %q", name, spec.Name)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		_ = client.deleteWorkflow(cctx, spec.Namespace, name)
	})

	// 2. Wait (bounded) for a terminal phase.
	phase, err := client.waitForPhase(ctx, spec.Namespace, name,
		[]string{"Succeeded", "Failed", "Error"}, 3*time.Minute)
	if err != nil {
		t.Fatalf("workflow %s never reached a terminal phase: %v", name, err)
	}
	t.Logf("workflow %s reached phase %s", name, phase)

	// 3. List via the production Reader.
	page, err := client.reader.List(ctx, core.Query{Namespace: spec.Namespace, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed bool
	for _, it := range page.Items {
		if it.Ref.Name == name {
			listed = true
			if it.Phase != phase {
				t.Errorf("listed phase %q != observed %q", it.Phase, phase)
			}
		}
	}
	if !listed {
		t.Errorf("workflow %s missing from list of %d items", name, len(page.Items))
	}

	// 4. Detail via the production Reader.
	wf, err := client.reader.Get(ctx, core.Ref{Namespace: spec.Namespace, Name: name})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if wf.Summary.Phase != phase {
		t.Errorf("detail phase %q != observed %q", wf.Summary.Phase, phase)
	}

	// 5. Version endpoint identity.
	ver, err := client.serverVersion(ctx)
	if err != nil {
		t.Logf("version endpoint unavailable: %v (compatibility note required)", err)
	} else if !strings.HasPrefix(ver, "v4.1.") && !strings.HasPrefix(ver, "v3.") {
		t.Errorf("unexpected server version %q: compatibility note required, not a crash", ver)
	}

	// 6. Delete + verify gone (cleanup contract).
	if err := client.deleteWorkflow(ctx, spec.Namespace, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := client.waitForDeletion(ctx, spec.Namespace, name, time.Minute); err != nil {
		t.Errorf("workflow %s still present after delete: %v", name, err)
	}
}

// TestE2EWorkflowJourney runs the hello-world fixture through the full
// submit/list/detail/delete journey. REAL-class.
// Testdata: testdata/hello-world.yaml — synthetic, labeled, never a
// production manifest (docs/development.md).
func TestE2EWorkflowJourney(t *testing.T) {
	cfg := requireGate(t)
	client := newE2EClient(t, cfg)
	runE2EWorkflowJourney(t, client, e2eWorkflowSpec{
		File:      "testdata/hello-world.yaml",
		Name:      "argo-tui-e2e-hello",
		Namespace: cfg.Namespace,
	})
}
