package app

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
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
	ctx, cancel := context.WithCancel(context.Background())
	m.setInflight("watch", cancel)
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	ch := make(chan any, watchQueueCap)
	go func() {
		err := m.deps.watcher.Watch(ctx, core.WatchRequest{
			Namespace:       m.deps.namespace,
			ResourceVersion: m.watchRV,
		}, func(event core.WatchEvent) error {
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
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil
	}
	e := msg.Event
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
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil
	}
	m.clearInflight("watch")
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
			delay := time.Second
			if ae.RetryAfter != nil && *ae.RetryAfter > delay {
				delay = *ae.RetryAfter
			}
			if delay > 5*time.Minute {
				m.watchMode = "rate limited; refresh required"
				return nil
			}
			g := genStamp{Conn: m.connGen, Sel: m.selGen}
			return tea.Tick(delay, func(time.Time) tea.Msg { return watchRetryMsg{genStamp: g} })
		}
	}
	m.watchMode = "polling fallback"
	m.watchRetries = 0
	// A closed/expired stream must not make the viewer unusable. Relist now;
	// the normal tick continues periodic reconciliation after this completes.
	return m.startListGeneration()
}
