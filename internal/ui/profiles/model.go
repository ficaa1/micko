// Package profiles is the profile picker: the dialog that chooses which
// cluster this session talks to.
//
// micko starts in it when no --profile was given, so the first screen asks
// which cluster rather than assuming one, and the P key reopens it at any
// time. A profile carries the server, the credentials and the port-forward
// target, so choosing one is a reconnection, not a filter.
//
// The picker is a dialog (shared.KeyCtxDialog): while it is open it owns every
// key except Ctrl-C. It holds no client and starts nothing. The root loads the
// profile names into it and converts its one intent into a reconnection.
//
// Unlike the namespace picker, typed text only narrows the list. A namespace
// that appears in no list is still a namespace the server may hold, so that
// picker accepts typed text as a value; a profile that appears in no list does
// not exist in the config file, and nothing could be connected to.
package profiles

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// SwitchMsg is the picker's only output: the profile the reader chose.
type SwitchMsg struct{ Profile string }

// Item is one row: the name to connect by, and enough of the profile to tell
// two clusters apart on screen. No token material is ever carried here,
// because every field of this struct is rendered.
type Item struct {
	Name      string
	Server    string
	Namespace string
}

// Model is the picker state.
type Model struct {
	open bool
	// current is the connected profile, marked in the list so the reader can
	// see where they are before they move. Empty before the first connection.
	current string
	// items is the configured profile list, already sorted by the loader.
	items []Item
	// rows is items narrowed by the typed filter; the cursor indexes it.
	rows   []Item
	cursor int
	// filter is what the reader has typed.
	filter string
	// configPath is the file the items came from, named in the empty state so
	// a reader with no profiles learns where to write one.
	configPath string
	// lastErr explains why the file yielded nothing.
	lastErr string
	// connecting is the profile a reconnection is running for. The dialog
	// stays up during it, because a port-forward takes seconds and a blank
	// screen for those seconds looks like a crash.
	connecting string

	theme         shared.Theme
	width, height int
}

// New builds a closed picker.
func New(theme shared.Theme) *Model { return &Model{theme: theme} }

// IsOpen reports whether the dialog owns the keyboard.
func (m *Model) IsOpen() bool { return m.open }

// Connecting reports whether a reconnection is running behind the dialog.
func (m *Model) Connecting() bool { return m.connecting != "" }

// Open shows the picker and clears the previous filter, so it never reopens
// narrowed by a search the reader has forgotten about. cursor is the profile
// to start on: the connected one, or the file's currentProfile before the
// first connection.
func (m *Model) Open(current, cursor string) {
	m.open = true
	m.current = current
	m.filter = ""
	m.connecting = ""
	m.applyFilter()
	m.selectName(cursor)
}

// Close hides the picker.
func (m *Model) Close() { m.open = false; m.connecting = "" }

// SetItems records the configured profiles and the file they came from.
func (m *Model) SetItems(items []Item, configPath string) {
	m.items = append([]Item(nil), items...)
	m.configPath = configPath
	m.applyFilter()
}

// SetError records why the config file yielded no profiles. It is not fatal:
// the dialog stays open and says what happened, because closing it would
// leave a session with no connection and no way to start one.
func (m *Model) SetError(msg string) { m.lastErr = shared.Sanitize(msg) }

// SetConnecting marks a running reconnection, so the dialog reports the wait
// instead of closing onto an empty list.
func (m *Model) SetConnecting(name string) { m.connecting = name }

// SetSize records the dialog box size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetTheme replaces the style set the dialog is drawn in.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// Items returns the current candidate list (tests and the root).
func (m *Model) Items() []Item { return append([]Item(nil), m.items...) }

// Selected returns the profile the picker would switch to right now, or the
// empty string when the filter matches no configured profile.
func (m *Model) Selected() string {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].Name
	}
	return ""
}

// Update implements the dialog key matrix. Every printable character narrows
// the list, so there is no separate "search mode" to enter and leave.
//
// Keys are ignored while a reconnection runs: the old connection is already
// gone and the new one is not up, so a second choice would have nothing to
// cancel and would race the first.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.open || m.connecting != "" {
		return nil
	}
	switch s := key.String(); s {
	case "esc":
		m.Close()
		return nil
	case "enter":
		name := m.Selected()
		if name == "" || name == m.current {
			m.Close()
			return nil
		}
		return func() tea.Msg { return SwitchMsg{Profile: name} }
	case "up", "ctrl+p":
		m.move(-1)
		return nil
	case "down", "ctrl+n":
		m.move(1)
		return nil
	case "backspace":
		r := []rune(m.filter)
		if len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.applyFilter()
		}
		return nil
	default:
		if runes := []rune(s); len(runes) == 1 && runes[0] >= 0x20 {
			m.filter += s
			m.applyFilter()
		}
		return nil
	}
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= len(m.rows) {
		m.cursor = 0
	}
}

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	rows := make([]Item, 0, len(m.items))
	for _, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.Name), q) {
			rows = append(rows, it)
		}
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = 0
	}
}

func (m *Model) selectName(name string) {
	for i, it := range m.rows {
		if it.Name == name {
			m.cursor = i
			return
		}
	}
	m.cursor = 0
}

// Hints is the dialog footer. It changes while connecting, because esc and
// enter do nothing then and a footer that offers them would be a lie.
func (m *Model) Hints() string {
	if m.connecting != "" {
		return "connecting to " + shared.Sanitize(m.connecting) + "…"
	}
	if m.current == "" {
		// Nothing is connected yet, so there is no session to escape back to.
		return "type to filter  ↑↓ move  enter connect  esc quit"
	}
	return "type to filter  ↑↓ move  enter switch  esc cancel"
}

// BodyLines renders the dialog. The empty state is the important one: a reader
// with no config file gets the path to write and a file to write into it,
// rather than an empty box that looks like a broken program.
func (m *Model) BodyLines() []string {
	var lines []string
	if m.current != "" {
		lines = append(lines, m.theme.Title.Render("switch profile")+m.theme.Muted.Render(" — current: ")+shared.Sanitize(m.current))
	} else {
		lines = append(lines, m.theme.Title.Render("choose a profile"))
	}
	if m.connecting != "" {
		lines = append(lines, "", "connecting to "+shared.Sanitize(m.connecting)+"…",
			m.theme.Dim.Render("a port-forward can take a few seconds"))
		return m.clamp(lines)
	}
	lines = append(lines, "filter: "+shared.Sanitize(m.filter)+"_")
	if m.lastErr != "" {
		lines = append(lines, "", m.theme.Warning.Render("config: "+m.lastErr))
	}
	if len(m.items) == 0 {
		return m.clamp(append(lines, m.emptyLines()...))
	}
	typed := strings.TrimSpace(m.filter)
	if len(m.rows) == 0 && typed != "" {
		lines = append(lines, "", "no profile matches "+shared.Sanitize(typed))
	}
	// Each column is as wide as the longest value on screen, so two clusters
	// are compared by reading down a column rather than across a row.
	nameWidth, serverWidth := 0, 0
	for _, it := range m.rows {
		nameWidth = maxWidth(nameWidth, shared.Sanitize(it.Name))
		serverWidth = maxWidth(serverWidth, shared.Sanitize(it.Server))
	}
	for i, it := range m.rows {
		mark := "  "
		if it.Name == m.current {
			mark = "* "
		}
		row := mark + pad(shared.Sanitize(it.Name), nameWidth)
		if it.Server != "" || it.Namespace != "" {
			row += "  " + pad(shared.Sanitize(it.Server), serverWidth)
		}
		if it.Namespace != "" {
			row += "  ns=" + shared.Sanitize(it.Namespace)
		}
		row = strings.TrimRight(row, " ")
		switch {
		case i == m.cursor:
			row = m.theme.SelectRow(row, m.width)
		case it.Name == m.current:
			row = m.theme.Accent.Render(row)
		}
		lines = append(lines, row)
	}
	if m.configPath != "" {
		lines = append(lines, "", m.theme.Dim.Render(shared.Sanitize(m.configPath)))
	}
	return m.clamp(lines)
}

// emptyLines is what a reader with no profiles sees: where the file goes, what
// goes in it, and the one command that works without any of it.
func (m *Model) emptyLines() []string {
	path := m.configPath
	if path == "" {
		path = "~/.config/micko/config.yaml"
	}
	lines := []string{"", "no profiles configured", "", "write them to:", "  " + shared.Sanitize(path), "", "a file that connects:"}
	for _, l := range strings.Split(sampleConfig, "\n") {
		lines = append(lines, "  "+l)
	}
	return append(lines, "", m.theme.Dim.Render("micko --demo looks around without a cluster"))
}

// sampleConfig is the smallest config file that connects. It is duplicated
// from no other source on purpose: the picker must render the same keys the
// config loader requires, and a sample assembled from the loader's structs
// would print the optional keys too.
const sampleConfig = `currentProfile: dev
profiles:
  dev:
    server: https://argo.example.com
    namespace: argo
    tokenEnv: ARGO_TOKEN`

func (m *Model) clamp(lines []string) []string {
	if m.height > 0 {
		return shared.ClampLines(lines, m.height)
	}
	return lines
}

// pad widens s to width display cells. Padding is measured in cells rather
// than bytes, so a name with a wide rune in it does not push its row out of
// the column.
func pad(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func maxWidth(cur int, s string) int {
	if w := ansi.StringWidth(s); w > cur {
		return w
	}
	return cur
}
