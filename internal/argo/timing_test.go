package argo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnostics"
)

// Each request writes one timing named by its endpoint. A list's timing ends
// with its own body, not after the gate scan that follows it, and the next
// list rides on the open connection.
func TestRequestTimings(t *testing.T) {
	page := loadFixture(t, "list_page1.json")
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			time.Sleep(50 * time.Millisecond)
			serveNoGates(w)
			return
		}
		_, _ = w.Write(page)
	})
	var out bytes.Buffer
	c := newClientWith(t, Options{Server: srv.URL, Diagnostics: diagnostics.New(&out)})

	for range 2 {
		if _, err := c.List(context.Background(), core.Query{Namespace: "team-a"}); err != nil {
			t.Fatal(err)
		}
	}
	srv.Close()
	if _, err := c.List(context.Background(), core.Query{Namespace: "team-a"}); err == nil {
		t.Fatal("List against a closed server succeeded")
	}

	var got []diagnostics.Event
	dec := json.NewDecoder(&out)
	for dec.More() {
		var e diagnostics.Event
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	// conn is unchecked where empty: a failed request may first pick an idle
	// connection the closed server already dropped.
	want := []struct{ endpoint, state, conn string }{
		{"list", "ok", "new"},
		{"gate", "ok", "reused"},
		{"list", "ok", "reused"},
		{"gate", "ok", "reused"},
		{"list", "failed", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d timings, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		e := got[i]
		if e.Stage != diagnostics.StageRequest || e.Endpoint != w.endpoint || e.State != w.state || (w.conn != "" && e.Conn != w.conn) {
			t.Errorf("timing %d = %s %s %s %s, want request %s %s %s", i, e.Stage, e.Endpoint, e.State, e.Conn, w.endpoint, w.state, w.conn)
		}
	}
	if first := got[0]; first.Status != 200 || first.Bytes != int64(len(page)) || first.TTFBMS <= 0 || first.TotalMS < first.TTFBMS {
		t.Errorf("first list = status %d, %d bytes, ttfb %.1f ms, total %.1f ms; want 200, %d bytes, 0 < ttfb <= total",
			first.Status, first.Bytes, first.TTFBMS, first.TotalMS, len(page))
	}
	if list := got[0].TotalMS; list >= 50 {
		t.Errorf("list total = %.1f ms, want less than the 50 ms gate scan after it", list)
	}
}
