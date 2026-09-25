package shared

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// truecolorSkins are the skins with a palette of their own.
var truecolorSkins = []string{
	"catppuccin-latte", "catppuccin-mocha", "dracula", "gruvbox-dark", "gruvbox-light",
	"monokai", "nord", "one-dark", "rose-pine", "rose-pine-dawn",
	"solarized-dark", "solarized-light", "tokyo-night",
}

// styles returns every lipgloss.Style token of a theme by field name, so a
// token added to Theme is covered by these tests without editing them.
func styles(t Theme) map[string]lipgloss.Style {
	out := map[string]lipgloss.Style{}
	v := reflect.ValueOf(t)
	for i := 0; i < v.NumField(); i++ {
		if s, ok := v.Field(i).Interface().(lipgloss.Style); ok {
			out[v.Type().Field(i).Name] = s
		}
	}
	return out
}

// SkinNames is the list a reader is shown when a name is wrong, and the list
// the command palette completes from. It must hold every skin exactly once,
// with the two special names first.
func TestSkinNamesListsEveryBuiltInSkin(t *testing.T) {
	names := SkinNames()
	if len(names) < 2 || names[0] != SkinAuto || names[1] != SkinDefault {
		t.Fatalf("SkinNames() = %v, want auto and default first", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("SkinNames() lists %q twice", n)
		}
		seen[n] = true
		if _, ok := LookupSkin(n); !ok {
			t.Errorf("SkinNames() lists %q, which LookupSkin does not find", n)
		}
	}
	for _, n := range truecolorSkins {
		if !seen[n] {
			t.Errorf("SkinNames() is missing %q", n)
		}
	}
	if len(names) != len(truecolorSkins)+2 {
		t.Errorf("SkinNames() has %d names, want %d: %v", len(names), len(truecolorSkins)+2, names)
	}
}

// A skin name typed into a config file or a flag matches whatever its case.
func TestLookupSkinIgnoresCaseAndSpace(t *testing.T) {
	s, ok := LookupSkin("  Tokyo-Night ")
	if !ok || s.Name != "tokyo-night" {
		t.Fatalf("LookupSkin(Tokyo-Night) = %+v, %v", s, ok)
	}
	if _, ok := LookupSkin("tokyo"); ok {
		t.Fatal("LookupSkin accepted a prefix of a name")
	}
}

// An unknown name is refused with every valid name in the message, so the
// reader can fix it without looking the names up.
func TestUnknownSkinErrorListsTheValidNames(t *testing.T) {
	err := CheckSkin("solarised")
	if err == nil {
		t.Fatal("CheckSkin accepted an unknown name")
	}
	for _, n := range SkinNames() {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("error does not list %q: %v", n, err)
		}
	}
	if _, err := SkinTheme("solarised", false); err == nil {
		t.Error("SkinTheme accepted an unknown name")
	}
	if err := CheckSkin("nord"); err != nil {
		t.Errorf("CheckSkin(nord) = %v", err)
	}
}

// Every truecolor skin sets a colour on every token that is meant to carry
// one. A token left at the zero style would draw in the terminal's colours
// inside an otherwise themed screen, which is the defect a new token most
// easily brings in.
func TestEveryTruecolorSkinDefinesEveryToken(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	// Tokens drawn as a block of colour, whose background is the point.
	backed := map[string]bool{"Selected": true, "Band": true, "BadgeReadOnly": true, "BadgeActions": true}
	for _, name := range truecolorSkins {
		th, err := SkinTheme(name, false)
		if err != nil {
			t.Fatal(err)
		}
		if th.Skin != name {
			t.Errorf("%s: theme reports skin %q", name, th.Skin)
		}
		for field, s := range styles(th) {
			if !HasForeground(s) {
				t.Errorf("%s: %s has no foreground colour", name, field)
			}
			if backed[field] && !HasBackground(s) {
				t.Errorf("%s: %s has no background colour", name, field)
			}
		}
		if th.BorderShape.TopLeft == "" || th.BorderShape.Top == "" {
			t.Errorf("%s: no border characters", name)
		}
	}
}

// The default skin names ANSI palette slots, so the terminal's own scheme
// decides the hues. Every token still has to style something; only Text and
// Band are meant to be the terminal's own foreground and background.
func TestDefaultSkinStylesEveryTokenButTheTerminalDefaults(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := SkinTheme(SkinDefault, false)
	if err != nil {
		t.Fatal(err)
	}
	for field, s := range styles(th) {
		if field == "Text" || field == "Band" {
			continue
		}
		if s.Render("x") == "x" {
			t.Errorf("default skin leaves %s unstyled", field)
		}
	}
	for field, s := range styles(th) {
		// An ANSI slot, never a fixed RGB or 256-colour value: those would
		// ignore the terminal's own scheme.
		if out := s.Render("x"); strings.Contains(out, "38;2;") || strings.Contains(out, "38;5;") {
			t.Errorf("default skin %s uses a fixed colour: %q", field, out)
		}
	}
}

// NO_COLOR outranks the skin: every skin, asked for by name, draws plain.
func TestNoColorKeepsEverySkinPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, name := range SkinNames() {
		th, err := SkinTheme(name, false)
		if err != nil {
			t.Fatal(err)
		}
		for field, s := range styles(th) {
			if got := s.Render("x"); got != "x" {
				t.Errorf("%s under NO_COLOR: %s rendered %q", name, field, got)
			}
		}
	}
}

// The noColor argument has the same effect as the environment variable.
func TestNoColorArgumentKeepsASkinPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := SkinTheme("dracula", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Selected.Render("x"); got != "x" {
		t.Fatalf("noColor theme styled the selection: %q", got)
	}
}

// auto resolves to a skin drawn for the background the terminal reported,
// and until then draws as the default skin, which suits either.
func TestAutoPicksAVariantForEachBackground(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	dark, _ := LookupSkin(AutoSkin(true))
	light, _ := LookupSkin(AutoSkin(false))
	if !dark.Dark || light.Dark {
		t.Fatalf("auto variants: dark=%+v light=%+v", dark, light)
	}
	before, err := SkinTheme(SkinAuto, false)
	if err != nil {
		t.Fatal(err)
	}
	def := NewTheme(false)
	if before.Selected.Render("x") != def.Selected.Render("x") || before.Title.Render("x") != def.Title.Render("x") {
		t.Error("auto before the terminal answers does not draw as the default skin")
	}
	if before.Skin != SkinAuto {
		t.Errorf("auto theme reports skin %q", before.Skin)
	}
}

// A selection drawn as a block is padded across the row, so the highlight is
// a bar; a plain selection gains no trailing spaces for a mouse to copy.
func TestSelectRowPadsOnlyABlockSelection(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	block, _ := SkinTheme("nord", false)
	if got := lipgloss.Width(block.SelectRow("row", 10)); got != 10 {
		t.Errorf("block selection is %d cells, want 10", got)
	}
	if got := NewTheme(true).SelectRow("row", 10); got != "row" {
		t.Errorf("plain selection = %q, want the row unpadded", got)
	}
}

// A zero Theme still draws a border with characters in it.
func TestZeroThemeBordersAreSquare(t *testing.T) {
	if b := (Theme{}).Borders(); b.TopLeft != "┌" || b.Left != "│" {
		t.Fatalf("zero theme border = %+v", b)
	}
}
