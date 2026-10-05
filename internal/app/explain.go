package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The Explain section's log evidence is the end of the first failing pod's
// log, read with a bound on lines and time while the section is on screen.

// explainLogTimeout bounds one read, so a server that never ends the
// response cannot leave the section reading.
const explainLogTimeout = 20 * time.Second

// explainLineBytes caps one kept line; the rules show a few hundred
// characters at most.
const explainLineBytes = 4096

// explainLogMsg is a finished read of log evidence.
type explainLogMsg struct {
	genStamp
	RequestID uint64
	Lines     []string
	// Canceled marks a read the root canceled.
	Canceled bool
	Err      error
}

// syncExplainLog starts the read the Explain section wants and cancels one
// the reader has left.
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

// syncSections starts and stops the detail sections' background work: the
// Explain log read and the Events streams.
func (m *Root) syncSections() tea.Cmd {
	return tea.Batch(m.syncExplainLog(), m.syncEvents())
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

// handleExplainLog applies a finished read to the section unless it is stale.
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

// explainLogError returns a failed read's displayable reason and whether the
// pod or its log is gone.
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
// req.TailLines lines, even if the server ignores the tail parameter.
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

// lines returns the kept lines, oldest first.
func (t *lineTail) lines() []string {
	if !t.full {
		return append([]string(nil), t.buf[:t.next]...)
	}
	return append(append([]string(nil), t.buf[t.next:]...), t.buf[:t.next]...)
}
