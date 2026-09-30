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

// hasForeground reports whether s sets a foreground colour.
func hasForeground(s lipgloss.Style) bool {
	return isColor(s.GetForeground())
}

// styles returns each style token by field name.
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

// Skin names preserve configuration compatibility and list each accepted name once.
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
	if err := CheckSkin("nord"); err != nil {
		t.Errorf("CheckSkin(nord) = %v", err)
	}
}

// Truecolor skins give every token a foreground and painted blocks a background.
func TestEveryTruecolorSkinDefinesEveryToken(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	// Tokens drawn as a block of colour, whose background is the point.
	backed := map[string]bool{"Selected": true, "Band": true, "BadgeReadOnly": true, "BadgeActions": true}
	for _, name := range truecolorSkins {
		skin, ok := LookupSkin(name)
		if !ok {
			t.Fatalf("unknown skin %q", name)
		}
		th := skin.Theme(false)
		if th.Skin != name {
			t.Errorf("%s: theme reports skin %q", name, th.Skin)
		}
		for field, s := range styles(th) {
			if !hasForeground(s) {
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

// The default skin styles semantic tokens with ANSI slots and preserves terminal defaults.
func TestDefaultSkinStylesEveryTokenButTheTerminalDefaults(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	skin, ok := LookupSkin(SkinDefault)
	if !ok {
		t.Fatalf("unknown skin %q", SkinDefault)
	}
	th := skin.Theme(false)
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

// Environment and explicit no-color settings leave every token plain.
func TestPlainThemes(t *testing.T) {
	for _, mode := range []struct {
		name, env string
		flag      bool
	}{{"environment", "1", false}, {"argument", "", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", mode.env)
			themes := map[string]Theme{"NewTheme": NewTheme(mode.flag)}
			for _, name := range SkinNames() {
				skin, ok := LookupSkin(name)
				if !ok {
					t.Fatalf("unknown skin %q", name)
				}
				themes[name] = skin.Theme(mode.flag)
			}
			for name, th := range themes {
				for _, phase := range []string{"Running", "Succeeded", "Failed", "Error", "Pending", "Suspended", "Unknown"} {
					if got := th.PhaseStyle(phase).Render("x"); got != "x" {
						t.Errorf("%s PhaseStyle(%q) = %q, want plain x", name, phase, got)
					}
				}
				for field, style := range styles(th) {
					if got := style.Render("x"); got != "x" {
						t.Errorf("%s %s = %q, want plain x", name, field, got)
					}
				}
			}
		})
	}
}

// Auto selects the appropriate background variant and uses default styling before resolution.
func TestAutoPicksAVariantForEachBackground(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	dark, _ := LookupSkin(AutoSkin(true))
	light, _ := LookupSkin(AutoSkin(false))
	if !dark.Dark || light.Dark {
		t.Fatalf("auto variants: dark=%+v light=%+v", dark, light)
	}
	skin, ok := LookupSkin(SkinAuto)
	if !ok {
		t.Fatalf("unknown skin %q", SkinAuto)
	}
	before := skin.Theme(false)
	def := NewTheme(false)
	if before.Selected.Render("x") != def.Selected.Render("x") || before.Title.Render("x") != def.Title.Render("x") {
		t.Error("auto before the terminal answers does not draw as the default skin")
	}
	if before.Skin != SkinAuto {
		t.Errorf("auto theme reports skin %q", before.Skin)
	}
}

// Block selections fill the row while plain selections stay unpadded.
func TestSelectRowPadsOnlyABlockSelection(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	skin, ok := LookupSkin("nord")
	if !ok {
		t.Fatal("nord skin missing")
	}
	block := skin.Theme(false)
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
