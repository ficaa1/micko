package app

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
	"github.com/ficaa1/micko/internal/ui/workflowlist"
)

// The raw view, the clipboard copy and the browser link take content out of
// the program. None of them writes to the server.

// maxCopyBytes bounds one clipboard write. Terminals refuse or truncate large
// escape sequences, so the cut is made here, where it can be reported.
const maxCopyBytes = 512 * 1024

// enterRaw switches to the borderless full-screen view, starting at the top.
func (m *Root) enterRaw() {
	m.rawMode = true
	m.rawTop = 0
	m.rawLeft = 0
	m.gPending = false
}

// handleRawKey handles the raw view's keys: scroll, pan, wrap, copy or leave.
func (m *Root) handleRawKey(key string) tea.Cmd {
	if m.gPending {
		m.gPending = false
		if key == "g" {
			m.rawTop = 0
			return nil
		}
	} else if key == "g" {
		m.gPending = true
		return nil
	}
	content := m.rawLines()
	lines := len(m.rawScreenLines())
	page := m.height - 1
	if page < 1 {
		page = 1
	}
	switch key {
	case "f", "esc":
		m.rawMode = false
	case "y":
		m.flash = m.copyLabel()
		return tea.SetClipboard(m.copyText())
	case "j", "down":
		m.rawScroll(1, lines)
	case "k", "up":
		m.rawScroll(-1, lines)
	case "pgdown", "ctrl+d", "space":
		m.rawScroll(page-1, lines)
	case "pgup", "ctrl+u":
		m.rawScroll(-(page - 1), lines)
	case "w":
		m.rawWrap = !m.rawWrap
		m.rawLeft = 0
		m.rawScroll(0, len(m.rawScreenLines()))
	case "h", "left":
		m.rawPan(-m.rawStep(), content)
	case "l", "right":
		m.rawPan(m.rawStep(), content)
	case "0":
		m.rawLeft = 0
	case "$":
		m.rawPan(rawWidest(content), content)
	case "home":
		m.rawTop = 0
	case "G", "end":
		m.rawScroll(lines, lines)
	}
	return nil
}

func (m *Root) rawScroll(delta, lines int) {
	max := lines - (m.height - 1)
	if max < 0 {
		max = 0
	}
	m.rawTop += delta
	if m.rawTop > max {
		m.rawTop = max
	}
	if m.rawTop < 0 {
		m.rawTop = 0
	}
}

// rawStep is one horizontal pan: half the screen, so a cut word stays in view.
func (m *Root) rawStep() int {
	if m.width < 2 {
		return 1
	}
	return m.width / 2
}

// rawPan moves the left edge, stopping where the widest line's end meets the
// right edge of the screen.
func (m *Root) rawPan(delta int, lines []string) {
	if m.rawWrap {
		return
	}
	max := rawWidest(lines) - m.width
	if max < 0 {
		max = 0
	}
	m.rawLeft += delta
	if m.rawLeft > max {
		m.rawLeft = max
	}
	if m.rawLeft < 0 {
		m.rawLeft = 0
	}
}

// rawWidest returns the display width of the widest line.
func rawWidest(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(expandTabs(l)))
	}
	return w
}

// expandTabs replaces each tab with spaces to the next 8-column stop.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, part := range strings.SplitAfter(s, "\t") {
		text, tab := strings.CutSuffix(part, "\t")
		b.WriteString(text)
		col += ansi.StringWidth(text)
		if tab {
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		}
	}
	return b.String()
}

// rawScreenLines returns the raw content as screen rows, wrapped when
// wrapping is on.
func (m *Root) rawScreenLines() []string {
	lines := m.rawLines()
	if !m.rawWrap || m.width < 1 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.Split(ansi.Hardwrap(expandTabs(l), m.width, true), "\n")...)
	}
	return out
}

// rawLines is the whole content of the active route, unwindowed and unstyled.
func (m *Root) rawLines() []string {
	switch m.route {
	case RouteDetail:
		if m.detailView != nil {
			return m.detailView.RawLines()
		}
	case RouteLogs:
		if m.logsView != nil {
			return m.logsView.RawLines()
		}
	default:
		if def := m.kind(m.route); def != nil {
			return def.pane.RawLines()
		}
		return m.listRawLines()
	}
	return nil
}

// listRawLines returns the visible list rows as tab-separated name, phase
// and UID, led by the namespace across namespaces.
func (m *Root) listRawLines() []string {
	rows := m.listView.Rows()
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		ns := ""
		if m.listAcrossNamespaces() {
			ns = shared.Sanitize(r.Ref.Namespace) + "\t"
		}
		out = append(out, ns+shared.Sanitize(r.Ref.Name)+"\t"+
			shared.Sanitize(workflowlist.DisplayPhase(r))+"\t"+
			shared.Sanitize(r.Ref.UID))
	}
	return out
}

// rawView renders the content and a last hint line, so a selection from the
// top stops before the hint.
func (m *Root) rawView() tea.View {
	lines := m.rawScreenLines()
	// A resize or a shorter refresh can leave the anchors past the content.
	m.rawScroll(0, len(lines))
	m.rawPan(0, lines)
	h := m.height
	if h <= 0 {
		h = len(lines) + 1
	}
	body := lines
	if m.rawTop < len(body) {
		body = body[m.rawTop:]
	} else {
		body = nil
	}
	if len(body) > h-1 {
		body = body[:h-1]
	}
	out := make([]string, 0, h)
	for _, l := range body {
		// An unpanned line keeps its tabs for a mouse copy.
		if m.rawLeft > 0 {
			l = ansi.Cut(expandTabs(l), m.rawLeft, m.rawLeft+m.width)
		}
		out = append(out, l)
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	hint := "RAW — f or esc leaves  y copy  q quit"
	switch {
	case m.rawWrap:
		hint = "RAW — w unwrap  f or esc leaves  y copy  q quit"
	case m.width > 0 && rawWidest(lines) > m.width:
		hint = "RAW — h/l pan  w wrap  f or esc leaves  y copy  q quit"
		if m.rawLeft > 0 {
			hint = "col " + strconv.Itoa(m.rawLeft+1) + "  " + hint
		}
	}
	if m.flash != "" {
		hint = m.flash + "   " + hint
	}
	out = append(out, m.theme.Dim.Render(hint))
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	return v
}

// copyText is what y copies, chosen by what the reader is looking at.
func (m *Root) copyText() string {
	var s string
	switch {
	case m.rawMode:
		s = strings.Join(m.rawLines(), "\n")
	case m.route == RouteDetail && m.detailView != nil:
		if row, ok := m.detailView.SelectedNode(); ok {
			s = row.Name
			if row.PodName != "" {
				s = row.PodName
			}
		} else {
			s = strings.Join(m.detailView.RawLines(), "\n")
		}
	case m.route == RouteLogs && m.logsView != nil:
		s = strings.Join(m.logsView.RawLines(), "\n")
	case m.kind(m.route) != nil:
		_, s = m.kind(m.route).pane.SelectedName()
	default:
		if ref := m.listView.SelectedRef(); ref.Name != "" {
			s = ref.Name
		}
	}
	if len(s) > maxCopyBytes {
		s = s[:maxCopyBytes]
	}
	return s
}

// copyLabel is the footer report for the copy, including any truncation.
func (m *Root) copyLabel() string {
	text := m.copyText()
	if text == "" {
		return "nothing to copy here"
	}
	n := strings.Count(text, "\n") + 1
	label := "copied " + plural(n, "line")
	if len(text) >= maxCopyBytes {
		label += " (cut at " + strconv.Itoa(maxCopyBytes/1024) + " KB)"
	}
	return label + " — if nothing pasted, your terminal blocks clipboard writes"
}

// workflowURL is the selected workflow's Argo UI address, or "".
func (m *Root) workflowURL() string {
	if m.webURL == "" {
		return ""
	}
	if m.kind(m.route) != nil {
		return m.kindURL()
	}
	ref := m.selection
	if m.route == RouteList {
		ref = m.listView.SelectedRef()
	}
	if ref.Name == "" || ref.Namespace == "" {
		return ""
	}
	// An archived run's page is keyed by UID; its name may belong to another run.
	if m.detailState.archived && ref == m.detailState.ref && ref.UID != "" {
		return strings.TrimSuffix(m.webURL, "/") + "/archived-workflows/" + ref.Namespace + "/" + ref.UID
	}
	return strings.TrimSuffix(m.webURL, "/") + "/workflows/" + ref.Namespace + "/" + ref.Name
}

// openInBrowser opens the selected workflow in the Argo UI and copies the
// link.
func (m *Root) openInBrowser() tea.Cmd {
	url := m.workflowURL()
	if url == "" {
		if m.webURL == "" {
			m.flash = "no web address for this profile — add webURL to it"
		} else {
			m.flash = "no workflow selected"
		}
		return nil
	}
	open := m.openURL
	if open == nil {
		open = openInDefaultBrowser
	}
	if err := open(url); err != nil {
		m.flash = "could not open a browser; the link is on the clipboard"
	} else {
		m.flash = "opened " + url
	}
	return tea.SetClipboard(url)
}

// openInDefaultBrowser passes the address as its own argument to a fixed
// per-platform command.
func openInDefaultBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
