package kindlist

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// minUsableWidth is the narrowest pane the table is drawn in, the same bound
// the workflow list uses.
const minUsableWidth = 60

// sideInfoWidth is the pane width from which the info panel sits beside the
// table instead of below it. Narrower, a side panel would leave neither the
// table nor the panel wide enough to read.
const sideInfoWidth = 140

// colGap is the run of spaces between two columns.
const colGap = 2

// minRestWidth is the narrowest "rest of the row" column worth drawing.
const minRestWidth = 10

// BodyLines renders the pane body: the toolbar, the table and, when open,
// the info panel.
func (m *Model[T]) BodyLines(now time.Time) []string {
	if m.width > 0 && m.width < minUsableWidth {
		return []string{
			"argo-tui: terminal too small (" + itoa(m.width) + "x" + itoa(m.height) + ")",
			"Resize to at least 60 columns to show the " + m.spec.Noun + " list.",
			"q quit  ? help  ctrl+c quit",
		}
	}
	toolbar := m.toolbarLines()
	if !m.info {
		lines := append(toolbar, m.tableLines(m.width, m.height-len(toolbar), now)...)
		return clamp(lines, m.height)
	}
	info := m.infoFields(now)
	if m.width >= sideInfoWidth {
		panelW := m.width * 2 / 5
		if panelW > 64 {
			panelW = 64
		}
		tableW := m.width - panelW - 3
		bodyH := m.height - len(toolbar)
		table := m.tableLines(tableW, bodyH, now)
		panel := m.panelLines(info, panelW, bodyH)
		lines := toolbar
		n := len(table)
		if len(panel) > n {
			n = len(panel)
		}
		if bodyH > 0 && n > bodyH {
			n = bodyH
		}
		for i := 0; i < n; i++ {
			left, right := "", ""
			if i < len(table) {
				left = table[i]
			}
			if i < len(panel) {
				right = panel[i]
			}
			lines = append(lines, padRight(left, tableW)+m.theme.Border.Render(" │ ")+right)
		}
		return clamp(lines, m.height)
	}
	// Below the table: the panel takes what its fields need, up to about
	// half the pane, so the table keeps its column heads and a few rows.
	// The table gets the rest, and the panel sits at the bottom of the pane.
	bodyH := m.height - len(toolbar)
	room := 0
	if bodyH > 0 {
		room = bodyH/2 - 1
		if room < 2 {
			room = 2
		}
	}
	panel := m.bottomPanel(info, m.width, room)
	tableH := 0
	if m.height > 0 {
		tableH = bodyH - len(panel) - 1
	}
	table := m.tableLines(m.width, tableH, now)
	for len(table) < tableH {
		table = append(table, "")
	}
	lines := append(toolbar, table...)
	lines = append(lines, m.theme.Border.Render(strings.Repeat("─", maxInt(m.width, 1))))
	lines = append(lines, panel...)
	return clamp(lines, m.height)
}

// twoColumnWidth is the pane width from which a bottom panel too tall for
// its room flows into two columns rather than cutting its last fields.
const twoColumnWidth = 100

// bottomPanel lays the info panel out below the table. When the fields do
// not fit the height and the pane is wide, they flow into two columns, split
// between two fields rather than inside one.
func (m *Model[T]) bottomPanel(fields []Field, width, height int) []string {
	one := m.panelLines(fields, width, 0)
	if height <= 0 || len(one) <= height || width < twoColumnWidth {
		return m.panelLines(fields, width, height)
	}
	colW := (width - 3) / 2
	split := len(fields)
	lines := 0
	for i, f := range fields {
		n := len(m.panelLines([]Field{f}, colW, 0))
		if f.Label != "" && lines >= (len(one)+1)/2 {
			split = i
			break
		}
		lines += n
	}
	left := m.panelLines(fields[:split], colW, height)
	right := m.panelLines(fields[split:], colW, height)
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padRight(l, colW)+m.theme.Border.Render(" │ ")+r)
	}
	return out
}

// toolbarLines is the filter, sort and state line, and a wrapped reason line
// when a failure needs more room than the line has.
func (m *Model[T]) toolbarLines() []string {
	search := "Search: (none)  / to filter by name"
	if m.allNS {
		search = "Search: (none)  / to filter by namespace/name"
	}
	if m.searchOn {
		search = "Search: " + m.searchBuf + "[_]  (enter keep, esc cancel)"
	} else if m.query != "" {
		search = "Search: " + m.query
	}
	if len(m.rows) < len(m.items) || m.query != "" {
		search += " [within " + itoa(len(m.items)) + " collected]"
	}
	parts := []string{search, "Sort: " + m.SortLabel()}
	if m.info {
		reveal := "values redacted (v reveals)"
		if m.Revealed() {
			reveal = "values shown (v redacts)"
		}
		parts = append(parts, reveal)
	}
	// The note and a failure's reason are the toolbar's messages. Each
	// follows on the line when it fits whole and gets wrapped lines of its
	// own when it does not, because either one cut short would hide what it
	// says about the rows below.
	type message struct {
		text  string
		style func(...string) string
	}
	var msgs []message
	if m.note != "" {
		msgs = append(msgs, message{shared.Sanitize(m.note), m.theme.Warning.Render})
	}
	switch m.status {
	case StatusLoading:
		parts = append(parts, m.theme.Warning.Render("loading…"))
	case StatusStale:
		msgs = append(msgs, message{"stale " + humanDuration(m.errAge) + " — " + m.errMsg, m.theme.Warning.Render})
	case StatusForbidden:
		msgs = append(msgs, message{"forbidden: " + m.errMsg, m.theme.ErrorText.Render})
	case StatusUnauthenticated:
		msgs = append(msgs, message{"unauthenticated: " + m.errMsg, m.theme.ErrorText.Render})
	case StatusUnsupported:
		msgs = append(msgs, message{m.errMsg, m.theme.ErrorText.Render})
	}
	line := strings.Join(parts, "  ")
	if m.width > 0 {
		line = ansi.Truncate(line, m.width, "…")
	}
	out := []string{line}
	for _, msg := range msgs {
		last := out[len(out)-1]
		if m.width <= 0 || ansi.StringWidth(last)+2+ansi.StringWidth(msg.text) <= m.width {
			out[len(out)-1] = last + "  " + msg.style(msg.text)
			continue
		}
		for _, l := range strings.Split(shared.Wrap(msg.text, m.width), "\n") {
			out = append(out, msg.style(l))
		}
	}
	return out
}

// tableLines renders the column heads and the rows that fit in height lines
// of a table width wide.
func (m *Model[T]) tableLines(width, height int, now time.Time) []string {
	cols := m.layout(width)
	head := ""
	for i, c := range cols {
		if i > 0 {
			head += strings.Repeat(" ", colGap)
		}
		cell := truncate(c.Title, c.Width)
		if i == len(cols)-1 && !c.Right {
			head += cell
			continue
		}
		head += pad(cell, c.Width, c.Right)
	}
	lines := []string{m.theme.TableHeader.Render(head)}
	if len(m.rows) == 0 {
		m.winStart, m.winEnd = 0, 0
		return append(lines, m.emptyState(width)...)
	}
	start, end := m.window(height - 1)
	m.winStart, m.winEnd = start, end
	sel := m.selIndex()
	for i := start; i < end; i++ {
		r := m.rows[i]
		line := ""
		for j, c := range cols {
			if j > 0 {
				line += strings.Repeat(" ", colGap)
			}
			var text string
			if c.ID == nsColumn {
				ns, _ := m.spec.Key(r)
				text = ns
			} else {
				text = m.spec.Cell(r, c.ID, now)
			}
			cell := truncate(shared.Sanitize(text), c.Width)
			if j == len(cols)-1 && !c.Right {
				line += cell
				continue
			}
			line += pad(cell, c.Width, c.Right)
		}
		switch {
		case i == sel:
			line = m.theme.SelectRow(line, width)
		case m.spec.RowStyle != nil:
			line = m.spec.RowStyle(r, m.theme).Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

// nsColumn is the NAMESPACE column's id; its cells come from the row's key.
const nsColumn = "_namespace"

// layout is the kind's columns for a table width, with the NAMESPACE column
// in front across namespaces, and the open last column sized to the rest or
// dropped when the rest is too narrow to read.
func (m *Model[T]) layout(width int) []Column {
	w := width
	if w <= 0 {
		w = 120
	}
	var cols []Column
	if m.allNS {
		nsw := len("NAMESPACE")
		for _, it := range m.items {
			ns, _ := m.spec.Key(it)
			if n := ansi.StringWidth(shared.Sanitize(ns)); n > nsw {
				nsw = n
			}
		}
		limit := 16
		if w >= 100 {
			limit = 20
		} else if w < 80 {
			limit = 12
		}
		if nsw > limit {
			nsw = limit
		}
		cols = append(cols, Column{ID: nsColumn, Title: "NAMESPACE", Width: nsw})
		w -= nsw + colGap
	}
	kind := m.spec.Columns(w)
	used := 0
	for i, c := range kind {
		if i > 0 {
			used += colGap
		}
		used += c.Width
	}
	if n := len(kind); n > 0 && kind[n-1].Width == 0 {
		rest := w - used
		if rest < minRestWidth {
			kind = kind[:n-1]
		} else {
			kind[n-1].Width = rest
		}
	}
	return append(cols, kind...)
}

// window returns the row range that fits in budget lines, moving only as far
// as needed to keep the cursor visible.
func (m *Model[T]) window(budget int) (int, int) {
	if m.height <= 0 || budget >= len(m.rows) {
		m.scrollTop = 0
		return 0, len(m.rows)
	}
	if budget < 1 {
		budget = 1
	}
	start := m.scrollTop
	if max := len(m.rows) - budget; start > max {
		start = max
	}
	if start < 0 {
		start = 0
	}
	if sel := m.selIndex(); sel >= 0 {
		if sel < start {
			start = sel
		}
		if sel >= start+budget {
			start = sel - budget + 1
		}
	}
	m.scrollTop = start
	return start, start + budget
}

// emptyState is what a table with no rows shows, wrapped to width. Loading,
// a refusal, a filter that matches nothing and a namespace with none of the
// kind each say which they are.
func (m *Model[T]) emptyState(width int) []string {
	noun := m.spec.Noun
	style := m.theme.Dim
	var line string
	switch {
	case m.status == StatusLoading:
		line = "loading " + noun + "…"
	case m.status == StatusForbidden:
		style, line = m.theme.ErrorText, "no "+noun+" visible: list forbidden"
	case m.status == StatusUnauthenticated:
		style, line = m.theme.ErrorText, "no "+noun+" visible: not authenticated"
	case m.status == StatusUnsupported:
		style, line = m.theme.ErrorText, "no "+noun+" on this server"
	case m.status == StatusStale && len(m.items) == 0:
		style, line = m.theme.ErrorText, "no "+noun+" visible: the list failed"
	case len(m.items) > 0:
		line = "no " + noun + " match the current filter"
	case !m.spec.Namespaced:
		line = "no " + noun + " on this cluster"
	case m.allNS:
		line = "no " + noun + " in any namespace this token can read"
		if m.spec.EmptyNote != "" {
			line += " " + m.spec.EmptyNote
		}
	default:
		line = "no " + noun + " in this namespace"
		if m.spec.EmptyNote != "" {
			line += " " + m.spec.EmptyNote
		}
	}
	// The line is wrapped rather than clipped: its end is often the part
	// that says what an empty list means.
	var out []string
	for _, l := range strings.Split(shared.Wrap(line, width), "\n") {
		out = append(out, style.Render(l))
	}
	return out
}

// infoFields is the panel content for the selected row.
func (m *Model[T]) infoFields(now time.Time) []Field {
	sel, ok := m.Selected()
	if !ok || m.spec.Info == nil {
		return []Field{{Value: "nothing selected"}}
	}
	return m.spec.Info(sel, m.Revealed(), now)
}

// panelLines lays the fields out in width cells and at most height lines:
// labels in one column, values wrapped beside them. A panel cut short says
// how to see the rest.
func (m *Model[T]) panelLines(fields []Field, width, height int) []string {
	labelW := 0
	for _, f := range fields {
		if n := ansi.StringWidth(f.Label); n > labelW {
			labelW = n
		}
	}
	if labelW > 16 {
		labelW = 16
	}
	valueW := width - labelW - 2
	if valueW < 12 {
		valueW = 12
	}
	var out []string
	for _, f := range fields {
		value := shared.Sanitize(f.Value)
		wrapped := strings.Split(shared.Wrap(value, valueW), "\n")
		for i, v := range wrapped {
			label := ""
			if i == 0 {
				label = f.Label
			}
			if f.Warn {
				v = m.theme.Warning.Render(v)
			}
			line := m.theme.Dim.Render(pad(truncate(label, labelW), labelW, false)) + "  " + v
			out = append(out, line)
		}
	}
	if height > 0 && len(out) > height {
		out = out[:height-1]
		out = append(out, m.theme.Dim.Render("… more in the manifest: f"))
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, width, "…")
	}
	return out
}

func clamp(lines []string, h int) []string {
	if h > 0 && len(lines) > h {
		return lines[:h]
	}
	return lines
}

func pad(s string, w int, right bool) string {
	n := ansi.StringWidth(s)
	if n >= w {
		return s
	}
	if right {
		return strings.Repeat(" ", w-n) + s
	}
	return s + strings.Repeat(" ", w-n)
}

func padRight(s string, w int) string { return pad(s, w, false) }

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// humanDuration renders a compact duration: 45s, 4m, 2h13m, 3d4h.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d >= 24*time.Hour:
		days := int(d / (24 * time.Hour))
		if h := int(d%(24*time.Hour)) / int(time.Hour); h > 0 {
			return itoa(days) + "d" + itoa(h) + "h"
		}
		return itoa(days) + "d"
	case d >= time.Hour:
		h := int(d / time.Hour)
		if mins := int(d%time.Hour) / int(time.Minute); mins > 0 {
			return itoa(h) + "h" + itoa(mins) + "m"
		}
		return itoa(h) + "h"
	case d >= time.Minute:
		return itoa(int(d/time.Minute)) + "m"
	default:
		return itoa(int(d/time.Second)) + "s"
	}
}

// HumanDuration is the compact duration the kinds' cells use, so every
// column of relative times reads the same.
func HumanDuration(d time.Duration) string { return humanDuration(d) }
