package app

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/workflowlist"
)

const watchQueueCap = 128
const maxWatchRetries = 5

type watchEventMsg struct {
	genStamp
	Event core.WatchEvent
}
type watchDoneMsg struct {
	genStamp
	Err error
}
type watchRetryMsg struct{ genStamp }

// startWatch opens one bounded watch. The coordinator, not the transport,
// owns reconnect/relist policy; polling remains active as reconciliation.
func (m *Root) startWatch() tea.Cmd {
	if m.deps.watcher == nil {
		return nil
	}
	m.cancelInflight("watch")
	m.watchMode = "watch"
	m.watchAttempt++
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.watchAttempt}
	m.setInflight("watch", uint64(m.watchAttempt), cancel)
	ch := make(chan any, watchQueueCap)
	// The request is built here, on the update loop, not inside the
	// goroutine. A namespace switch writes both fields from the loop, so
	// reading them in the goroutine is a data race and can open the watch
	// against the namespace the reader just left.
	req := core.WatchRequest{
		Namespace:       m.deps.namespace,
		ResourceVersion: m.watchRV,
	}
	watcher := m.deps.watcher
	go func() {
		err := watcher.Watch(ctx, req, func(event core.WatchEvent) error {
			select {
			case ch <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			default:
				return errors.New("watch event queue overflow; relist required")
			}
		})
		select {
		case ch <- watchDoneMsg{genStamp: g, Err: err}:
		case <-ctx.Done():
		default:
			// The bounded queue is full after an overflow. Closing lets the
			// consumer fall back to polling instead of leaking the goroutine.
		}
		close(ch)
	}()
	return drainWatchCmd(ctx, g, ch)
}

func drainWatchCmd(ctx context.Context, g genStamp, ch chan any) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return watchDoneMsg{genStamp: g, Err: ctx.Err()}
		case item, ok := <-ch:
			if !ok {
				return watchDoneMsg{genStamp: g, Err: errors.New("watch channel closed")}
			}
			switch value := item.(type) {
			case core.WatchEvent:
				return tea.BatchMsg{
					func() tea.Msg { return watchEventMsg{genStamp: g, Event: value} },
					drainWatchCmd(ctx, g, ch),
				}
			case watchDoneMsg:
				return value
			default:
				return watchDoneMsg{genStamp: g, Err: errors.New("invalid watch queue item")}
			}
		}
	}
}

func (m *Root) handleWatchEvent(msg watchEventMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.watchAttempt {
		return nil
	}
	e := msg.Event
	m.watchRetries = 0
	if e.ResourceVersion != "" {
		m.watchRV = e.ResourceVersion
	}
	if e.Type == core.WatchBookmark {
		return nil
	}
	found := -1
	for i := range m.listState.items {
		if m.listState.items[i].Ref.UID == e.Summary.Ref.UID {
			found = i
			break
		}
	}
	switch e.Type {
	case core.WatchDeleted:
		if found >= 0 {
			m.listState.items = append(m.listState.items[:found], m.listState.items[found+1:]...)
		}
	default: // ADDED, MODIFIED, and unknown future types are displayable.
		if found >= 0 {
			m.listState.items[found] = e.Summary
		} else if e.Summary.Ref.UID != "" {
			// The snapshot cap bounds the collected list. A watch that adds
			// past it would grow the snapshot without limit while the view
			// still claimed to hold the whole namespace, so the addition is
			// dropped and the snapshot says it is incomplete instead.
			if cap := m.deps.snapshotCap; cap > 0 && len(m.listState.items) >= cap {
				m.listState.incomplete = true
				m.listView.SetStatus(workflowlist.StatusIncomplete, "", 0)
				return nil
			}
			m.listState.items = append(m.listState.items, e.Summary)
		}
	}
	m.listView.SetItems(m.listState.items, m.deps.clock.Now())
	if m.selection.UID != "" && m.selection.UID == e.Summary.Ref.UID && m.route == RouteDetail {
		return m.startDetailFetch()
	}
	return nil
}

func (m *Root) handleWatchDone(msg watchDoneMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.watchAttempt {
		return nil
	}
	m.clearInflight("watch", uint64(msg.Attempt))
	if errors.Is(msg.Err, context.Canceled) {
		return nil
	}
	if ae := core.AsAPIError(msg.Err); ae != nil {
		switch ae.Kind {
		case core.ErrUnauthenticated, core.ErrForbidden:
			m.watchMode = "authentication/permission required"
			return nil
		case core.ErrRateLimited:
			m.watchMode = "rate limited"
			if m.watchRetries >= maxWatchRetries {
				m.watchMode = "rate limited; refresh required"
				return nil
			}
			m.watchRetries++
			delay := watchRetryDelay(m.watchRetries, ae.RetryAfter)
			if delay > 5*time.Minute {
				m.watchMode = "rate limited; refresh required"
				return nil
			}
			g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.watchAttempt}
			return tea.Tick(delay, func(time.Time) tea.Msg { return watchRetryMsg{genStamp: g} })
		}
	}
	m.watchMode = "polling fallback"
	m.watchRetries = 0
	// A closed/expired stream must not make the viewer unusable. Relist now;
	// the normal tick continues periodic reconciliation after this completes.
	return m.startListGeneration()
}

func watchRetryDelay(retries int, retryAfter *time.Duration) time.Duration {
	if retries < 1 {
		retries = 1
	}
	backoff := time.Second << uint(retries-1)
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	delay := backoff + time.Duration(retries%5)*100*time.Millisecond
	if retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}
	return delay
}
