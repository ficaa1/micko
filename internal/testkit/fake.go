// Package testkit provides the independent fake core.Reader, a fake clock
// and explicitly synthetic fixtures for deterministic tests (plan §8 F1
// slice 3; environments ET-1 in docs/development.md).
//
// Fixture policy: everything here is SYNTHETIC and named as such. Nothing in
// this package is a captured production payload, and nothing may be
// presented as one (plan: "Fixtures explicitly synthetic, never mislabeled
// captures").
package testkit

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// FakeClock is a controllable time source for deterministic tests. Zero
// value is usable (Starts at its zero time); Advance moves it forward.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a clock pinned at t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{now: t} }

// Now implements a monotonic-ish wall clock read.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// FakeReader is an independent in-memory core.Reader for ET-1 tests and the
// --demo backend. It never performs I/O and never constructs HTTP clients
// (ADR 0001: demo runs on the fake Reader only).
type FakeReader struct {
	mu        sync.Mutex
	Workflows map[core.Ref]core.Workflow
	// Order defines deterministic List ordering (already sorted by the
	// test/demo builder); when empty, items sort by (Namespace, Name).
	Order []core.Ref
	// PageLimit, when > 0, forces List to paginate in pages of this size
	// so pagination consumers can be exercised deterministically.
	PageLimit int64

	// ListErr/GetErr/StreamErr are injectable errors returned by the
	// corresponding call (before any other behavior).
	ListErr   error
	GetErr    error
	StreamErr error

	// CronWorkflows is what ListCronWorkflows answers from, filtered by
	// namespace. CronErr fails the call instead.
	CronWorkflows []core.CronWorkflow
	CronErr       error

	// WorkflowTemplates and ClusterWorkflowTemplates answer the template
	// lists; TemplateErr and ClusterTemplateErr fail them instead.
	WorkflowTemplates        []core.WorkflowTemplate
	ClusterWorkflowTemplates []core.WorkflowTemplate
	TemplateErr              error
	ClusterTemplateErr       error

	// ArchivedWorkflows is the workflow archive, newest first. ArchiveErr
	// fails both archive calls, the way a server without an archive fails
	// them.
	ArchivedWorkflows []core.Workflow
	ArchiveErr        error

	// Namespaces is the extra namespace list ListNamespaces reports, on top
	// of the namespaces the stored workflows are in. NamespacesErr fails the
	// call instead.
	Namespaces    []string
	NamespacesErr error

	// ListDelay/GetDelay/StreamDelay are injected delays, applied with
	// context awareness (canceled context aborts the wait).
	ListDelay   time.Duration
	GetDelay    time.Duration
	StreamDelay time.Duration

	// StreamSequence, when non-empty, feeds LogRecords to StreamLogs in
	// order; when empty, StreamLogs finishes immediately (clean EOF → nil).
	StreamSequence []core.LogRecord

	// PodLogs and WorkflowLogs, when they hold an entry for the request,
	// replace StreamSequence: PodLogs by pod name for a pod-scoped stream,
	// WorkflowLogs by workflow name for a workflow-wide one. The demo uses
	// them so every pod shows its own log.
	PodLogs      map[string][]core.LogRecord
	WorkflowLogs map[string][]core.LogRecord

	// StreamHook, when set, is invoked at stream start with the request;
	// returning an error fails the stream before any record is delivered.
	StreamHook func(core.LogRequest) error

	// Events are the Kubernetes events WatchEvents serves, EventsErr fails
	// every event stream, and EventHook sees each request first and can
	// fail it. PublishEvent sends an event to the streams open at the time,
	// for a test that needs a live stream. EventStarts, EventCancels and
	// EventRequests record the streams opened.
	Events        []core.Event
	EventsErr     error
	EventHook     func(core.EventWatchRequest) error
	EventStarts   int
	EventCancels  int
	EventRequests []core.EventWatchRequest
	// eventSubs are the open streams' queues for published events.
	eventSubs map[chan core.Event]bool

	// Call counters (guarded by mu) for test assertions.
	ListCalls, GetCalls, StreamStarts int
	// KindCalls counts the list calls of the other resource kinds.
	KindCalls int
	// StreamCancels counts streams that ended via context cancellation.
	StreamCancels int
}

// compile-time proof the fake satisfies the frozen contract.
var _ core.Reader = (*FakeReader)(nil)

// List implements core.Reader.
func (f *FakeReader) List(ctx context.Context, q core.Query) (core.Page, error) {
	f.mu.Lock()
	f.ListCalls++
	listErr, delay := f.ListErr, f.ListDelay
	items := f.collect(q)
	f.mu.Unlock()

	if err := sleepCtx(ctx, delay); err != nil {
		return core.Page{}, err
	}
	if listErr != nil {
		return core.Page{}, listErr
	}
	return f.page(items, q), nil
}

// collect snapshots and filters the workflow map (callers hold f.mu).
func (f *FakeReader) collect(q core.Query) []core.Summary {
	var order []core.Ref
	if len(f.Order) > 0 {
		order = append([]core.Ref(nil), f.Order...)
	} else {
		for ref := range f.Workflows {
			order = append(order, ref)
		}
		sort.Slice(order, func(i, j int) bool {
			if order[i].Namespace != order[j].Namespace {
				return order[i].Namespace < order[j].Namespace
			}
			return order[i].Name < order[j].Name
		})
	}
	items := make([]core.Summary, 0, len(order))
	for _, ref := range order {
		if q.Namespace != "" && ref.Namespace != q.Namespace {
			continue
		}
		wf, ok := f.Workflows[ref]
		if !ok {
			continue
		}
		if !MatchLabels(q.LabelSelector, wf.Summary.Labels) {
			continue
		}
		items = append(items, wf.Summary)
	}
	return items
}

// page applies the opaque-continue pagination contract over items.
func (f *FakeReader) page(items []core.Summary, q core.Query) core.Page {
	limit := q.Limit
	if limit <= 0 && f.PageLimit > 0 {
		limit = f.PageLimit
	}
	page := core.Page{ResourceVersion: "fake-rv"}
	if limit <= 0 || int64(len(items)) <= limit {
		page.Items = items
		return page
	}
	start := int64(0)
	if q.Continue != "" {
		// The fake stores the *index* as its opaque token. Tests treat the
		// token as opaque; only the fake knows the encoding.
		for i, s := range items {
			if continueToken(s.Ref) == q.Continue {
				start = int64(i)
				break
			}
		}
	}
	end := start + limit
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	page.Items = items[start:end]
	if end < int64(len(items)) {
		page.Continue = continueToken(items[end].Ref)
	}
	return page
}

func continueToken(ref core.Ref) string {
	return fmt.Sprintf("opaque:%s/%s", ref.Namespace, ref.Name)
}

// Get implements core.Reader.
func (f *FakeReader) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	f.mu.Lock()
	f.GetCalls++
	getErr, delay := f.GetErr, f.GetDelay
	f.mu.Unlock()

	if err := sleepCtx(ctx, delay); err != nil {
		return core.Workflow{}, err
	}
	if getErr != nil {
		return core.Workflow{}, getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.Workflows[ref]
	if !ok {
		return core.Workflow{}, core.ErrNotFoundf("workflow %s/%s not found", ref.Namespace, ref.Name)
	}
	return wf, nil
}

// StreamLogs implements core.Reader. It feeds StreamSequence records
// serially, honoring injected per-record delay and context cancellation,
// and returns nil on clean EOF.
func (f *FakeReader) StreamLogs(ctx context.Context, req core.LogRequest, cb func(core.LogRecord) error) error {
	f.mu.Lock()
	f.StreamStarts++
	streamErr, delay := f.StreamErr, f.StreamDelay
	hook := f.StreamHook
	seq := append([]core.LogRecord(nil), f.StreamSequence...)
	if req.PodName != "" {
		if recs, ok := f.PodLogs[req.PodName]; ok {
			seq = append([]core.LogRecord(nil), recs...)
		}
	} else if recs, ok := f.WorkflowLogs[req.Ref.Name]; ok {
		seq = append([]core.LogRecord(nil), recs...)
	}
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		f.mu.Lock()
		f.StreamCancels++
		f.mu.Unlock()
		return err
	}
	if hook != nil {
		if err := hook(req); err != nil {
			return err
		}
	}
	if streamErr != nil {
		return streamErr
	}
	for i, rec := range seq {
		if i > 0 {
			if err := sleepCtx(ctx, delay); err != nil {
				f.mu.Lock()
				f.StreamCancels++
				f.mu.Unlock()
				return err
			}
		}
		if ctx.Err() != nil {
			f.mu.Lock()
			f.StreamCancels++
			f.mu.Unlock()
			return ctx.Err()
		}
		if rec.Container == "" {
			rec.Container = req.Container
		}
		if rec.PodName == "" {
			rec.PodName = req.PodName
		}
		if req.Timestamps {
			rec.Content = serverTimestamp(rec.ReceivedAt) + " " + rec.Content
		}
		if err := cb(rec); err != nil {
			return err
		}
	}
	return nil // clean finite EOF
}

// serverTimestamp is the stamp the Kubernetes API puts in front of each
// line when a log request asks for timestamps: RFC 3339 in UTC with a fixed
// nine-digit fraction. The fake stamps a record with its ReceivedAt, which
// the demo sets to the moment the line was written.
func serverTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// StreamCancelCount returns the number of streams ended via context
// cancellation (thread-safe counter for tests).
func (f *FakeReader) StreamCancelCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.StreamCancels
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Demo data builders -------------------------------------------------------

// SyntheticWorkflow builds one explicitly synthetic workflow for tests and
// demo mode. Name prefixes mark provenance: demo names start with "demo-".
func SyntheticWorkflow(ns, name, phase string, created time.Time) core.Workflow {
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:             core.Ref{Namespace: ns, Name: name, UID: "synthetic-uid-" + name},
			ResourceVersion: "100",
			Phase:           phase,
			CreatedAt:       created,
			Labels:          map[string]string{"workflows.argoproj.io/phase": phase},
		},
		Nodes:          map[string]core.Node{},
		NodesAvailable: true,
		Resource:       []byte(fmt.Sprintf(`{"metadata":{"name":%q,"namespace":%q},"synthetic":true}`, name, ns)),
	}
	return wf
}

func ptrTime(t time.Time) *time.Time { return &t }

// Namespaces, when set, is what ListNamespaces answers. It lets the namespace
// picker be exercised without a server.
//
// FakeReader always implements core.NamespaceLister: a Reader that cannot
// answer returns an empty list, which is a state the picker has to render
// honestly anyway.
func (f *FakeReader) ListNamespaces(ctx context.Context) ([]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NamespacesErr != nil {
		return nil, "", f.NamespacesErr
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(f.Namespaces))
	for _, n := range append(append([]string(nil), f.Namespaces...), namespacesOf(f.Workflows)...) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, "fake reader", nil
}

func namespacesOf(wfs map[core.Ref]core.Workflow) []string {
	out := make([]string, 0, len(wfs))
	for ref := range wfs {
		out = append(out, ref.Namespace)
	}
	return out
}

var _ core.NamespaceLister = (*FakeReader)(nil)
