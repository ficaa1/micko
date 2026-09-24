package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// skin.go owns the palette every pane is drawn in.
//
// The root holds one theme and hands a copy to every child, so a skin change
// has to reach each of them at once: a pane left on the old theme would draw
// its rows in one palette inside a frame drawn in another. SetTheme is that
// single path, and every child the root builds later is built from m.theme.
//
// The auto skin needs a fact only the terminal knows, the background colour.
// The root asks for it when auto is chosen and applies the dark or light
// variant when the answer arrives. Until then, and on a terminal that never
// answers, auto draws as the default skin, which suits either background.

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
}

// Theme is the theme the frame is drawn in.
func (m *Root) Theme() shared.Theme { return m.theme }

// Skin is the skin last asked for by name. It reads "auto" while auto is
// in effect, whichever variant the terminal's background chose.
func (m *Root) Skin() string { return m.skin }

// ApplySkin switches to a skin by name. An unknown name changes nothing and
// returns an error that lists the valid names.
//
// The command it returns asks the terminal for its background colour. It is
// non-nil only for auto before the terminal has answered; the caller must
// run it, or auto stays on its fallback.
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

// backgroundQuery asks the terminal for its background colour when auto is
// waiting on it. Every other skin already knows its colours, so it asks
// nothing: the query is an escape sequence some terminals print instead of
// answering.
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
