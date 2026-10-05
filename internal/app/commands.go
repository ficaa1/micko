package app

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/journal"
)

// Clock is the time source; tests and the demo inject a fake one.
type Clock interface {
	Now() time.Time
}

// SystemClock reads the wall clock, so on-screen ages keep moving.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// deps carries the root model's injected collaborators. The root never sees
// the transport.
type deps struct {
	reader   core.Reader
	watcher  core.Watcher
	actioner core.Actioner
	// nsLister answers the namespace picker; nil when the Reader cannot list
	// namespaces.
	nsLister core.NamespaceLister
	// eventWatcher feeds the Events section; nil when the backend has none.
	eventWatcher core.EventWatcher
	// cronLister lists cron workflows; nil when the connection cannot.
	cronLister core.CronLister
	// templateLister and clusterTemplateLister list the two template kinds; nil
	// when the connection cannot.
	templateLister        core.TemplateLister
	clusterTemplateLister core.ClusterTemplateLister
	// archive reads the workflow archive; nil when the connection cannot.
	archive core.ArchiveReader
	// journal records every write attempt. Nil records nothing.
	journal  *journal.Journal
	clock    Clock
	interval time.Duration
	// namespace is the active namespace; switching it bumps the connection
	// generation.
	namespace string
	// allNamespaces widens the list and the watch to every namespace. namespace
	// keeps the one the toggle returns to.
	allNamespaces bool
	// drillNamespace and labelSelector narrow the workflow list to the runs one
	// object owns. Both are empty outside a drill-down.
	drillNamespace string
	labelSelector  string
	// snapshotCap bounds the summaries collected per generation.
	snapshotCap int
	// pageSize is the requested list page size.
	pageSize int64
}

// listNamespace is the namespace the list and the watch ask for; empty means
// every namespace. A drill-down asks in its owner's namespace.
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

// Each async command gets a context the root cancels on a generation bump or
// route change, and stamps its reply so the root can drop stale results.

// listCmd collects one full snapshot page by page.
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
			// Follow the token even past an empty page, but stop on a repeated one.
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

// detailCmd fetches one workflow detail. The UID keeps a same-name replacement
// from being mistaken for the selected workflow.
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

// logSourcesCmd reads one workflow and answers with its pod-to-step map. On
// failure it reports nothing and the log pane keeps pod names.
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

// The pump feeds records and a terminal sentinel through one in-order queue,
// so the end cannot overtake records already queued.

// streamQueueCap bounds queued records; a full queue blocks the stream
// callback, not the UI.
const streamQueueCap = 256

// emitBatchCap bounds the records delivered per message.
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
		// The sentinel shares the queue, so it arrives after every record.
		select {
		case ch <- streamItem{end: true, err: err}:
		case <-ctx.Done():
		}
		close(ch)
	}()
	return d.drainCmd(ctx, g, id, ch)
}

// drainCmd delivers one batch and chains itself until the stream ends.
func (d deps) drainCmd(ctx context.Context, g genStamp, id uint64, ch chan streamItem) func() tea.Msg {
	return func() tea.Msg {
		var batch []core.LogRecord
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

// batchThenEnd delivers records and the end in one message, so the end
// cannot overtake them.
func batchThenEnd(g genStamp, id uint64, batch []core.LogRecord, err error) logRecordMsg {
	msg := terminalLogMsg(g, id, err, false)
	msg.Records = batch
	return msg
}

// tickCmd schedules the next poll.
func (d deps) tickCmd() func() tea.Msg {
	return func() tea.Msg {
		time.Sleep(d.interval)
		return tickMsg{}
	}
}
