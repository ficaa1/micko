package app

import (
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
	"github.com/ficaa1/argo-tui/internal/ui/workflowlist"
)

// raw.go holds the three commands that take content OUT of the program: the
// borderless full-screen view, the clipboard copy, and the browser link.
//
// None of them mutates anything on the server. The browser link is the only
// one that leaves the process, and it opens a page the user already has
// access to — argo-tui never sends a request to the web address itself.

// maxCopyBytes bounds one clipboard write. The copy travels to the terminal
// as an escape sequence, and terminals refuse or truncate very large ones, so
// the cut is made here where it can be reported.
const maxCopyBytes = 512 * 1024

// enterRaw switches to the borderless full-screen view, starting at the top.
func (m *Root) enterRaw() {
	m.rawMode = true
	m.rawTop = 0
	m.gPending = false
}

// handleRawKey is the whole key matrix of the raw view: scroll, or leave.
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
	lines := len(m.rawLines())
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

// listRawLines renders the visible list rows as plain "name phase age" text.
// The table's padding is what makes it readable on screen and awkward in a
// paste, so the raw form uses single spaces. Across namespaces each line
// starts with the row's namespace, as the table does.
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

// rawView renders the borderless screen: content, then one hint line so the
// way out is never a guess. The hint is the last row, so a selection that
// starts at the top of the screen stops before it.
func (m *Root) rawView() tea.View {
	lines := m.rawLines()
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
	out := append([]string{}, body...)
	for len(out) < h-1 {
		out = append(out, "")
	}
	hint := "RAW — f or esc leaves  y copy  q quit"
	if m.flash != "" {
		hint = m.flash + "   " + hint
	}
	out = append(out, m.theme.Dim.Render(hint))
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	return v
}

// copyText is what the y key puts on the clipboard, chosen by what the reader
// is looking at.
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

// copyLabel is the footer report for the copy, including the truncation when
// one happened. A silent partial copy is the failure this line prevents.
func (m *Root) copyLabel() string {
	text := m.copyText()
	if text == "" {
		return "nothing to copy here"
	}
	n := strings.Count(text, "\n") + 1
	label := "copied " + plural(n, "line")
	if len(text) >= maxCopyBytes {
		label += " (cut at " + itoa(maxCopyBytes/1024) + " KB)"
	}
	return label + " — if nothing pasted, your terminal blocks clipboard writes"
}

// workflowURL builds the Argo UI address of the selected workflow. It returns
// an empty string when the profile has no web address or nothing is selected.
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
	return strings.TrimSuffix(m.webURL, "/") + "/workflows/" + ref.Namespace + "/" + ref.Name
}

// openInBrowser opens the selected workflow in the Argo UI and copies the
// link as well, so the reader keeps the address even if no browser opened.
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

// openInDefaultBrowser hands the address to the desktop. The command is fixed
// per platform and the address is a separate argument, so nothing in a
// workflow name can become part of the command line.
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
