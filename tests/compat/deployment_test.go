//go:build compat

// Package compat exercises a disposable deployment provisioned by scripts/compat/argo.sh.
package compat

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/app"
	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/session"
)

const namespace = "micko-compat"

func TestDisposableDeployment(t *testing.T) {
	if os.Getenv("MICKO_COMPAT") != "1" {
		t.Skip("disposable deployment not provisioned: run scripts/compat/argo.sh")
	}
	server := os.Getenv("MICKO_COMPAT_SERVER")
	parsed, err := url.Parse(server)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(os.Getenv("MICKO_COMPAT_CLUSTER"), "micko-compat-") {
		t.Fatal("refusing a deployment outside the disposable runner")
	}
	caFile := os.Getenv("MICKO_COMPAT_CA")
	tokenFile := os.Getenv("MICKO_COMPAT_TOKEN")
	token := readPrivate(t, tokenFile)
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	t.Run("pinned server version", func(t *testing.T) {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(readPrivate(t, caFile))) {
			t.Fatal("invalid fixture CA")
		}
		transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/v1/version", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("version request failed")
		}
		defer resp.Body.Close()
		var answer struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil || resp.StatusCode != 200 || !strings.HasPrefix(answer.Version, "v4.1.2") {
			t.Fatal("deployment is not Argo v4.1.2")
		}
	})
	t.Run("authentication and CA", func(t *testing.T) {
		for _, c := range []struct {
			name, token, ca string
			kind            core.ErrorKind
		}{
			{"no token", "", caFile, core.ErrUnauthenticated},
			{"invalid token", "invalid-fixture-token", caFile, core.ErrUnauthenticated},
			{"forbidden token", readPrivate(t, os.Getenv("MICKO_COMPAT_FORBIDDEN_TOKEN")), caFile, core.ErrForbidden},
			{"untrusted CA", token, "", ""},
		} {
			t.Run(c.name, func(t *testing.T) {
				client, err := argo.NewClient(argo.Options{Server: server, CAFile: c.ca, TokenFn: func() (string, error) { return c.token, nil }})
				if err != nil {
					t.Fatal("could not construct fixture client")
				}
				_, err = client.List(ctx, core.Query{Namespace: namespace})
				if err == nil {
					t.Fatal("unauthorized or untrusted request succeeded")
				}
				if c.kind != "" {
					ae := core.AsAPIError(err)
					if ae == nil || ae.Kind != c.kind {
						t.Fatalf("expected %s; got %T", c.kind, err)
					}
				}
			})
		}
	})
	for _, profile := range []string{"direct", "managed"} {
		t.Run(profile, func(t *testing.T) {
			connector, err := session.NewConnector(session.Options{ConfigPath: os.Getenv("MICKO_COMPAT_CONFIG")})
			if err != nil {
				t.Fatal("cannot load temporary profile")
			}
			defer connector.Close()
			connection, err := connector.Connect(ctx, profile)
			if err != nil {
				t.Fatalf("%s connection failed: %v", profile, err)
			}
			if connection.Namespace != namespace {
				t.Fatal("destination namespace differs")
			}
			var endpoint string
			if profile == "managed" {
				endpoint = waitReady(t, connection)
			}
			reader, ok := connection.Reader.(core.ResourceReader)
			if !ok {
				t.Fatal("missing resource reader")
			}
			writer, ok := connection.Reader.(core.ResourceActioner)
			if !ok {
				t.Fatal("missing resource writer")
			}
			actioner, ok := connection.Reader.(core.Actioner)
			if !ok {
				t.Fatal("missing workflow actions")
			}
			for _, source := range []struct {
				kind core.ResourceKind
				name string
			}{
				{core.KindWorkflowTemplate, "micko-compat-template"},
				{core.KindClusterWorkflowTemplate, "micko-compat-cluster-template"},
				{core.KindCronWorkflow, "micko-compat-cron"},
			} {
				t.Run(string(source.kind), func(t *testing.T) {
					ref := core.Ref{Namespace: namespace, Name: source.name}
					if source.kind == core.KindClusterWorkflowTemplate {
						ref.Namespace = ""
					}
					state, err := reader.GetResource(ctx, source.kind, ref)
					if err != nil || state.Ref.UID == "" {
						t.Fatal("cannot read source identity")
					}
					if source.kind == core.KindCronWorkflow {
						for _, action := range []core.Action{core.ActionResume, core.ActionSuspend} {
							result, err := writer.ExecuteResourceAction(ctx, core.ResourceActionRequest{Resource: state, Namespace: namespace, Action: action, Confirmation: core.Confirmation{Confirmed: true}})
							if err != nil || result.Outcome != core.ActionAccepted {
								t.Fatal("cron action was not accepted")
							}
							after, err := reader.GetResource(ctx, source.kind, state.Ref)
							if err != nil || after.Ref != state.Ref || after.Suspended != (action == core.ActionSuspend) {
								t.Fatal("cron suspend state did not change")
							}
							state = after
						}
					}
					message := "compat-" + profile + "-" + string(source.kind)
					result, err := writer.ExecuteResourceAction(ctx, core.ResourceActionRequest{Resource: state, Namespace: namespace, Action: core.ActionSubmit, Parameters: []string{"message=" + message}, Confirmation: core.Confirmation{Confirmed: true}})
					if err != nil || result.Outcome != core.ActionAccepted || result.Affected == nil {
						t.Fatal("submission was not accepted")
					}
					created := *result.Affected
					if created.Namespace != namespace || created.UID == "" {
						t.Fatal("submission has wrong identity")
					}
					t.Cleanup(func() {
						cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						existing, err := connection.Reader.Get(cleanupCtx, created)
						if err != nil {
							if ae := core.AsAPIError(err); ae != nil && ae.Kind == core.ErrNotFound {
								return
							}
							t.Error("cleanup cannot verify workflow ownership")
							return
						}
						if existing.Summary.Ref != created || existing.Summary.Labels["app.kubernetes.io/part-of"] != "micko-compat" {
							t.Error("cleanup refused a replacement")
							return
						}
						if _, err := actioner.Execute(cleanupCtx, core.ActionRequest{Ref: created, Action: core.ActionDelete, Confirmation: core.Confirmation{Confirmed: true, Final: true}}); err != nil {
							t.Error("cleanup delete failed")
						}
					})
					pollCtx, pollCancel := context.WithTimeout(ctx, 3*time.Minute)
					defer pollCancel()
					for {
						wf, err := connection.Reader.Get(pollCtx, created)
						if err != nil {
							t.Fatal("read submitted workflow failed")
						}
						if core.FinishedPhase(wf.Summary.Phase) {
							if wf.Summary.Phase != "Succeeded" {
								t.Fatalf("workflow finished %s", wf.Summary.Phase)
							}
							break
						}
						select {
						case <-pollCtx.Done():
							t.Fatal("workflow did not finish")
						case <-time.After(time.Second):
						}
					}
					page, err := connection.Reader.List(ctx, core.Query{Namespace: namespace, Limit: 100})
					if err != nil {
						t.Fatal("workflow list failed")
					}
					found := false
					for _, row := range page.Items {
						if row.Ref == created {
							found = true
						}
					}
					if !found {
						t.Fatal("submitted workflow not listed")
					}
					var logs strings.Builder
					err = connection.Reader.StreamLogs(ctx, core.LogRequest{Ref: created, Container: "main"}, func(record core.LogRecord) error { logs.WriteString(record.Content); return nil })
					if err != nil || !strings.Contains(logs.String(), message) {
						t.Fatal("production log transport did not return the submitted parameter")
					}
					if _, err := actioner.Execute(ctx, core.ActionRequest{Ref: created, Action: core.ActionDelete, Confirmation: core.Confirmation{Confirmed: true, Final: true}}); err != nil {
						t.Fatal("workflow delete failed")
					}
					_, err = connection.Reader.Get(ctx, created)
					if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrNotFound {
						t.Fatal("workflow still available after deletion")
					}
				})
			}
			if profile == "managed" {
				connection.Close()
				u, err := url.Parse(endpoint)
				if err != nil {
					t.Fatal("invalid managed endpoint")
				}
				deadline := time.Now().Add(10 * time.Second)
				for {
					socket, err := net.DialTimeout("tcp", u.Host, 200*time.Millisecond)
					if err != nil {
						break
					}
					socket.Close()
					if time.Now().After(deadline) {
						t.Fatal("managed forward listener survived Close")
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
		})
	}
}

func readPrivate(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read disposable credential or CA file")
	}
	return strings.TrimSpace(string(data))
}

func waitReady(t *testing.T, connection *app.Connection) string {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state, ok := <-connection.States:
			if !ok {
				t.Fatal("managed lifecycle ended before ready")
			}
			if state.Ready {
				return state.Target
			}
		case <-timer.C:
			t.Fatal("managed forward never became ready")
		}
	}
}
