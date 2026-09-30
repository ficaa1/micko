//go:build e2e

package e2e

// The journey reads and deletes through the production client; the harness
// submits the workflows and polls their state with its own requests.

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

	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/buildinfo"
	"gopkg.in/yaml.v3"
)

// e2eClient carries the production client and what the harness needs to
// submit and delete the synthetic workflows the product cannot create.
type e2eClient struct {
	reader *argo.Client
	base   string
	token  string
	http   *http.Client
}

// newE2EClient builds the production Argo client for the allowlisted
// endpoint, and the harness's own requests beside it.
func newE2EClient(t *testing.T, cfg e2eConfig) *e2eClient {
	t.Helper()
	reader, err := argo.NewClient(argo.Options{
		Server:    cfg.Server,
		TokenFn:   func() (string, error) { return cfg.Token, nil },
		UserAgent: buildinfo.UserAgent(),
	})
	if err != nil {
		t.Fatalf("argo client: %v", err)
	}
	return &e2eClient{
		reader: reader,
		base:   strings.TrimSuffix(cfg.Server, "/"),
		token:  cfg.Token,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// submitWorkflow POSTs the testdata manifest (Argo v4.1.2: POST
// /api/v1/workflows/{namespace} with the workflow inside a create request)
// and returns the name the server assigned.
//
// The fixture uses generateName, so the name is not known until the server
// answers. Every later step addresses the workflow by that name, including
// the cleanup delete: a guessed name waits for a workflow that does not
// exist and leaves the created one behind.
func (c *e2eClient) submitWorkflow(ctx context.Context, ns, manifestPath string) (string, error) {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("read fixture: %w", err)
	}
	var document any
	if err := yaml.Unmarshal(manifest, &document); err != nil {
		return "", fmt.Errorf("parse fixture YAML: %w", err)
	}
	body, err := json.Marshal(map[string]any{"namespace": ns, "workflow": document})
	if err != nil {
		return "", fmt.Errorf("encode fixture JSON: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/v1/workflows/"+ns, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	created, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("submit: HTTP %d: %s", resp.StatusCode, truncate(created))
	}
	if readErr != nil {
		return "", fmt.Errorf("submit: reading the response failed: %w", readErr)
	}
	var answer struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(created, &answer); err != nil {
		return "", fmt.Errorf("submit: unparseable response: %w", err)
	}
	if answer.Metadata.Name == "" {
		return "", fmt.Errorf("submit: the server named no workflow: %s", truncate(created))
	}
	return answer.Metadata.Name, nil
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
// hits (docs/development.md wait discipline: bounded, never busy-loop
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

// serverVersion reads GET /api/v1/version (identity check).
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
