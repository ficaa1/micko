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
	// logs holds one read per failing pod; at most one is loading, under
	// request logReq.
	logs   []diagnose.Log
	logReq uint64
	// pick indexes explainPicks: the failure whose log l opens.
	pick int
}

// ExplainLogIntent is what the Explain section wants read: the end of one
// pod's log, as evidence for a failure.
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
		// Never nil, so a card whose read has not started says it is coming.
		in.Logs = append([]diagnose.Log{}, m.ex.logs...)
		m.ex.report = diagnose.Explain(in)
		m.ex.at, m.ex.stale = m.now, false
	}
	return m.ex.report
}

// explainChanged marks the report stale after the workflow changed.
func (m *Model) explainChanged() { m.ex.stale = true }

// ExplainLogWanted returns the next failing pod log to read, first failure
// first, and false while another read runs or none is left.
func (m *Model) ExplainLogWanted() (ExplainLogIntent, bool) {
	if m.tab != "explain" || !m.loaded || m.loading || m.notFound || m.lastErr != "" || m.explainLoading() != nil {
		return ExplainLogIntent{}, false
	}
	for _, t := range m.explainReport().LogTargets() {
		if t.PodName != "" && m.explainLog(t.PodName) == nil {
			return ExplainLogIntent{NodeID: t.NodeID, PodName: t.PodName, Container: "main", TailLines: diagnose.LogTail}, true
		}
	}
	return ExplainLogIntent{}, false
}

// explainLog returns the read for pod, or nil.
func (m *Model) explainLog(pod string) *diagnose.Log {
	for i := range m.ex.logs {
		if m.ex.logs[i].PodName == pod {
			return &m.ex.logs[i]
		}
	}
	return nil
}

// explainLoading returns the read under way, or nil.
func (m *Model) explainLoading() *diagnose.Log {
	for i := range m.ex.logs {
		if m.ex.logs[i].State == diagnose.LogLoading {
			return &m.ex.logs[i]
		}
	}
	return nil
}

// StartExplainLog records that request id is reading the log in intent.
func (m *Model) StartExplainLog(id uint64, intent ExplainLogIntent) {
	m.forgetExplainLog(intent.PodName)
	m.ex.logs = append(m.ex.logs, diagnose.Log{
		NodeID: intent.NodeID, PodName: intent.PodName, Container: intent.Container,
		State: diagnose.LogLoading, Tail: intent.TailLines,
	})
	m.ex.logReq = id
	m.ex.stale = true
}

// SetExplainLog applies a finished read: the lines, or why it failed and
// whether the server said the pod or its log is gone. It reports false,
// changing nothing, for a reply to any request but the latest.
func (m *Model) SetExplainLog(id uint64, lines []string, errText string, gone bool) bool {
	l := m.explainLoading()
	if l == nil || id != m.ex.logReq {
		return false
	}
	l.Lines = lines
	if errText != "" {
		l.State, l.Err, l.Gone = diagnose.LogFailed, errText, gone
	} else {
		l.State = diagnose.LogRead
	}
	m.ex.stale = true
	return true
}

// StopExplainLog forgets a read that is still loading, because the root
// canceled it. The section asks for it again when it is next shown.
func (m *Model) StopExplainLog() {
	if l := m.explainLoading(); l != nil {
		m.forgetExplainLog(l.PodName)
	}
}

// forgetExplainLog drops the log evidence for pod.
func (m *Model) forgetExplainLog(pod string) {
	logs := m.ex.logs[:0:0]
	for _, l := range m.ex.logs {
		if l.PodName != pod {
			logs = append(logs, l)
		}
	}
	m.ex.logs = logs
	m.ex.stale = true
}

// explainLines is the section's scrolling content at the pane's width.
func (m *Model) explainLines() []string {
	lines, _ := m.explainCards()
	return lines
}

// explainCards returns the section's lines and each card's first line.
func (m *Model) explainCards() ([]string, []int) {
	r := m.explainReport()
	er := newExplainRenderer(r, m.theme, m.width, m.revealResource, false)
	er.picked = -1
	if picks := m.explainPicks(); len(picks) > 1 {
		er.picked = picks[m.explainPick()]
	}
	return er.cards(r)
}

// explainPicks returns the indexes of findings whose log l can open.
func (m *Model) explainPicks() []int {
	var out []int
	for i, f := range m.explainReport().Findings {
		if f.LogTarget.PodName != "" {
			out = append(out, i)
		}
	}
	return out
}

// explainPick returns the pick clamped to explainPicks.
func (m *Model) explainPick() int {
	return min(m.ex.pick, max(len(m.explainPicks())-1, 0))
}

// stepExplainPick moves the pick, wrapping, and scrolls its card into view.
func (m *Model) stepExplainPick(delta int) {
	picks := m.explainPicks()
	if len(picks) < 2 {
		return
	}
	m.ex.pick = (m.explainPick() + delta + len(picks)) % len(picks)
	_, starts := m.explainCards()
	start := starts[picks[m.ex.pick]]
	end := len(m.explainLines())
	if f := picks[m.ex.pick] + 1; f < len(starts) {
		end = starts[f] - 1
	}
	if start < m.ex.top || end > m.ex.top+m.viewRows() {
		m.jumpTo(start)
	}
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
	if l := m.explainLoading(); l != nil && l.PodName != "" {
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

// explainLogsCmd opens the picked failure's full pod log.
func (m *Model) explainLogsCmd() tea.Cmd {
	picks := m.explainPicks()
	if len(picks) == 0 {
		return nil
	}
	t := m.explainReport().Findings[picks[m.explainPick()]].LogTarget
	intent := NodeLogsIntent{NodeID: t.NodeID, Name: m.nodeName(t.NodeID), PodName: t.PodName}
	return func() tea.Msg { return intent }
}

// explainHints is the footer for the Explain section.
func (m *Model) explainHints() string {
	h := []string{"tab section", "1-9 jump", "j/k scroll", "y copy report"}
	if picks := m.explainPicks(); len(picks) > 0 {
		h = append(h, "l failing log")
		if len(picks) > 1 {
			h = append(h, "n/N pick failure")
		}
	}
	h = append(h, "v reveal", "a actions", "r refresh", "f raw", "esc back")
	return strings.Join(h, "  ")
}

// handleExplainKey handles n and N, which pick the failure l opens.
func (m *Model) handleExplainKey(key string) {
	switch key {
	case "n":
		m.stepExplainPick(1)
	case "N":
		m.stepExplainPick(-1)
	}
}
