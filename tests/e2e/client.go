//go:build e2e

package e2e

// client.go — the ET-4 journey client.
//
// HARD DEPENDENCY LABEL (E1 honest-labeling rule, plan §8 E1 gate): the
// journey drives the PRODUCTION core.Reader (internal/argo, A1 branch).
// That package is not merged to this branch yet, so this file cannot
// reference it without breaking the build. Until I1 merges internal/argo,
// newE2EClient returns a "gate open but adapter missing" skip: the REAL
// tier stays BLOCKED-DEPENDENCY (docs/acceptance-matrix.md §6 item 3
// pattern — never a silent pass, never a fake claim).
//
// After the merge, swap the marked block for:
//
//	reader, err := argo.NewClient(argo.Options{Server: cfg.Server, ...})
//	… and delete the dependencySkip branch below.
//
// The submit/delete/wait helpers are REST calls against the pinned
// v4.1.2 surface (POST /api/v1/workflows/{namespace}, DELETE
// /api/v1/workflows/{namespace}/{name}, GET /api/v1/workflows/{ns}/{name})
// implemented here with the standard library so no extra dependency is
// needed (the harness never uses the production read-only adapter for
// writes — it has none, by design, plan §6).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"argo-tui/internal/core"
	"gopkg.in/yaml.v3"
)

// e2eClient carries the journey's collaborators. Reader is the production
// core.Reader under test (nil until internal/argo is merged — see the
// dependency label above).
type e2eClient struct {
	reader core.Reader
	base   string
	token  string
	http   *http.Client
}

// newE2EClient builds the journey client from the allowlist. It skips the
// test when the production adapter is not merged yet (the honest
// BLOCKED-DEPENDENCY state, recorded as the skip reason).
func newE2EClient(t *testing.T, cfg e2eConfig) *e2eClient {
	t.Helper()
	if !productionReaderAvailable() {
		t.Skipf("E2E gate open but production adapter (internal/argo, A1) is not merged on this branch — " +
			"REAL tier stays BLOCKED-DEPENDENCY until I1; fake-server (ET-2) coverage remains active")
	}
	reader, err := buildProductionReader(cfg)
	if err != nil {
		t.Fatalf("build production reader: %v", err)
	}
	return &e2eClient{
		reader: reader,
		base:   strings.TrimSuffix(cfg.Server, "/"),
		token:  cfg.Token,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// buildProductionReader wires the production adapter (internal/argo) once
// it is merged. Kept as a function so the swap is a one-line change:
//
//	return argo.NewReader(argo.Options{
//	    Server:   cfg.Server,
//	    TokenFn:  func() (string, error) { return cfg.Token, nil },
//	    TokenSource: "e2e allowlist token",
//	})
//
// Until then it is unreachable (guarded by productionReaderAvailable).
func buildProductionReader(cfg e2eConfig) (core.Reader, error) {
	return nil, fmt.Errorf("internal/argo not merged yet (I1 swaps this stub for the production client)")
}

// productionReaderAvailable reports whether internal/argo is importable
// on this branch. Implementation note: Go has no runtime existence probe
// for packages, so this is a build-time constant set by the merge state —
// flipped to true in the I1 integration commit (single place to change).
const adapterMerged = false

func productionReaderAvailable() bool { return adapterMerged }

// submitWorkflow POSTs the testdata manifest (Argo v4.1.2: POST
// /api/v1/workflows/{namespace} with the workflow in the request body).
func (c *e2eClient) submitWorkflow(ctx context.Context, ns, manifestPath string) error {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}
	var document any
	if err := yaml.Unmarshal(manifest, &document); err != nil {
		return fmt.Errorf("parse fixture YAML: %w", err)
	}
	body, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode fixture JSON: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/v1/workflows/"+ns, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("submit: HTTP %d: %s", resp.StatusCode, truncate(b))
	}
	return nil
}

// deleteWorkflow issues DELETE /api/v1/workflows/{ns}/{name} (404 tolerated:
// already gone).
func (c *e2eClient) deleteWorkflow(ctx context.Context, ns, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.base+"/api/v1/workflows/"+ns+"/"+name, nil)
	if err != nil {
		return err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delete: HTTP %d: %s", resp.StatusCode, truncate(b))
	}
	return nil
}

// waitForPhase polls the workflow until phase ∈ terminal or the timeout
// hits (test-environment.md §4.4 wait discipline: bounded, never busy-loop
// forever).
func (c *e2eClient) waitForPhase(ctx context.Context, ns, name string, terminal []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		phase, err := c.fetchPhase(ctx, ns, name)
		if err == nil {
			for _, want := range terminal {
				if phase == want {
					return phase, nil
				}
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return "", fmt.Errorf("wait: last error: %w", err)
			}
			return "", fmt.Errorf("wait: phase %q not in %v within %s", phase, terminal, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// waitForDeletion polls until the workflow 404s (bounded).
func (c *e2eClient) waitForDeletion(ctx context.Context, ns, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := c.workflowStatus(ctx, ns, name)
		if err == nil && status == http.StatusNotFound {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("verify deletion: %w", err)
			}
			return fmt.Errorf("workflow still returned HTTP %d after %s", status, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// workflowStatus performs the non-mutating read used to verify deletion.
// In particular, 403 is not treated as proof that the object is gone.
func (c *e2eClient) workflowStatus(ctx context.Context, ns, name string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/api/v1/workflows/"+ns+"/"+name, nil)
	if err != nil {
		return 0, err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

// fetchPhase reads the workflow's status.phase (synthetic detail probe).
func (c *e2eClient) fetchPhase(ctx context.Context, ns, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/api/v1/workflows/"+ns+"/"+name, nil)
	if err != nil {
		return "", err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get: HTTP %d", resp.StatusCode)
	}
	var wf struct {
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wf); err != nil {
		return "", err
	}
	return wf.Status.Phase, nil
}

// serverVersion reads GET /api/v1/version (CMP-02 identity check).
func (c *e2eClient) serverVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/version", nil)
	if err != nil {
		return "", err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version: HTTP %d", resp.StatusCode)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Version, nil
}

func (c *e2eClient) auth(r *http.Request) {
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
