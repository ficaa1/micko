package detail

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// node_row.go lays out one row of the nodes tab: the tree and the name, then
// the columns that carry the run facts.
//
//	▾ ● demo-train-pipeline DAG       train        4m00s  ███████████████████·  AWAITING…
//	  ├─ ✓ preprocess                 preprocess   1m10s  ██████··············
//
// A row is assembled as styled pieces and cut to width before any piece is
// styled, so truncation never splits an escape sequence and the plain theme
// and every skin produce the same text.

// cellWidth measures a string in terminal cells, so a wide rune in a node
// name cannot shift the columns out of line.
func cellWidth(s string) int { return ansi.StringWidth(s) }

// piece is a run of text in one style.
type piece struct {
	text  string
	style lipgloss.Style
}

// pieces is a line under construction.
type pieces []piece

func (p *pieces) add(text string, style lipgloss.Style) {
	if text != "" {
		*p = append(*p, piece{text, style})
	}
}

func (p pieces) width() int {
	n := 0
	for _, s := range p {
		n += cellWidth(s.text)
	}
	return n
}

func (p pieces) plain() string {
	var b strings.Builder
	for _, s := range p {
		b.WriteString(s.text)
	}
	return b.String()
}

func (p pieces) render() string {
	var b strings.Builder
	for _, s := range p {
		b.WriteString(s.style.Render(s.text))
	}
	return b.String()
}

// truncate cuts the line to n cells, marking the cut with "…" in the style
// of the piece it lands in.
func (p pieces) truncate(n int) pieces {
	if p.width() <= n {
		return p
	}
	var out pieces
	room := n - 1
	for _, s := range p {
		w := cellWidth(s.text)
		if w <= room {
			out = append(out, s)
			room -= w
			continue
		}
		cut := ansi.Truncate(s.text, room, "")
		out = append(out, piece{cut + "…", s.style})
		return out
	}
	return out
}

// trimRight drops trailing spaces, so a row that ends in padding renders the
// same text in every theme: a styled run of spaces would otherwise survive
// where a plain one is trimmed.
func (p pieces) trimRight() pieces {
	for len(p) > 0 {
		last := p[len(p)-1]
		t := strings.TrimRight(last.text, " ")
		if t != "" {
			p[len(p)-1].text = t
			return p
		}
		p = p[:len(p)-1]
	}
	return p
}

// padTo pads the line with spaces to n cells.
func (p *pieces) padTo(n int) {
	if w := p.width(); w < n {
		p.add(strings.Repeat(" ", n-w), lipgloss.NewStyle())
	}
}

// nodeColumns is the column set for one pane width. A zero width is
// unbounded: every column at its natural width, for the raw full-screen view.
type nodeColumns struct {
	width    int
	name     int
	template int
	duration int
	bar      int
	message  int
}

const (
	colGap = 2
	// durationW fits "DURATION" and every shortDuration value.
	durationW = 8
	// minNameW is the narrowest the name column gets while other columns
	// are shown; they are dropped first.
	minNameW = 24
	// minNameText is the fewest cells of a name a row keeps. A deep row
	// whose name would be cut shorter overflows its column instead.
	minNameText = 12
	// minMessageW is the narrowest message column worth drawing.
	minMessageW = 12
	// rawTemplateW and rawBarW size the unbounded layout.
	rawTemplateW = 24
	rawBarW      = 24
)

// Column tiers, as pane widths. Each is the pane of a terminal 140, 100 or
// 80 columns wide, whose border takes four cells, so the tiers change where
// the terminal width a reader knows says they do.
const (
	tierTemplate = 136
	tierWideBar  = 96
	tierBar      = 76
)

// columnsFor chooses the columns for a pane width. nameNeed and
// templateNeed are the widest name cell and template of the rows. The
// columns come and go by width: the template from tierTemplate, the timing
// bar from tierBar, growing with the pane, and the duration while the name
// keeps minNameW. The name column takes what its rows need, leaving the
// message at least a quarter of what is left (never under minMessageW), and
// the message takes the rest. On a pane under 60 cells the message is the
// first to go, because it no longer fits beside a readable name.
func columnsFor(width, nameNeed, templateNeed int) nodeColumns {
	c := nodeColumns{width: width, duration: durationW}
	if width <= 0 {
		c.name = nameNeed
		c.template = min(templateNeed, rawTemplateW)
		c.bar = rawBarW
		c.message = -1
		return c
	}
	switch {
	case width >= tierTemplate:
		if templateNeed > 0 {
			c.template = min(max(templateNeed, 8), 20)
		}
		// The bar gains a cell for every two the pane grows past the
		// tier, so a wide terminal buys timing resolution as well as
		// message room.
		c.bar = min(24+(width-tierTemplate)/2, 40)
	case width >= tierWideBar:
		c.bar = 20
	case width >= tierBar:
		c.bar = 12
	}
	fixed := func() int {
		n := 0
		for _, w := range []int{c.template, c.duration, c.bar} {
			if w > 0 {
				n += w + colGap
			}
		}
		return n
	}
	for width-fixed() < minNameW {
		switch {
		case c.bar > 0:
			c.bar = 0
		case c.template > 0:
			c.template = 0
		case c.duration > 0:
			c.duration = 0
		}
		if c.bar == 0 && c.template == 0 && c.duration == 0 {
			break
		}
	}
	avail := width - fixed()
	msgMin := max(minMessageW, avail/4)
	name := min(nameNeed, max(avail-colGap-msgMin, min(minNameW, avail)))
	c.name = max(name, 0)
	if m := avail - c.name - colGap; m >= minMessageW {
		c.message = m
	}
	return c
}

// rowRenderer draws rows for one layout.
type rowRenderer struct {
	theme shared.Theme
	cols  nodeColumns
	span  timeSpan
	now   time.Time
}

// nodeGlyph is the row's phase glyph. The gate and skipped rows get their
// own, and a node with no phase yet reads as pending.
func nodeGlyph(r FlatRow) string {
	switch {
	case r.Section != "":
		return "!"
	case r.Suspended():
		return shared.PhaseSymbol("Suspended")
	case r.Skipped() || r.Row.Phase == "Omitted":
		return glyphSkipped
	case r.Row.Phase == "":
		return shared.PhaseSymbol("Pending")
	default:
		return shared.PhaseSymbol(r.Row.Phase)
	}
}

// glyphSkipped marks a node that did not run: skipped by a condition, or
// omitted because what it depended on did not succeed.
const glyphSkipped = "⊘"

func (rr rowRenderer) glyphStyle(r FlatRow) lipgloss.Style {
	switch {
	case r.Section != "":
		return rr.theme.Warning
	case r.Suspended():
		return rr.theme.Warning
	case r.Skipped() || r.Row.Phase == "Omitted":
		return rr.theme.Muted
	default:
		return rr.theme.PhaseStyle(r.Row.Phase)
	}
}

// nodeTag is the short type tag after the name. Only structural nodes carry
// one: a pod row is the common case and a tag on every row would be noise.
// An exit handler or a hook is tagged with its role instead of its type.
func nodeTag(r FlatRow) string {
	if r.Section != "" {
		return ""
	}
	if r.Row.Role != "" {
		return r.Row.Role
	}
	switch r.Row.Type {
	case "", "Pod", "Container":
		return ""
	default:
		return r.Row.Type
	}
}

// depsForms is the dependency annotation of a DAG task in the forms it can
// take, longest first: "← extract"; for a join, the first two names and a
// count, then the first name and a count, then only the count. A narrow
// column steps down through them before it drops the annotation.
func depsForms(deps []string) []string {
	n := len(deps)
	if n == 0 {
		return nil
	}
	first := oneLine(deps[0])
	if n == 1 {
		return []string{"← " + first}
	}
	two := "← " + first + ", " + oneLine(deps[1])
	if n > 2 {
		two += " +" + itoaDetail(n-2)
	}
	return []string{two, "← " + first + " +" + itoaDetail(n-1), "← " + itoaDetail(n) + " tasks"}
}

// nameExtras are the optional parts after a row's name, in drawing order.
// drop is the order they give way in when the column is narrow: the
// dependency annotation first, then the type tag. The retry count, the exit
// code and the fold count are facts the reader cannot get from anywhere else
// on the row, so they stay.
type nameExtra struct {
	text  string
	style lipgloss.Style
	drop  int
	// shorter holds shorter forms of text, tried in order before the
	// extra is dropped.
	shorter []string
}

func (rr rowRenderer) extras(r FlatRow) []nameExtra {
	t := rr.theme
	var out []nameExtra
	if tag := nodeTag(r); tag != "" {
		out = append(out, nameExtra{text: oneLine(tag), style: t.Muted, drop: 2})
	}
	if r.Row.Retries > 0 {
		out = append(out, nameExtra{text: "↻ " + itoaDetail(r.Row.Retries), style: t.Warning})
	}
	if code := oneLine(r.Row.ExitCode); code != "" && code != "0" {
		out = append(out, nameExtra{text: "exit " + code, style: t.PhaseFailed})
	}
	if r.Folded {
		out = append(out, nameExtra{text: "+" + itoaDetail(r.FoldedCount), style: t.Accent})
	}
	if forms := depsForms(r.Row.Deps); len(forms) > 0 {
		out = append(out, nameExtra{text: forms[0], style: t.Muted, drop: 1, shorter: forms[1:]})
	}
	return out
}

// nameCellWidth is the width a row's name cell needs to show everything.
func (rr rowRenderer) nameCellWidth(r FlatRow) int {
	w := cellWidth(r.Indent) + 2 + cellWidth(oneLine(rowDisplayName(r.Row)))
	for _, e := range rr.extras(r) {
		w += 1 + cellWidth(e.text)
	}
	return w
}

// nameCell draws the tree, the glyph, the name and whatever extras fit in
// width cells. The name is cut last and never below minNameText; a row
// that still does not fit overflows, and the caller shifts the rest of that
// row rather than lose the name. An extra with shorter forms steps down
// through them before it is dropped.
func (rr rowRenderer) nameCell(r FlatRow, width int, match bool) pieces {
	t := rr.theme
	name := oneLine(rowDisplayName(r.Row))
	extras := rr.extras(r)
	fixed := cellWidth(r.Indent) + 2
	total := func() int {
		n := fixed + cellWidth(name)
		for _, e := range extras {
			n += 1 + cellWidth(e.text)
		}
		return n
	}
	if width > 0 {
		for i := range extras {
			for len(extras[i].shorter) > 0 && total() > width {
				extras[i].text, extras[i].shorter = extras[i].shorter[0], extras[i].shorter[1:]
			}
		}
		for level := 1; level <= 2 && total() > width; level++ {
			kept := extras[:0:0]
			for _, e := range extras {
				if e.drop != level {
					kept = append(kept, e)
				}
			}
			extras = kept
		}
		if over := total() - width; over > 0 {
			room := max(cellWidth(name)-over, minNameText)
			name = truncCell(name, room)
		}
	}

	var p pieces
	rr.addIndent(&p, r)
	p.add(nodeGlyph(r)+" ", rr.glyphStyle(r))
	nameStyle := t.Text
	switch {
	case match:
		nameStyle = t.Accent
	case r.Skipped():
		nameStyle = t.Muted
	}
	p.add(name, nameStyle)
	for _, e := range extras {
		p.add(" ", lipgloss.NewStyle())
		p.add(e.text, e.style)
	}
	return p
}

// addIndent draws the tree in the guide style, dimmer than the names, and a
// folded marker in the accent, because it stands for rows that are not on
// screen.
func (rr rowRenderer) addIndent(p *pieces, r FlatRow) {
	if !r.Folded {
		p.add(r.Indent, rr.theme.TreeGuide)
		return
	}
	i := strings.LastIndex(r.Indent, markerFolded)
	if i < 0 {
		p.add(r.Indent, rr.theme.TreeGuide)
		return
	}
	p.add(r.Indent[:i], rr.theme.TreeGuide)
	p.add(markerFolded, rr.theme.Accent)
	p.add(r.Indent[i+len(markerFolded):], rr.theme.TreeGuide)
}

// rowMessage is the message column's text and style. The gate states that it
// waits for a person; a skipped node's reason ("when 'false' evaluated
// false") is the same on every such row and stays in the info panel.
func (rr rowRenderer) rowMessage(r FlatRow) (string, lipgloss.Style) {
	t := rr.theme
	msg := oneLine(r.Row.Message)
	switch {
	case r.Suspended():
		return "AWAITING RESUME", t.Warning
	case r.Section != "":
		return msg, t.Warning
	case r.Skipped():
		return "", t.Muted
	case r.Row.Phase == "Failed" || r.Row.Phase == "Error":
		return msg, t.PhaseFailed
	default:
		return msg, t.Muted
	}
}

// line builds the whole row, unstyled pieces included, cut to the pane.
func (rr rowRenderer) line(r FlatRow, match bool) pieces {
	c := rr.cols
	t := rr.theme
	p := rr.nameCell(r, c.name, match)
	p.padTo(c.name)
	gap := strings.Repeat(" ", colGap)
	plainStyle := lipgloss.NewStyle()
	if c.template > 0 {
		p.add(gap, plainStyle)
		tmpl := truncCell(oneLine(r.Row.Template), c.template)
		p.add(padCell(tmpl, c.template), t.Muted)
	}
	if c.duration > 0 {
		p.add(gap, plainStyle)
		txt := ""
		style := t.Muted
		// A skipped node did not run, so it has no duration to state,
		// though its bar still marks when the condition was decided.
		if r.Section == "" && !r.Skipped() {
			if d, ok := nodeDuration(r.Row, rr.now); ok {
				txt = shortDuration(d)
				if _, _, running, _ := nodeInterval(r.Row, rr.now); running {
					style = t.PhaseRunning
				}
			}
		}
		p.add(strings.Repeat(" ", c.duration-cellWidth(txt))+txt, style)
	}
	if c.bar > 0 {
		p.add(gap, plainStyle)
		if r.Section != "" {
			p.add(strings.Repeat(" ", c.bar), plainStyle)
		} else {
			for _, s := range nodeBar(rr.span, r.Row, rr.now, c.bar, barsASCII(t)) {
				p.add(s.text, s.kind.style(t))
			}
		}
	}
	if msg, style := rr.rowMessage(r); msg != "" && c.message != 0 {
		p.add(gap, plainStyle)
		if c.message > 0 {
			msg = truncCell(msg, c.message)
		}
		p.add(msg, style)
	}
	if c.width > 0 {
		p = p.truncate(c.width)
	}
	return p
}

// render draws the row. The selected row is one bar across the pane in the
// selection style, so its pieces give up their own colours.
func (rr rowRenderer) render(r FlatRow, selected, match bool) string {
	p := rr.line(r, match).trimRight()
	if selected {
		return rr.theme.SelectRow(p.plain(), rr.cols.width)
	}
	return p.render()
}

// header is the column heads, aligned with the rows. The timing bar's head
// carries the span it covers, so the bars have a scale.
func (rr rowRenderer) header() string {
	c := rr.cols
	t := rr.theme
	var p pieces
	p.add(padCell("NAME", c.name), t.TableHeader)
	gap := strings.Repeat(" ", colGap)
	if c.template > 0 {
		p.add(gap+padCell("TEMPLATE", c.template), t.TableHeader)
	}
	if c.duration > 0 {
		p.add(gap+padCell("", c.duration-cellWidth("DURATION"))+"DURATION", t.TableHeader)
	}
	if c.bar > 0 {
		label := "TIMELINE"
		scale := ""
		if rr.span.ok() {
			scale = shortDuration(rr.span.length())
		}
		if cellWidth(label)+1+cellWidth(scale) > c.bar {
			label = truncCell(label, c.bar)
			scale = ""
		}
		p.add(gap+label, t.TableHeader)
		p.add(padCell("", c.bar-cellWidth(label)-cellWidth(scale))+scale, t.Muted)
	}
	if c.message != 0 {
		p.add(gap+"MESSAGE", t.TableHeader)
	}
	if c.width > 0 {
		p = p.truncate(c.width)
	}
	return p.trimRight().render()
}

// RenderFlatRow draws one node line on its own, sizing the name column to
// that row. The nodes tab lays out every row against the widest name
// instead; this form serves callers that have a single row.
func RenderFlatRow(r FlatRow, width int, theme shared.Theme, selected bool) string {
	rr := rowRenderer{theme: theme}
	rr.cols = columnsFor(width, rr.nameCellWidth(r), cellWidth(r.Row.Template))
	return rr.render(r, selected, false)
}

// oneLine sanitizes server text for a single row: control sequences go, and
// line breaks and tabs become spaces so a message cannot break the row.
func oneLine(s string) string {
	s = shared.Sanitize(s)
	if strings.ContainsAny(s, "\n\r\t") {
		s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	}
	return s
}

// padCell pads s with spaces to exactly n display cells.
func padCell(s string, n int) string {
	if w := cellWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// truncCell clips s to n display cells, marking the cut.
func truncCell(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cellWidth(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return ansi.Truncate(s, n-1, "") + "…"
}
