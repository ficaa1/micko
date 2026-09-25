package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// explain.go reads the log evidence for the detail pane's Explain section:
// the end of the first failing pod's log. The read is bounded (a fixed
// number of lines, no follow, a time limit), runs only while the section is
// on screen, and is canceled when the reader leaves the section or the
// workflow. Its reply carries the generations and the request ID, so a
// reply for a workflow or a pod the pane has moved on from changes nothing.

// explainLogTimeout bounds one read. A server that never ends the response
// would otherwise leave the section saying it is reading for good.
const explainLogTimeout = 20 * time.Second

// explainLineBytes bounds one kept line. The rules show a few hundred
// characters of a line at most, and a pod that prints megabyte lines must not
// hold a megabyte per line here.
const explainLineBytes = 4096

// explainLogMsg is a finished read of log evidence.
type explainLogMsg struct {
	genStamp
	RequestID uint64
	Lines     []string
	// Canceled marks a read the root canceled; its section has already
	// forgotten it.
	Canceled bool
	Err      error
}

// syncExplainLog starts the read the Explain section wants, and cancels a
// read the reader has left the section behind. It runs after every change
// that can move either: a key in the detail pane, a loaded workflow, a
// workflow opened on a section.
func (m *Root) syncExplainLog() tea.Cmd {
	if m.route != RouteDetail || m.detailView == nil || m.detailView.Section() != shared.SectionExplain {
		m.stopExplainLog()
		return nil
	}
	intent, ok := m.detailView.ExplainLogWanted()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), explainLogTimeout)
	id := m.ids.newID()
	m.setInflight("explain", id, cancel)
	m.detailView.StartExplainLog(id, intent)
	req := core.LogRequest{
		Ref:       m.selection,
		PodName:   intent.PodName,
		Container: intent.Container,
		TailLines: int64(intent.TailLines),
	}
	return m.deps.explainLogCmd(ctx, genStamp{Conn: m.connGen, Sel: m.selGen}, id, req)
}

// stopExplainLog cancels a read under way and tells the section, which asks
// for it again when it is next shown.
func (m *Root) stopExplainLog() {
	if !m.hasInflight("explain") {
		return
	}
	m.cancelInflight("explain")
	if m.detailView != nil {
		m.detailView.StopExplainLog()
	}
}

// handleExplainLog applies a finished read to the section, unless it is
// stale: canceled, or stamped with a workflow or a request the pane has
// moved on from.
func (m *Root) handleExplainLog(msg explainLogMsg) tea.Cmd {
	m.clearInflight("explain", msg.RequestID)
	if msg.Canceled || msg.Conn != m.connGen || msg.Sel != m.selGen || m.detailView == nil {
		return nil
	}
	errText, gone := "", false
	if msg.Err != nil {
		errText, gone = explainLogError(msg.Err)
	}
	m.detailView.SetExplainLog(msg.RequestID, msg.Lines, errText, gone)
	return nil
}

// explainLogError is a failed read's reason, safe to show, and whether the
// server said the pod or its log no longer exists.
func explainLogError(err error) (string, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "the read timed out after " + explainLogTimeout.String(), false
	}
	msg := err.Error()
	gone := strings.Contains(strings.ToLower(msg), "not found")
	if ae := core.AsAPIError(err); ae != nil {
		msg = ae.Message
		gone = gone || ae.Kind == core.ErrNotFound
	}
	return shared.Sanitize(shared.RedactTokens(msg)), gone
}

// explainLogCmd reads the end of one pod's log and keeps its last
// req.TailLines lines. It keeps only that many whatever the server sends, so
// a server that ignores the tail parameter costs time, not memory.
func (d deps) explainLogCmd(ctx context.Context, g genStamp, id uint64, req core.LogRequest) tea.Cmd {
	return func() tea.Msg {
		tail := newLineTail(int(req.TailLines))
		err := d.reader.StreamLogs(ctx, req, func(r core.LogRecord) error {
			tail.add(r.Content)
			return nil
		})
		msg := explainLogMsg{genStamp: g, RequestID: id, Lines: tail.lines()}
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			msg.Canceled = true
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			msg.Err = context.DeadlineExceeded
		case err != nil:
			msg.Err = err
		}
		return msg
	}
}

// lineTail keeps the last n lines added to it, each cut to
// explainLineBytes.
type lineTail struct {
	buf  []string
	next int
	full bool
}

func newLineTail(n int) *lineTail {
	return &lineTail{buf: make([]string, max(n, 1))}
}

func (t *lineTail) add(s string) {
	if len(s) > explainLineBytes {
		s = strings.ToValidUTF8(s[:explainLineBytes], "") + "…"
	}
	t.buf[t.next] = s
	t.next++
	if t.next == len(t.buf) {
		t.next, t.full = 0, true
	}
}

// lines is what the tail holds, oldest first.
func (t *lineTail) lines() []string {
	if !t.full {
		return append([]string(nil), t.buf[:t.next]...)
	}
	return append(append([]string(nil), t.buf[t.next:]...), t.buf[:t.next]...)
}
