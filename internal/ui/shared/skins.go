package shared

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// Skin names with a meaning of their own.
const (
	// SkinDefault is the ANSI-16 skin: it names palette indexes, so the
	// terminal's own colour scheme still applies.
	SkinDefault = "default"
	// SkinAuto picks a dark or a light skin from the background colour the
	// terminal reports. Until the terminal answers, or when it never does,
	// it looks like the default skin, which suits either background.
	SkinAuto = "auto"
	// autoDark and autoLight are the skins auto chooses between. They are
	// one family, so switching terminals keeps the same look.
	autoDark  = "catppuccin-mocha"
	autoLight = "catppuccin-latte"
)

// Skin is one named palette.
type Skin struct {
	Name string
	// Dark reports whether the skin is drawn for a dark background. The
	// default and auto skins suit both and report false.
	Dark bool
	p    *palette
}

// palette holds the colours of a truecolor skin, by role. Every value is a
// published colour of the scheme the skin is named after.
type palette struct {
	// bg is the scheme's background. The skin never paints it behind the
	// whole screen; it is the text colour on badges, where the badge's
	// bright colour is the background.
	bg string
	// surface paints the header band; selection paints the selected row.
	surface, selection string
	// text is ordinary text, muted secondary text, and subtle the lines:
	// borders, tree guides and empty bar tracks.
	text, muted, subtle string
	// accent is the scheme's signature colour; mark highlights marked rows.
	accent, mark string
	// Phase colours. warning also carries Suspended.
	running, succeeded, failed, pending, other, warning string
}

// palettes are the built-in truecolor skins.
var palettes = map[string]struct {
	dark bool
	p    palette
}{
	// https://catppuccin.com/palette
	"catppuccin-mocha": {true, palette{
		bg: "#1e1e2e", surface: "#313244", selection: "#45475a",
		text: "#cdd6f4", muted: "#9399b2", subtle: "#585b70",
		accent: "#cba6f7", mark: "#fab387",
		running: "#89b4fa", succeeded: "#a6e3a1", failed: "#f38ba8",
		pending: "#bac2de", other: "#f5c2e7", warning: "#f9e2af",
	}},
	"catppuccin-latte": {false, palette{
		bg: "#eff1f5", surface: "#dce0e8", selection: "#ccd0da",
		text: "#4c4f69", muted: "#7c7f93", subtle: "#acb0be",
		accent: "#8839ef", mark: "#fe640b",
		running: "#1e66f5", succeeded: "#40a02b", failed: "#d20f39",
		pending: "#5c5f77", other: "#ea76cb", warning: "#df8e1d",
	}},
	// https://github.com/morhetz/gruvbox
	"gruvbox-dark": {true, palette{
		bg: "#282828", surface: "#3c3836", selection: "#504945",
		text: "#ebdbb2", muted: "#928374", subtle: "#665c54",
		accent: "#8ec07c", mark: "#fe8019",
		running: "#83a598", succeeded: "#b8bb26", failed: "#fb4934",
		pending: "#a89984", other: "#d3869b", warning: "#fabd2f",
	}},
	"gruvbox-light": {false, palette{
		bg: "#fbf1c7", surface: "#ebdbb2", selection: "#d5c4a1",
		text: "#3c3836", muted: "#928374", subtle: "#bdae93",
		accent: "#427b58", mark: "#af3a03",
		running: "#076678", succeeded: "#79740e", failed: "#9d0006",
		pending: "#7c6f64", other: "#8f3f71", warning: "#b57614",
	}},
	// https://www.nordtheme.com/docs/colors-and-palettes; muted is the
	// brightened nord3 the Nord ports use for comments.
	"nord": {true, palette{
		bg: "#2e3440", surface: "#3b4252", selection: "#434c5e",
		text: "#d8dee9", muted: "#616e88", subtle: "#4c566a",
		accent: "#88c0d0", mark: "#d08770",
		running: "#81a1c1", succeeded: "#a3be8c", failed: "#bf616a",
		pending: "#e5e9f0", other: "#b48ead", warning: "#ebcb8b",
	}},
	// https://draculatheme.com/contribute
	"dracula": {true, palette{
		bg: "#282a36", surface: "#44475a", selection: "#44475a",
		text: "#f8f8f2", muted: "#6272a4", subtle: "#44475a",
		accent: "#bd93f9", mark: "#ffb86c",
		running: "#8be9fd", succeeded: "#50fa7b", failed: "#ff5555",
		pending: "#f8f8f2", other: "#ff79c6", warning: "#f1fa8c",
	}},
	// https://github.com/folke/tokyonight.nvim (the night style)
	"tokyo-night": {true, palette{
		bg: "#1a1b26", surface: "#292e42", selection: "#283457",
		text: "#c0caf5", muted: "#565f89", subtle: "#414868",
		accent: "#7aa2f7", mark: "#ff9e64",
		running: "#7dcfff", succeeded: "#9ece6a", failed: "#f7768e",
		pending: "#a9b1d6", other: "#bb9af7", warning: "#e0af68",
	}},
	// https://ethanschoonover.com/solarized/
	"solarized-dark": {true, palette{
		bg: "#002b36", surface: "#073642", selection: "#073642",
		text: "#839496", muted: "#586e75", subtle: "#586e75",
		accent: "#268bd2", mark: "#cb4b16",
		running: "#2aa198", succeeded: "#859900", failed: "#dc322f",
		pending: "#93a1a1", other: "#d33682", warning: "#b58900",
	}},
	"solarized-light": {false, palette{
		bg: "#fdf6e3", surface: "#eee8d5", selection: "#eee8d5",
		text: "#657b83", muted: "#93a1a1", subtle: "#93a1a1",
		accent: "#268bd2", mark: "#cb4b16",
		running: "#2aa198", succeeded: "#859900", failed: "#dc322f",
		pending: "#586e75", other: "#d33682", warning: "#b58900",
	}},
	// https://github.com/atom/atom/tree/master/packages/one-dark-syntax
	"one-dark": {true, palette{
		bg: "#282c34", surface: "#21252b", selection: "#3e4451",
		text: "#abb2bf", muted: "#5c6370", subtle: "#4b5263",
		accent: "#61afef", mark: "#d19a66",
		running: "#56b6c2", succeeded: "#98c379", failed: "#e06c75",
		pending: "#828997", other: "#c678dd", warning: "#e5c07b",
	}},
	// https://rosepinetheme.com/palette
	"rose-pine": {true, palette{
		bg: "#191724", surface: "#26233a", selection: "#403d52",
		text: "#e0def4", muted: "#6e6a86", subtle: "#524f67",
		accent: "#c4a7e7", mark: "#f6c177",
		running: "#ebbcba", succeeded: "#9ccfd8", failed: "#eb6f92",
		pending: "#908caa", other: "#31748f", warning: "#f6c177",
	}},
	"rose-pine-dawn": {false, palette{
		bg: "#faf4ed", surface: "#f2e9e1", selection: "#dfdad9",
		text: "#575279", muted: "#9893a5", subtle: "#cecacd",
		accent: "#907aa9", mark: "#ea9d34",
		running: "#d7827e", succeeded: "#56949f", failed: "#b4637a",
		pending: "#797593", other: "#286983", warning: "#ea9d34",
	}},
	// https://monokai.nl/ (the original TextMate theme, not Monokai Pro)
	"monokai": {true, palette{
		bg: "#272822", surface: "#3e3d32", selection: "#49483e",
		text: "#f8f8f2", muted: "#75715e", subtle: "#49483e",
		accent: "#fd971f", mark: "#ae81ff",
		running: "#66d9ef", succeeded: "#a6e22e", failed: "#f92672",
		pending: "#f8f8f2", other: "#ae81ff", warning: "#e6db74",
	}},
}

// SkinNames lists every name a skin setting accepts: auto and default first,
// then the truecolor skins in alphabetical order.
func SkinNames() []string {
	names := make([]string, 0, len(palettes))
	for n := range palettes {
		names = append(names, n)
	}
	sort.Strings(names)
	return append([]string{SkinAuto, SkinDefault}, names...)
}

// LookupSkin finds a skin by name, ignoring case and surrounding space.
func LookupSkin(name string) (Skin, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case SkinAuto, SkinDefault:
		return Skin{Name: name}, true
	}
	e, ok := palettes[name]
	if !ok {
		return Skin{}, false
	}
	p := e.p
	return Skin{Name: name, Dark: e.dark, p: &p}, true
}

// CheckSkin returns an error naming every valid skin when name is not one.
func CheckSkin(name string) error {
	if _, ok := LookupSkin(name); ok {
		return nil
	}
	return fmt.Errorf("unknown skin %q (valid skins: %s)", name, strings.Join(SkinNames(), ", "))
}

// AutoSkin is the skin auto resolves to once the terminal has reported
// whether its background is dark.
func AutoSkin(dark bool) string {
	if dark {
		return autoDark
	}
	return autoLight
}

// Theme builds the skin's theme. noColor, or NO_COLOR in the environment,
// yields the plain theme whatever the skin.
func (s Skin) Theme(noColor bool) Theme {
	if noColor || hasNoColorEnv() {
		return plainTheme()
	}
	if s.p == nil {
		// default, and auto before the terminal has answered.
		t := defaultTheme()
		if s.Name != "" {
			t.Skin = s.Name
		}
		return t
	}
	return s.p.theme(s.Name)
}

// SkinTheme builds the theme for a skin by name. An unknown name is an
// error that lists the valid ones.
func SkinTheme(name string, noColor bool) (Theme, error) {
	s, ok := LookupSkin(name)
	if !ok {
		return Theme{}, CheckSkin(name)
	}
	return s.Theme(noColor), nil
}

// theme maps a palette onto every token.
func (p palette) theme(name string) Theme {
	fg := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	badge := func(hex string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.bg)).Background(lipgloss.Color(hex))
	}
	muted := fg(p.muted)
	return Theme{
		Skin:           name,
		PhaseRunning:   fg(p.running),
		PhaseSucceeded: fg(p.succeeded),
		PhaseFailed:    fg(p.failed),
		PhasePending:   fg(p.pending),
		PhaseOther:     fg(p.other),
		Header:         fg(p.accent).Bold(true),
		Selected:       fg(p.text).Background(lipgloss.Color(p.selection)),
		Dim:            muted,
		Warning:        fg(p.warning),
		ErrorText:      fg(p.failed).Bold(true),
		Text:           fg(p.text),
		Muted:          muted,
		Accent:         fg(p.accent),
		TableHeader:    muted.Bold(true),
		Marked:         fg(p.mark).Bold(true),
		TreeGuide:      fg(p.subtle),
		BarFill:        fg(p.succeeded),
		BarEmpty:       fg(p.subtle),
		BarRunning:     fg(p.running),
		Border:         fg(p.subtle),
		BorderShape:    lipgloss.RoundedBorder(),
		Title:          fg(p.accent).Bold(true),
		Footer:         muted,
		Band:           fg(p.text).Background(lipgloss.Color(p.surface)),
		AppName:        fg(p.accent).Bold(true),
		BadgeReadOnly:  badge(p.succeeded),
		BadgeActions:   badge(p.failed),
		HintKey:        fg(p.accent).Bold(true),
		HintDesc:       muted,
		TabActive:      fg(p.accent).Bold(true).Underline(true),
		TabInactive:    muted,
		// Micko takes the skin's nearest hues to his own, so he sits in the
		// scheme rather than on top of it.
		MickoBody: fg(p.failed),
		MickoWing: fg(p.running),
		MickoDark: fg(p.muted),
		MickoBeak: fg(p.text),
	}
}
