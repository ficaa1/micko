package detail

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// explain.go draws the Explain section: the findings of the diagnose rules
// as cards, one per finding, most severe first.
//
//	✗ ERROR  transform failed all 3 attempts: exit code 1 each time
//	│ node       transform(2) · Pod · template transform
//	│ when       failed 3m40s into the run, after running 50s
//	│ attempts   transform(0) failed exit 1 after 40s · transform(1) …
//	│ log        the lines that matter from the last 7 lines of transform(2)
//	│              2  applying 14 transforms
//	│            > 3  ERROR column 'revenue_eur' has 312 nulls
//	│ next       Retrying did not help: …
//
// The severity is a glyph and a word as well as a colour, so a mono terminal
// and NO_COLOR keep it. Every string from the server passes the sanitizer
// on its way to the screen, and parameter values follow the pane's reveal
// state, as in the resource tab and the info panel.

// explainInput hands the rules the tree in pipeline order with what each
// node waited for (OutlineRow.After) and when each ran, measured the way the
// bars measure it (nodeInterval), and the run's start as the Timeline's
// span sees it (workflowSpan). Nodes the tree could not place are added as
// trees of their own, so a failure among them is still found.
func explainInput(out Outline, wf core.Workflow, now time.Time) diagnose.Input {
	in := diagnose.Input{Workflow: wf, Start: workflowSpan(wf, now).start, Now: now}
	var branch func(r OutlineRow) diagnose.Branch
	branch = func(r OutlineRow) diagnose.Branch {
		start, end, _, ran := nodeInterval(r, now)
		b := diagnose.Branch{ID: r.NodeID, After: r.After, Role: r.Role, Ran: ran, Start: start, End: end}
		for _, c := range r.Children {
			b.Children = append(b.Children, branch(c))
		}
		return b
	}
	for _, rows := range [][]OutlineRow{out.Rows, out.Unreachable} {
		for _, r := range rows {
			in.Tree = append(in.Tree, branch(r))
		}
	}
	return in
}

// Severity glyphs. Each is one cell and differs in shape, so the severity
// reads without colour; the word beside it says it outright.
const (
	glyphError   = "✗"
	glyphWarning = "▲"
	glyphInfo    = "◇"
	// logMatch marks a log line picked for what it says, as opposed to
	// the context line before it.
	logMatch = ">"
)

// explainLabelMax bounds the evidence label column.
const explainLabelMax = 13

// explainRenderer lays findings out as cards for one width. A zero width
// is unbounded: the report for the clipboard and the full-screen view,
// which wraps nothing.
type explainRenderer struct {
	theme  shared.Theme
	width  int
	reveal bool
	// gutter starts every line of a card after its first: a rule in the
	// severity's colour on the pane, spaces in the plain report.
	gutter string
	labelW int
	// picked is the finding whose card l opens, or -1.
	picked int
}

func newExplainRenderer(r diagnose.Report, theme shared.Theme, width int, reveal, raw bool) explainRenderer {
	er := explainRenderer{theme: theme, width: width, reveal: reveal, gutter: "│ ", picked: -1}
	if raw {
		er.gutter = "  "
	}
	er.labelW = cellWidth("next")
	for _, f := range r.Findings {
		for _, ev := range f.Evidence {
			er.labelW = max(er.labelW, cellWidth(oneLine(ev.Label)))
		}
	}
	er.labelW = min(er.labelW, explainLabelMax)
	return er
}

func severityGlyph(s diagnose.Severity) string {
	switch s {
	case diagnose.Error:
		return glyphError
	case diagnose.Warning:
		return glyphWarning
	default:
		return glyphInfo
	}
}

func (er explainRenderer) severityStyle(s diagnose.Severity) lipgloss.Style {
	switch s {
	case diagnose.Error:
		return er.theme.ErrorText
	case diagnose.Warning:
		return er.theme.Warning
	default:
		return er.theme.Accent
	}
}

// severityTag is the glyph and the severity's word, in capitals.
func severityTag(s diagnose.Severity) string {
	return severityGlyph(s) + " " + strings.ToUpper(s.String())
}

// wrapCells wraps s to width cells at word boundaries, cutting a word wider
// than the line. A zero width does not wrap.
func wrapCells(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var out []string
	for _, l := range strings.Split(shared.Wrap(s, width), "\n") {
		out = append(out, hardWrap(l, width)...)
	}
	return out
}

// finish renders a line, cut to the width.
func (er explainRenderer) finish(p pieces) string {
	if er.width > 0 {
		p = p.truncate(er.width)
	}
	return p.trimRight().render()
}

// cards renders every finding and returns the line each card starts on.
func (er explainRenderer) cards(r diagnose.Report) ([]string, []int) {
	var out []string
	starts := make([]int, 0, len(r.Findings))
	for i, f := range r.Findings {
		if i > 0 {
			out = append(out, "")
		}
		starts = append(starts, len(out))
		c := er
		if i == er.picked {
			c.gutter = "┃ "
		}
		out = append(out, c.card(f)...)
	}
	return out, starts
}

// card renders one finding: the severity and headline, the evidence under a
// label column, and the next step.
func (er explainRenderer) card(f diagnose.Finding) []string {
	t := er.theme
	sev := er.severityStyle(f.Severity)
	tag := severityTag(f.Severity)
	headIndent := cellWidth(tag) + 2
	var out []string
	for i, l := range wrapCells(oneLine(f.Headline), er.width-headIndent) {
		var p pieces
		if i == 0 {
			p.add(tag, sev)
			p.add("  ", lipgloss.NewStyle())
		} else {
			p.add(strings.Repeat(" ", headIndent), lipgloss.NewStyle())
		}
		p.add(l, t.Header)
		out = append(out, er.finish(p))
	}
	for _, ev := range f.Evidence {
		out = append(out, er.evidence(ev, sev)...)
	}
	if f.Next != "" {
		out = append(out, er.labelled("next", t.Accent, oneLine(f.Next), t.Text, sev)...)
	}
	return out
}

// textW is the room for evidence text beside the label column.
func (er explainRenderer) textW() int {
	if er.width <= 0 {
		return 0
	}
	return max(er.width-cellWidth(er.gutter)-er.labelW-1, 12)
}

// labelled renders a label and its text, the text wrapped under itself.
func (er explainRenderer) labelled(label string, labelStyle lipgloss.Style, text string, textStyle, sev lipgloss.Style) []string {
	var out []string
	for i, l := range wrapCells(text, er.textW()) {
		var p pieces
		p.add(er.gutter, sev)
		name := ""
		if i == 0 {
			name = truncCell(label, er.labelW)
		}
		p.add(padCell(name, er.labelW)+" ", labelStyle)
		p.add(l, textStyle)
		out = append(out, er.finish(p))
	}
	return out
}

// evidence renders one fact: its text, its parameters as name=value pairs
// (values hidden unless revealed) and its picked log lines, numbered, with
// the lines picked for what they say marked.
func (er explainRenderer) evidence(ev diagnose.Evidence, sev lipgloss.Style) []string {
	t := er.theme
	text := oneLine(ev.Text)
	if len(ev.Params) > 0 {
		var ps []string
		for _, p := range ev.Params {
			v := redactedMarker
			if er.reveal {
				v = oneLine(p.Value)
			}
			ps = append(ps, oneLine(p.Name)+"="+v)
		}
		text = strings.TrimSpace(text + " " + strings.Join(ps, " · "))
	}
	out := er.labelled(oneLine(ev.Label), t.Muted, text, t.Text, sev)
	numW := 1
	for _, l := range ev.Log {
		numW = max(numW, len(strconv.Itoa(l.N)))
	}
	for _, l := range ev.Log {
		var p pieces
		p.add(er.gutter, sev)
		p.add(strings.Repeat(" ", er.labelW+1), lipgloss.NewStyle())
		mark, style := " ", t.Muted
		if l.Match {
			mark, style = logMatch, sev
		}
		p.add(mark+" ", style)
		p.add(strings.Repeat(" ", numW-len(strconv.Itoa(l.N)))+strconv.Itoa(l.N)+"  ", t.Muted)
		p.add(oneLine(l.Text), t.Text)
		out = append(out, er.finish(p))
	}
	return out
}

// explainCounts is "1 error · 2 info": how many findings of each severity,
// leaving out the severities with none.
func explainCounts(r diagnose.Report) string {
	e, w, i := r.Counts()
	var parts []string
	if e > 0 {
		parts = append(parts, plural(e, "error"))
	}
	if w > 0 {
		parts = append(parts, plural(w, "warning"))
	}
	if i > 0 {
		parts = append(parts, strconv.Itoa(i)+" info")
	}
	if len(parts) == 0 {
		return "no findings"
	}
	return strings.Join(parts, " · ")
}

// explainReportLines is the explanation as plain text, for the clipboard
// and the full-screen view: a line naming the workflow and its outcome, the
// counts, then every card with no colour and no wrapping, ready to paste
// into an incident channel.
func explainReportLines(wf core.Workflow, r diagnose.Report, reveal bool, now time.Time) []string {
	s := wf.Summary
	head := "Explain " + oneLine(s.Ref.Name)
	if s.Ref.Namespace != "" {
		head += " in " + oneLine(s.Ref.Namespace)
	}
	head += ": " + oneLine(phaseName(s.Phase))
	switch {
	case s.StartedAt != nil && s.FinishedAt != nil && s.FinishedAt.After(*s.StartedAt):
		head += " after " + shared.ShortDuration(s.FinishedAt.Sub(*s.StartedAt))
	case s.FinishedAt != nil:
	case s.StartedAt != nil:
		head += " for " + shared.ShortDuration(now.Sub(*s.StartedAt))
	}
	out := []string{head, explainCounts(r), ""}
	er := newExplainRenderer(r, shared.NewTheme(true), 0, reveal, true)
	cards, _ := er.cards(r)
	return append(out, cards...)
}
