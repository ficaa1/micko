package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// SetTheme re-themes the shell and every child view.
func (m *Root) SetTheme(t shared.Theme) {
	m.theme = t
	m.help.SetTheme(t)
	m.listView.SetTheme(t)
	if m.detailView != nil {
		m.detailView.SetTheme(t)
	}
	if m.logsView != nil {
		m.logsView.SetTheme(t)
	}
	if m.actionView != nil {
		m.actionView.SetTheme(t)
	}
	if m.nsView != nil {
		m.nsView.SetTheme(t)
	}
	if m.profView != nil {
		m.profView.SetTheme(t)
	}
	if m.palView != nil {
		m.palView.SetTheme(t)
	}
	for _, def := range m.kindDefs {
		def.pane.SetTheme(t)
	}
}

// Theme is the theme the frame is drawn in.
func (m *Root) Theme() shared.Theme { return m.theme }

// Skin is the skin last asked for; "auto" while auto is in effect.
func (m *Root) Skin() string { return m.skin }

// ApplySkin switches to a skin by name; an unknown name changes nothing. The
// returned command, non-nil only for auto before the terminal has answered,
// asks for the background colour and must be run.
func (m *Root) ApplySkin(name string) (tea.Cmd, error) {
	s, ok := shared.LookupSkin(name)
	if !ok {
		return nil, shared.CheckSkin(name)
	}
	m.skin = s.Name
	m.SetTheme(m.resolveSkin().Theme(false))
	return m.backgroundQuery(), nil
}

// resolveSkin turns the requested skin into the palette to draw: auto
// becomes its dark or light variant once the background is known.
func (m *Root) resolveSkin() shared.Skin {
	name := m.skin
	if name == shared.SkinAuto && m.bgKnown {
		name = shared.AutoSkin(m.bgDark)
	}
	s, ok := shared.LookupSkin(name)
	if !ok {
		s, _ = shared.LookupSkin(shared.SkinDefault)
	}
	return s
}

// backgroundQuery asks the terminal for its background colour while auto
// waits on it. Some terminals print the query, so nothing else asks.
func (m *Root) backgroundQuery() tea.Cmd {
	if m.skin != shared.SkinAuto || m.bgKnown {
		return nil
	}
	return tea.RequestBackgroundColor
}

// handleBackground records the terminal's background and, under auto,
// switches to the variant drawn for it.
func (m *Root) handleBackground(msg tea.BackgroundColorMsg) {
	m.bgKnown = true
	m.bgDark = msg.IsDark()
	if m.skin == shared.SkinAuto {
		m.SetTheme(m.resolveSkin().Theme(false))
	}
}
