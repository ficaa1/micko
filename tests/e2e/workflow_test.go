// Package e2e hosts the REAL-cluster tier (ET-4/ET-5 in
// docs/test-environment.md): tests that require an explicitly provisioned
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

	"argo-tui/internal/core"
)

// e2eWorkflowSpec identifies one testdata fixture for the journey.
type e2eWorkflowSpec struct {
	// File is the testdata YAML name (also the submitted manifest).
	File string
	// Name is the expected workflow name after submission.
	Name string
	// Namespace is the allowlisted test namespace.
	Namespace string
}

// runE2EWorkflowJourney is the shared ET-4 journey used by the test below.
// It is deliberately sequential: submit → wait phase → list → detail →
// version check → delete → verify deletion, with the timeout discipline
// from test-environment.md §4.4 (poll, never busy-loop forever).
func runE2EWorkflowJourney(t *testing.T, client *e2eClient, spec e2eWorkflowSpec) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Submit the pinned synthetic fixture.
	if err := client.submitWorkflow(ctx, spec.Namespace, spec.File); err != nil {
		t.Fatalf("submit %s: %v", spec.File, err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		_ = client.deleteWorkflow(cctx, spec.Namespace, spec.Name)
	})

	// 2. Wait (bounded) for a terminal phase — CMP-01 precondition.
	phase, err := client.waitForPhase(ctx, spec.Namespace, spec.Name,
		[]string{"Succeeded", "Failed", "Error"}, 3*time.Minute)
	if err != nil {
		t.Fatalf("workflow %s never reached a terminal phase: %v", spec.Name, err)
	}
	t.Logf("workflow %s reached phase %s", spec.Name, phase)

	// 3. List via the production Reader (CMP-01 list leg).
	page, err := client.reader.List(ctx, core.Query{Namespace: spec.Namespace, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed bool
	for _, it := range page.Items {
		if it.Ref.Name == spec.Name {
			listed = true
			if it.Phase != phase {
				t.Errorf("listed phase %q != observed %q", it.Phase, phase)
			}
		}
	}
	if !listed {
		t.Errorf("workflow %s missing from list of %d items", spec.Name, len(page.Items))
	}

	// 4. Detail via the production Reader (CMP-01 detail leg).
	wf, err := client.reader.Get(ctx, core.Ref{Namespace: spec.Namespace, Name: spec.Name})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if wf.Summary.Phase != phase {
		t.Errorf("detail phase %q != observed %q", wf.Summary.Phase, phase)
	}

	// 5. Version endpoint identity (CMP-02).
	ver, err := client.serverVersion(ctx)
	if err != nil {
		t.Logf("version endpoint unavailable: %v (compatibility note required)", err)
	} else if !strings.HasPrefix(ver, "v4.1.") && !strings.HasPrefix(ver, "v3.") {
		t.Errorf("unexpected server version %q: compatibility note required, not a crash", ver)
	}

	// 6. Delete + verify gone (cleanup contract, test-environment §4.5).
	if err := client.deleteWorkflow(ctx, spec.Namespace, spec.Name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := client.waitForDeletion(ctx, spec.Namespace, spec.Name, time.Minute); err != nil {
		t.Errorf("workflow %s still present after delete: %v", spec.Name, err)
	}
}

// TestE2EWorkflowJourney runs the hello-world fixture through the full
// submit/list/detail/delete journey. REAL-class (matrix CMP-01/CMP-02).
// Testdata: testdata/hello-world.yaml — synthetic, labeled, never a
// production manifest (docs/test-environment.md §4.4).
func TestE2EWorkflowJourney(t *testing.T) {
	cfg := requireGate(t)
	client := newE2EClient(t, cfg)
	runE2EWorkflowJourney(t, client, e2eWorkflowSpec{
		File:      "testdata/hello-world.yaml",
		Name:      "argo-tui-e2e-hello",
		Namespace: cfg.Namespace,
	})
}
