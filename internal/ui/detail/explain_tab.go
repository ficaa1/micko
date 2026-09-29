package detail

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
)

// explain_tab.go is the Explain section's state: the report, the log
// evidence the root reads for it, the scroll position, the status line and
// the hints. The section never reads anything itself. It says which pod's
// log it wants (ExplainLogWanted), and the root reads it and hands the lines
// back (SetExplainLog), stamped with the request they answer.

// explainState is the Explain section's state for the workflow on screen.
type explainState struct {
	report diagnose.Report
	// at is the clock time the report was worked out at. stale marks a
	// report the workflow or the log evidence has changed under.
	at    time.Time
	stale bool
	// top is the first line on screen.
	top int
	// log is the log evidence: nil until a read is started, then loading,
	// then read or failed. logReq is the request it belongs to; a reply for
	// any other request is stale.
	log    *diagnose.Log
	logReq uint64
}

// ExplainLogIntent is what the Explain section wants read: the end of one
// pod's log, as evidence for the first failure.
type ExplainLogIntent struct {
	NodeID    string
	PodName   string
	Container string
	TailLines int
}

// explainWorkflow is the workflow as the rules read it: the parts the pane
// keeps, with the raw resource for the spec's deadline and suspend flag.
func (m *Model) explainWorkflow() core.Workflow {
	wf := m.workflow()
	wf.Resource = m.rawResource
	wf.NodesUnavailableReason = m.state.Outline.UnavailableReason
	return wf
}

// explainReport is the report for the workflow now on screen. It is worked
// out again when the workflow or the log evidence changed, and when the
// clock has moved a second, so a running workflow's times stay current.
func (m *Model) explainReport() diagnose.Report {
	if m.ex.stale || m.ex.at.IsZero() || m.now.Sub(m.ex.at) >= time.Second || m.now.Before(m.ex.at) {
		in := explainInput(m.state.Outline, m.explainWorkflow(), m.now)
		in.Log = m.ex.log
		if in.Log == nil {
			// The live section always reads a log for the first failure,
			// so before the read starts the card already says one is
			// coming rather than showing no log at all.
			in.Log = &diagnose.Log{State: diagnose.LogLoading, Tail: diagnose.LogTail}
		}
		m.ex.report = diagnose.Explain(in)
		m.ex.at, m.ex.stale = m.now, false
	}
	return m.ex.report
}

// explainChanged marks the report stale after the workflow changed.
func (m *Model) explainChanged() { m.ex.stale = true }

// ExplainLogWanted reports the log the Explain section wants read, and
// false when it wants none: another section is on screen, no workflow is
// loaded, nothing failed in a pod whose name is known, or a read for that
// pod is already under way or done.
func (m *Model) ExplainLogWanted() (ExplainLogIntent, bool) {
	if m.tab != "explain" || !m.loaded || m.loading || m.notFound || m.lastErr != "" {
		return ExplainLogIntent{}, false
	}
	r := m.explainReport()
	if r.LogPod == "" || (m.ex.log != nil && m.ex.log.PodName == r.LogPod) {
		return ExplainLogIntent{}, false
	}
	return ExplainLogIntent{NodeID: r.LogNode, PodName: r.LogPod, Container: "main", TailLines: diagnose.LogTail}, true
}

// StartExplainLog records that request id is reading the log in intent.
func (m *Model) StartExplainLog(id uint64, intent ExplainLogIntent) {
	m.ex.log = &diagnose.Log{
		NodeID: intent.NodeID, PodName: intent.PodName, Container: intent.Container,
		State: diagnose.LogLoading, Tail: intent.TailLines,
	}
	m.ex.logReq = id
	m.ex.stale = true
}

// SetExplainLog applies a finished read: the lines, or why it failed and
// whether the server said the pod or its log is gone. It reports false,
// changing nothing, for a reply to any request but the latest.
func (m *Model) SetExplainLog(id uint64, lines []string, errText string, gone bool) bool {
	if m.ex.log == nil || id != m.ex.logReq || m.ex.log.State != diagnose.LogLoading {
		return false
	}
	l := *m.ex.log
	l.Lines = lines
	if errText != "" {
		l.State, l.Err, l.Gone = diagnose.LogFailed, errText, gone
	} else {
		l.State = diagnose.LogRead
	}
	m.ex.log = &l
	m.ex.stale = true
	return true
}

// StopExplainLog forgets a read that is still loading, because the root
// canceled it. The section asks for it again when it is next shown.
func (m *Model) StopExplainLog() {
	if m.ex.log != nil && m.ex.log.State == diagnose.LogLoading {
		m.ex.log = nil
		m.ex.stale = true
	}
}

// explainLines is the section's scrolling content at the pane's width.
func (m *Model) explainLines() []string {
	r := m.explainReport()
	return newExplainRenderer(r, m.theme, m.width, m.revealResource, false).cards(r)
}

// explainRawLines is the plain-text report the full-screen view shows and
// y copies.
func (m *Model) explainRawLines() []string {
	return explainReportLines(m.explainWorkflow(), m.explainReport(), m.revealResource, m.now)
}

// explainStatusLine counts the findings by severity, says when the log
// evidence is still being read, and gives the scroll position.
func (m *Model) explainStatusLine() string {
	r := m.explainReport()
	parts := []string{"explain", explainCounts(r)}
	if l := m.ex.log; l != nil && l.State == diagnose.LogLoading && l.PodName != "" {
		parts = append(parts, "reading the log of "+oneLine(m.nodeName(l.NodeID))+"…")
	}
	if !m.revealResource && explainHasParams(r) {
		parts = append(parts, "values redacted (v reveals)")
	}
	if n := m.scrollLines(); n > m.viewRows() {
		parts = append(parts, strconv.Itoa(m.ex.top+1)+"/"+strconv.Itoa(n))
	}
	s := strings.Join(parts, " · ")
	if m.width > 0 {
		s = truncCell(s, m.width)
	}
	return m.theme.Dim.Render(s)
}

// explainHasParams reports whether any finding shows parameter values.
func explainHasParams(r diagnose.Report) bool {
	for _, f := range r.Findings {
		for _, ev := range f.Evidence {
			if len(ev.Params) > 0 {
				return true
			}
		}
	}
	return false
}

// nodeName is a node's display name, or its ID when the map lacks it.
func (m *Model) nodeName(id string) string {
	if n, ok := m.nodeMap[id]; ok && n.DisplayName != "" {
		return n.DisplayName
	}
	return id
}

// explainLogsCmd opens the full log of the pod the first failure's card
// quotes, the pod the section reads its evidence from.
func (m *Model) explainLogsCmd() tea.Cmd {
	r := m.explainReport()
	if r.LogPod == "" {
		return nil
	}
	intent := NodeLogsIntent{NodeID: r.LogNode, Name: m.nodeName(r.LogNode), PodName: r.LogPod}
	return func() tea.Msg { return intent }
}

// explainHints is the footer for the Explain section.
func (m *Model) explainHints() string {
	h := []string{"tab section", "1-9 jump", "j/k scroll", "y copy report"}
	if r := m.explainReport(); r.LogPod != "" {
		h = append(h, "l failing log")
	}
	h = append(h, "v reveal", "a actions", "r refresh", "f raw", "esc back")
	return strings.Join(h, "  ")
}
