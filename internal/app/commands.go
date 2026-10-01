package app

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/journal"
)

// Clock abstracts time for deterministic tests (fake clock injection).
type Clock interface {
	Now() time.Time
}

// SystemClock reads the wall clock. Every relative time on screen — a
// workflow's age, how long a snapshot has been stale — is measured from
// Now, so a clock that does not move freezes all of them at start.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// deps carries the root model's injected collaborators. The Reader is the
// frozen core contract; the root model never knows about transport.
type deps struct {
	reader   core.Reader
	watcher  core.Watcher
	actioner core.Actioner
	// nsLister answers the namespace picker. It is optional: a Reader that
	// cannot list namespaces simply does not implement it.
	nsLister core.NamespaceLister
	// eventWatcher streams Kubernetes events for the Events section. It is
	// optional too: without it the section says the backend has none.
	eventWatcher core.EventWatcher
	// cronLister lists cron workflows. Optional like nsLister: without it the
	// cron route says the connection cannot list them.
	cronLister core.CronLister
	// templateLister and clusterTemplateLister list the two template kinds,
	// optional in the same way.
	templateLister        core.TemplateLister
	clusterTemplateLister core.ClusterTemplateLister
	// archive reads the workflow archive, optional in the same way.
	archive core.ArchiveReader
	// journal records every write attempt. Nil records nothing.
	journal  *journal.Journal
	clock    Clock
	interval time.Duration
	// namespace is the active namespace; switching it bumps the connection
	// generation.
	namespace string
	// allNamespaces widens the list and the watch to every namespace the
	// token may read. namespace keeps the session's own namespace, which the
	// toggle returns to.
	allNamespaces bool
	// drillNamespace and labelSelector narrow the workflow list to the runs
	// one object owns (a cron workflow's, a template's). drillNamespace is
	// that object's namespace, which the list asks for instead of the
	// session's; empty keeps the session's scope. Both are empty outside a
	// drill-down.
	drillNamespace string
	labelSelector  string
	// snapshotCap bounds collected summaries per generation
	// (snapshot cap 5,000; tests/demo may lower it).
	snapshotCap int
	// pageSize is the requested page size (default 100).
	pageSize int64
}

// listNamespace is the namespace the list and the watch ask for. Empty is
// Argo's "every namespace": the path becomes /api/v1/workflows/ with the
// trailing slash, which the server's route matches with an empty namespace.
//
// In a drill-down the owner's namespace wins: a cron workflow's runs live in
// its own namespace, so the list asks there even when the session is looking
// at every namespace.
func (d deps) listNamespace() string {
	if d.drillNamespace != "" {
		return d.drillNamespace
	}
	return d.scopeNamespace()
}

// scopeNamespace is the namespace the session looks at, ignoring any
// drill-down: the one the kind lists ask for. Empty is every namespace.
func (d deps) scopeNamespace() string {
	if d.allNamespaces {
		return ""
	}
	return d.namespace
}

// requestIDProvider hands out monotonically increasing request IDs.
type requestIDProvider struct{ next uint64 }

func (p *requestIDProvider) newID() uint64 {
	p.next++
	return p.next
}

// command constructors -------------------------------------------------------
//
// Each async command receives a context that the root cancels on generation
// bump/route change, plus the generations and request ID to stamp on the
// result message. The root discards stale results (model_test.go pins it).

// listCmd collects one full snapshot page-by-page. One list operation per
// generation; the next timer starts after completion, not concurrently.
func (d deps) listCmd(ctx context.Context, g genStamp, id uint64) func() tea.Msg {
	return func() tea.Msg {
		msg := listLoadedMsg{genStamp: g, RequestID: id}
		var items []core.Summary
		lastRV := ""
		cont := ""
		pages := 0
		maxPages := 0
		if d.pageSize > 0 && d.snapshotCap > 0 {
			maxPages = d.snapshotCap/int(d.pageSize) + 16
		}
		for {
			if err := ctx.Err(); err != nil {
				msg.Canceled = true
				return msg
			}
			page, err := d.reader.List(ctx, core.Query{
				Namespace:     d.listNamespace(),
				LabelSelector: d.labelSelector,
				Continue:      cont,
				Limit:         d.pageSize,
			})
			if err != nil {
				if ctx.Err() != nil {
					msg.Canceled = true
					return msg
				}
				ae := core.AsAPIError(err)
				if ae == nil {
					ae = core.WrapAPIError(core.ErrUnavailable, 0, "list failed: transport error", err)
				}
				msg.Err = ae
				return msg
			}
			items = append(items, page.Items...)
			if page.ResourceVersion != "" {
				lastRV = page.ResourceVersion
			}
			if d.snapshotCap > 0 && len(items) >= d.snapshotCap {
				msg.Capped = true
				break
			}
			if page.Continue == "" {
				break
			}
			// Follow continuation even when a page contains zero items
			// — but bound repeated tokens.
			cont = page.Continue
			pages++
			if maxPages > 0 && pages > maxPages {
				msg.Capped = true
				break
			}
		}
		msg.Page = core.Page{Items: items, ResourceVersion: lastRV}
		msg.Done = true
		return msg
	}
}

// detailCmd fetches one workflow detail. The request carries the UID so the
// server can fall back to the archive for same-name replacements
// (docs/development.md; micko always passes UID on detail GET).
func (d deps) detailCmd(ctx context.Context, g genStamp, id uint64, ref core.Ref) func() tea.Msg {
	return func() tea.Msg {
		wf, err := d.reader.Get(ctx, ref)
		if err != nil {
			if ctx.Err() != nil {
				return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Canceled: true}
			}
			ae := core.AsAPIError(err)
			if ae == nil {
				ae = core.WrapAPIError(core.ErrUnavailable, 0, "get failed: transport error", err)
			}
			return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Err: ae}
		}
		return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Workflow: wf}
	}
}

// logSourcesCmd reads one workflow for its node map and answers with the
// pod-to-step map the log pane labels its lines with. It reports nothing
// on failure: the labels then stay on pod names, which is what they show
// before the answer arrives anyway.
func (d deps) logSourcesCmd(ctx context.Context, g genStamp, id uint64, ref core.Ref) func() tea.Msg {
	return func() tea.Msg {
		wf, err := d.reader.Get(ctx, ref)
		if err != nil || wf.Summary.Ref.UID != ref.UID || !wf.NodesAvailable {
			return logSourcesMsg{genStamp: g, RequestID: id, Ref: ref}
		}
		return logSourcesMsg{genStamp: g, RequestID: id, Ref: ref, Sources: podSources(wf.Nodes)}
	}
}

// podSources maps each pod name to the display name of the node that ran
// it. Nodes without a resolved pod name are left out.
func podSources(nodes map[string]core.Node) map[string]string {
	out := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.PodName != "" && n.DisplayName != "" {
			out[n.PodName] = n.DisplayName
		}
	}
	return out
}

// Log stream plumbing --------------------------------------------------------
//
// The pump runs StreamLogs and feeds one in-order queue whose items are
// either records or a single terminal sentinel. A sentinel (instead of a
// side channel) keeps end-after-records ordering: records queued before the
// stream ends are always delivered first (the earlier side-channel design
// could drop tail records when both were ready simultaneously).
//
// Backpressure: the queue is bounded; sends select on ctx.Done so a slow
// consumer cannot grow it unboundedly.

// streamQueueCap bounds queued records between consumer reads; a full queue
// blocks the StreamLogs callback, not the UI.
const streamQueueCap = 256

// emitBatchCap bounds records per delivered message (root batches delivery
// instead of one full render per line).
const emitBatchCap = 64

type streamItem struct {
	rec core.LogRecord
	// end marks the terminal sentinel; err carries its outcome.
	end bool
	err error
}

// streamLogsCmd starts one stream and returns the first drain command.
func (d deps) streamLogsCmd(ctx context.Context, g genStamp, id uint64, req core.LogRequest) func() tea.Msg {
	ch := make(chan streamItem, streamQueueCap)
	go func() {
		err := d.reader.StreamLogs(ctx, req, func(r core.LogRecord) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- streamItem{rec: r}:
				return nil
			}
		})
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		// Terminal sentinel goes through the same queue, preserving order.
		select {
		case ch <- streamItem{end: true, err: err}:
		case <-ctx.Done():
		}
		close(ch)
	}()
	return d.drainCmd(ctx, g, id, ch)
}

// drainCmd emits one batched message and chains itself until the stream
// ends. Clean finite EOF yields Done with nil Err; context cancellation is
// distinguishable from network failure via Canceled.
func (d deps) drainCmd(ctx context.Context, g genStamp, id uint64, ch chan streamItem) func() tea.Msg {
	return func() tea.Msg {
		var batch []core.LogRecord
		// Block for the first item or cancellation.
		select {
		case <-ctx.Done():
			return logRecordMsg{genStamp: g, RequestID: id, Done: true, Canceled: true}
		case item, ok := <-ch:
			if !ok {
				return terminalLogMsg(g, id, nil, false)
			}
			if item.end {
				return terminalLogMsg(g, id, item.err, false)
			}
			batch = append(batch, item.rec)
		}
		// Opportunistically drain more without blocking.
		for len(batch) < emitBatchCap {
			select {
			case item, ok := <-ch:
				if !ok {
					return batchThenEnd(g, id, batch, nil)
				}
				if item.end {
					return batchThenEnd(g, id, batch, item.err)
				}
				batch = append(batch, item.rec)
			default:
				return logRecordMsg{genStamp: g, RequestID: id, Records: batch, Next: d.drainCmd(ctx, g, id, ch)}
			}
		}
		return logRecordMsg{genStamp: g, RequestID: id, Records: batch, Next: d.drainCmd(ctx, g, id, ch)}
	}
}

// terminalLogMsg builds the terminal message for a finished stream.
func terminalLogMsg(g genStamp, id uint64, err error, canceled bool) logRecordMsg {
	msg := logRecordMsg{genStamp: g, RequestID: id, Done: true, Err: err}
	msg.Canceled = canceled || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	return msg
}

// batchThenEnd returns records and their terminal status together so the end cannot overtake them.
func batchThenEnd(g genStamp, id uint64, batch []core.LogRecord, err error) logRecordMsg {
	msg := terminalLogMsg(g, id, err, false)
	msg.Records = batch
	return msg
}

// tickCmd schedules the next poll after completion (the next
// timer starts after completion).
func (d deps) tickCmd() func() tea.Msg {
	return func() tea.Msg {
		time.Sleep(d.interval)
		return tickMsg{}
	}
}
