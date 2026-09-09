//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"argo-tui/internal/core"
)

func TestProductionReaderUsesConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/ns" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"status":{"phase":"Succeeded"}}]}`))
	}))
	defer server.Close()
	reader, err := buildProductionReader(e2eConfig{Server: server.URL, Namespace: "ns"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.List(context.Background(), core.Query{Namespace: "ns"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref.Name != "wf" {
		t.Fatalf("list = %+v, err = %v", page, err)
	}
}

func TestProductionReaderFailsClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bad", http.StatusBadGateway) }))
	defer server.Close()
	reader, err := buildProductionReader(e2eConfig{Server: server.URL, Namespace: "ns"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.List(context.Background(), core.Query{Namespace: "ns"}); err == nil {
		t.Fatal("expected endpoint failure")
	}
}
