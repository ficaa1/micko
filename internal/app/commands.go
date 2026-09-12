package app

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
)

// Clock abstracts time for deterministic tests (fake clock injection).
type Clock interface {
	Now() time.Time
}

// deps carries the root model's injected collaborators. The Reader is the
// frozen core contract; the root model never knows about transport.
type deps struct {
	reader   core.Reader
	watcher  core.Watcher
	actioner core.Actioner
	// nsLister answers the namespace picker. It is optional: a Reader that
	// cannot list namespaces simply does not implement it.
	nsLister core.NamespaceLister
	clock    Clock
	interval time.Duration
	// namespace is the active namespace; switching it bumps the connection
	// generation (plan §2 journey 5).
	namespace string
	// snapshotCap bounds collected summaries per generation (plan §5:
	// snapshot cap 5,000; tests/demo may lower it).
	snapshotCap int
	// pageSize is the requested page size (plan §5: default 100).
	pageSize int64
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
// generation; the next timer starts after completion, not concurrently
// (plan §5; acceptance LIST-07).
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
				Namespace: d.namespace,
				Continue:  cont,
				Limit:     d.pageSize,
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
			// (plan §5) — but bound repeated tokens (LIST-04 guard).
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
// (docs/development.md; argo-tui always passes UID on detail GET).
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

// Log stream plumbing --------------------------------------------------------
//
// The pump runs StreamLogs and feeds one in-order queue whose items are
// either records or a single terminal sentinel. A sentinel (instead of a
// side channel) keeps end-after-records ordering: records queued before the
// stream ends are always delivered first (the earlier side-channel design
// could drop tail records when both were ready simultaneously).
//
// Backpressure: the queue is bounded; sends select on ctx.Done so a slow
// consumer cannot grow it unboundedly (plan §5; STR-03/STR-05).

// streamQueueCap bounds queued records between consumer reads; a full queue
// blocks the StreamLogs callback, not the UI.
const streamQueueCap = 256

// emitBatchCap bounds records per delivered message (root batches delivery
// instead of one full render per line, plan §5).
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
// distinguishable from network failure via Canceled (plan §4; STR-01).
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
					return emitBatchThenEnd(g, id, batch, nil, false)
				}
				if item.end {
					return emitBatchThenEnd(g, id, batch, item.err, false)
				}
				batch = append(batch, item.rec)
			default:
				return emitBatchThenChain(g, id, batch, d.drainCmd(ctx, g, id, ch))
			}
		}
		return emitBatchThenChain(g, id, batch, d.drainCmd(ctx, g, id, ch))
	}
}

// terminalLogMsg builds the terminal message for a finished stream.
func terminalLogMsg(g genStamp, id uint64, err error, canceled bool) logRecordMsg {
	msg := logRecordMsg{genStamp: g, RequestID: id, Done: true, Err: err}
	msg.Canceled = canceled || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	return msg
}

// emitBatchThenChain delivers the batch and chains the next drain command.
func emitBatchThenChain(g genStamp, id uint64, batch []core.LogRecord, next func() tea.Msg) tea.Msg {
	return tea.BatchMsg{
		func() tea.Msg { return logRecordMsg{genStamp: g, RequestID: id, Records: batch} },
		next,
	}
}

// emitBatchThenEnd delivers the batch then the terminal message.
func emitBatchThenEnd(g genStamp, id uint64, batch []core.LogRecord, err error, canceled bool) tea.Msg {
	return tea.BatchMsg{
		func() tea.Msg { return logRecordMsg{genStamp: g, RequestID: id, Records: batch} },
		func() tea.Msg { return terminalLogMsg(g, id, err, canceled) },
	}
}

// tickCmd schedules the next poll after completion (plan §5: the next
// timer starts after completion).
func (d deps) tickCmd() func() tea.Msg {
	return func() tea.Msg {
		time.Sleep(d.interval)
		return tickMsg{}
	}
}
