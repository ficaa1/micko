//go:build e2e

package e2e

// The E2E harness uses its own HTTP reader and submit/delete helpers.

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

	"github.com/ficaa1/micko/internal/core"
	"gopkg.in/yaml.v3"
)

// e2eClient carries the harness reader, endpoint and credentials.
type e2eClient struct {
	reader core.Reader
	base   string
	token  string
	http   *http.Client
}

// newE2EClient builds the harness reader from the validated allowlist.
func newE2EClient(t *testing.T, cfg e2eConfig) *e2eClient {
	t.Helper()
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

func buildProductionReader(cfg e2eConfig) (core.Reader, error) {
	if err := validateE2EConfig(cfg, false); err != nil {
		return nil, fmt.Errorf("production reader config: %w", err)
	}
	return &productionReader{base: strings.TrimSuffix(cfg.Server, "/"), token: cfg.Token,
		http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// productionReader is the real HTTP adapter used by the opt-in journey. It
// intentionally shares no fake-server code: configured endpoints either
// answer the Argo API or return an actionable error.
type productionReader struct {
	base, token string
	http        *http.Client
}

func (r *productionReader) List(ctx context.Context, q core.Query) (core.Page, error) {
	path := r.base + "/api/v1/workflows/" + q.Namespace
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return core.Page{}, err
	}
	r.auth(req)
	resp, err := r.http.Do(req)
	if err != nil {
		return core.Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return core.Page{}, fmt.Errorf("list: HTTP %d", resp.StatusCode)
	}
	var v struct {
		Items []struct {
			Metadata struct {
				Name, Namespace, UID, ResourceVersion string
				CreationTimestamp                     time.Time `json:"creationTimestamp"`
				Labels                                map[string]string
			} `json:"metadata"`
			Status struct{ Phase, Message string } `json:"status"`
		} `json:"items"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return core.Page{}, fmt.Errorf("list response: %w", err)
	}
	p := core.Page{ResourceVersion: v.Metadata.ResourceVersion}
	for _, item := range v.Items {
		m := item.Metadata
		p.Items = append(p.Items, core.Summary{Ref: core.Ref{Namespace: m.Namespace, Name: m.Name, UID: m.UID}, ResourceVersion: m.ResourceVersion, Phase: item.Status.Phase, Message: item.Status.Message, CreatedAt: m.CreationTimestamp, Labels: m.Labels})
	}
	return p, nil
}

func (r *productionReader) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/api/v1/workflows/"+ref.Namespace+"/"+ref.Name, nil)
	if err != nil {
		return core.Workflow{}, err
	}
	r.auth(req)
	resp, err := r.http.Do(req)
	if err != nil {
		return core.Workflow{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return core.Workflow{}, fmt.Errorf("get: HTTP %d", resp.StatusCode)
	}
	var v struct {
		Metadata struct {
			Name, Namespace, UID, ResourceVersion string
			CreationTimestamp                     time.Time `json:"creationTimestamp"`
			Labels                                map[string]string
		} `json:"metadata"`
		Status struct{ Phase, Message string } `json:"status"`
		Spec   json.RawMessage                 `json:"spec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return core.Workflow{}, fmt.Errorf("get response: %w", err)
	}
	m := v.Metadata
	return core.Workflow{Summary: core.Summary{Ref: core.Ref{Namespace: m.Namespace, Name: m.Name, UID: m.UID}, ResourceVersion: m.ResourceVersion, Phase: v.Status.Phase, Message: v.Status.Message, CreatedAt: m.CreationTimestamp, Labels: m.Labels}, NodesAvailable: false, NodesUnavailableReason: "production E2E reader does not synthesize nodes", Resource: v.Spec}, nil
}

func (r *productionReader) StreamLogs(context.Context, core.LogRequest, func(core.LogRecord) error) error {
	return fmt.Errorf("production log streaming is not implemented by this E2E adapter")
}
func (r *productionReader) auth(req *http.Request) {
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
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
