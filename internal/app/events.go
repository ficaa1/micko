package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// events.go streams the Kubernetes events of the workflow on screen into the
// detail pane's Events section, while the section is open.
//
// Two streams run side by side. Kubernetes accepts only equality terms
// joined by AND in an event field selector, so no one selector names a
// workflow and each of its pods, and events carry no labels to select the
// pods by. One stream per pod would open a connection for every pod of a
// fan-out and would need opening again as pods appear. So one stream asks
// for the workflow's own events (involvedObject.kind=Workflow and its
// name), and one for the namespace's pod events (involvedObject.kind=Pod),
// which the pane matches to the workflow's pods by the pod names in its
// node map.
//
// Each stream follows the workflow watch's rules: it resumes from the last
// resource version it delivered after the connection ends, backs off
// between attempts (watchRetryDelay, at most maxWatchRetries in a row
// without an event between them), starts over when the cursor has expired,
// and stops for good on a permission error or a server with no event
// stream. Replies carry the connection and selection generations and the
// stream's attempt, so a stream that was replaced changes nothing.

// eventStreams are the two streams, by index.
const (
	eventStreamWorkflow = iota
	eventStreamPods
	eventStreamCount
)

// eventPurposes are the inflight keys of the two streams.
var eventPurposes = [eventStreamCount]string{"events-workflow", "events-pods"}

// eventQueueCap bounds the events queued between the stream and the update
// loop. A stream that outruns the loop past it is restarted rather than
// allowed to grow the queue.
const eventQueueCap = 512

// eventBatchCap bounds the events per delivered message, so the replay of
// an hour of events renders a few times, not once per event.
const eventBatchCap = 128

// eventStreamState is how one stream is doing.
type eventStreamState int

const (
	eventIdle eventStreamState = iota
	eventLive
	eventRetrying
	eventStopped
)

// eventStream is one stream's bookkeeping.
type eventStream struct {
	state eventStreamState
	// attempt numbers the stream's starts; a reply for another is stale.
	attempt uint64
	// rv is the resource version of the last event delivered, where a
	// reconnect resumes.
	rv      string
	retries int
	// reason is why a stopped stream stopped, or when a retrying one
	// tries again.
	reason string
}

// eventsSession is the events being streamed for one workflow.
type eventsSession struct {
	ref     core.Ref
	active  bool
	streams [eventStreamCount]eventStream
}

type eventsMsg struct {
	genStamp
	Stream int
	Events []core.Event
}

type eventsDoneMsg struct {
	genStamp
	Stream int
	Err    error
}

type eventsRetryMsg struct {
	genStamp
	Stream int
}

// syncEvents starts the streams the Events section wants and stops the
// streams nobody is looking at. It runs after the same changes as
// syncExplainLog.
func (m *Root) syncEvents() tea.Cmd {
	if m.route != RouteDetail || m.detailView == nil || m.detailView.Section() != shared.SectionEvents {
		m.stopEvents()
		return nil
	}
	intent, ok := m.detailView.EventsWanted()
	if !ok {
		return nil
	}
	if m.events.active && m.events.ref == intent.Ref {
		return nil
	}
	m.stopEvents()
	m.events = eventsSession{ref: intent.Ref, active: true}
	if m.deps.eventWatcher == nil {
		for i := range m.events.streams {
			m.events.streams[i].state = eventStopped
		}
		m.detailView.SetEventsStatus("this backend has no event stream", true)
		return nil
	}
	cmds := make([]tea.Cmd, 0, eventStreamCount)
	for i := 0; i < eventStreamCount; i++ {
		cmds = append(cmds, m.startEventStream(i))
	}
	m.reportEvents()
	return tea.Batch(cmds...)
}

// stopEvents cancels both streams.
func (m *Root) stopEvents() {
	if !m.events.active {
		return
	}
	for _, p := range eventPurposes {
		m.cancelInflight(p)
	}
	m.events = eventsSession{}
}

// startEventStream opens stream i from its last resource version.
func (m *Root) startEventStream(i int) tea.Cmd {
	st := &m.events.streams[i]
	st.attempt++
	st.state = eventLive
	st.reason = ""
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}
	m.setInflight(eventPurposes[i], st.attempt, cancel)
	ref := m.events.ref
	req := core.EventWatchRequest{Namespace: ref.Namespace, ResourceVersion: st.rv}
	switch i {
	case eventStreamWorkflow:
		req.FieldSelector = "involvedObject.kind=Workflow,involvedObject.name=" + ref.Name
	default:
		req.FieldSelector = "involvedObject.kind=Pod"
	}
	watcher := m.deps.eventWatcher
	ch := make(chan any, eventQueueCap)
	go func() {
		err := watcher.WatchEvents(ctx, req, func(e core.Event) error {
			select {
			case ch <- e:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			default:
				return errors.New("event queue overflow; the stream starts over")
			}
		})
		select {
		case ch <- eventsDoneMsg{genStamp: g, Stream: i, Err: err}:
		case <-ctx.Done():
		default:
			// The queue is full after an overflow; closing it ends the
			// drain, which reports the stream done.
		}
		close(ch)
	}()
	return drainEventsCmd(ctx, g, i, ch)
}

// drainEventsCmd delivers queued events in batches and chains itself until
// the stream ends.
func drainEventsCmd(ctx context.Context, g genStamp, i int, ch chan any) tea.Cmd {
	return func() tea.Msg {
		var batch []core.Event
		take := func(item any) tea.Msg {
			switch v := item.(type) {
			case core.Event:
				batch = append(batch, v)
				return nil
			case eventsDoneMsg:
				return v
			default:
				return eventsDoneMsg{genStamp: g, Stream: i, Err: errors.New("invalid event queue item")}
			}
		}
		select {
		case <-ctx.Done():
			return eventsDoneMsg{genStamp: g, Stream: i, Err: ctx.Err()}
		case item, ok := <-ch:
			if !ok {
				return eventsDoneMsg{genStamp: g, Stream: i, Err: errors.New("event queue closed")}
			}
			if done := take(item); done != nil {
				return done
			}
		}
		for len(batch) < eventBatchCap {
			select {
			case item, ok := <-ch:
				if !ok {
					return tea.BatchMsg{
						func() tea.Msg { return eventsMsg{genStamp: g, Stream: i, Events: batch} },
						func() tea.Msg { return eventsDoneMsg{genStamp: g, Stream: i, Err: errors.New("event queue closed")} },
					}
				}
				if done := take(item); done != nil {
					return tea.BatchMsg{
						func() tea.Msg { return eventsMsg{genStamp: g, Stream: i, Events: batch} },
						func() tea.Msg { return done },
					}
				}
				continue
			default:
			}
			break
		}
		return tea.BatchMsg{
			func() tea.Msg { return eventsMsg{genStamp: g, Stream: i, Events: batch} },
			drainEventsCmd(ctx, g, i, ch),
		}
	}
}

// eventReplyCurrent reports whether a reply belongs to the stream now
// running.
func (m *Root) eventReplyCurrent(g genStamp, i int) bool {
	return m.events.active && i >= 0 && i < eventStreamCount &&
		g.Conn == m.connGen && g.Sel == m.selGen && g.Attempt == m.events.streams[i].attempt
}

// handleEvents hands a batch to the pane and moves the stream's cursor.
func (m *Root) handleEvents(msg eventsMsg) tea.Cmd {
	if !m.eventReplyCurrent(msg.genStamp, msg.Stream) {
		return nil
	}
	st := &m.events.streams[msg.Stream]
	for _, e := range msg.Events {
		if e.ResourceVersion != "" {
			st.rv = e.ResourceVersion
		}
	}
	if st.retries > 0 && len(msg.Events) > 0 {
		st.retries = 0
	}
	if m.detailView != nil {
		m.detailView.ApplyEvents(msg.Events)
	}
	return nil
}

// handleEventsDone decides what an ended stream does next.
func (m *Root) handleEventsDone(msg eventsDoneMsg) tea.Cmd {
	if !m.eventReplyCurrent(msg.genStamp, msg.Stream) {
		return nil
	}
	i := msg.Stream
	st := &m.events.streams[i]
	m.clearInflight(eventPurposes[i], st.attempt)
	if errors.Is(msg.Err, context.Canceled) {
		return nil
	}
	defer m.reportEvents()
	var we *core.WatchError
	ae := core.AsAPIError(msg.Err)
	switch {
	case ae != nil && (ae.Kind == core.ErrUnauthenticated || ae.Kind == core.ErrForbidden):
		st.state = eventStopped
		st.reason = "not allowed to watch events in " + m.events.ref.Namespace
		return nil
	case (errors.As(msg.Err, &we) && we.Kind == core.WatchUnsupported) || (ae != nil && ae.Kind == core.ErrNotFound):
		st.state = eventStopped
		st.reason = "this Argo server does not stream events"
		return nil
	case errors.As(msg.Err, &we) && we.Kind == core.WatchExpired:
		// The cursor is too old to resume from: start over with the
		// events that exist now. The pane keeps what it has and replaces
		// events the replay sends again.
		st.rv = ""
		return m.startEventStream(i)
	}
	if we != nil && we.LastResourceVersion != "" {
		st.rv = we.LastResourceVersion
	}
	if st.retries >= maxWatchRetries {
		st.state = eventStopped
		st.reason = "stream stopped after " + strconv.Itoa(st.retries) + " failed reconnects (r retries)"
		return nil
	}
	st.retries++
	var retryAfter *time.Duration
	if ae != nil {
		retryAfter = ae.RetryAfter
	}
	delay := watchRetryDelay(st.retries, retryAfter)
	st.state = eventRetrying
	st.reason = "reconnecting in " + shared.ShortDuration(delay.Round(time.Second))
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}
	return tea.Tick(delay, func(time.Time) tea.Msg { return eventsRetryMsg{genStamp: g, Stream: i} })
}

// handleEventsRetry reopens a stream whose back-off has passed.
func (m *Root) handleEventsRetry(msg eventsRetryMsg) tea.Cmd {
	if !m.eventReplyCurrent(msg.genStamp, msg.Stream) || m.events.streams[msg.Stream].state != eventRetrying {
		return nil
	}
	cmd := m.startEventStream(msg.Stream)
	m.reportEvents()
	return cmd
}

// restartEvents starts both streams over, for r on the section: a stream
// that stopped gets another chance.
func (m *Root) restartEvents() tea.Cmd {
	m.stopEvents()
	return m.syncEvents()
}

// reportEvents tells the pane how the streams are doing, the worse of the
// two: a stopped stream, then a reconnecting one, then live.
func (m *Root) reportEvents() {
	if m.detailView == nil || !m.events.active {
		return
	}
	worst := eventIdle
	reason := ""
	for _, st := range m.events.streams {
		if st.state > worst {
			worst, reason = st.state, st.reason
		}
	}
	switch worst {
	case eventStopped:
		m.detailView.SetEventsStatus(reason, true)
	case eventRetrying:
		m.detailView.SetEventsStatus(reason, true)
	default:
		m.detailView.SetEventsStatus("live", false)
	}
}
