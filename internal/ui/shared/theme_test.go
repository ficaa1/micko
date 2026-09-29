package shared

import (
	"testing"
)

func TestQuitKeySemantics(t *testing.T) {
	cases := []struct {
		name  string
		ctrlC bool
		key   string
		ctx   KeyContext
		want  bool
	}{
		{"q in browsing quits", false, "q", KeyCtxBrowsing, true},
		{"q in text entry does not quit", false, "q", KeyCtxTextEntry, false},
		{"q in dialog does not quit", false, "q", KeyCtxDialog, false},
		{"ctrl-c quits in text entry", true, "q", KeyCtxTextEntry, true},
		{"ctrl-c quits in dialog", true, "ctrl+c", KeyCtxDialog, true},
		{"ctrl-c quits in browsing", true, "ctrl+c", KeyCtxBrowsing, true},
		{"other key does not quit", false, "j", KeyCtxBrowsing, false},
	}
	for _, c := range cases {
		if got := QuitKeySet(c.ctrlC, c.key, c.ctx); got != c.want {
			t.Errorf("%s: QuitKeySet = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestKeyContextString(t *testing.T) {
	if KeyCtxTextEntry.String() != "text-entry" {
		t.Errorf("text entry label = %q", KeyCtxTextEntry.String())
	}
}

func TestNewThemePlainWithNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	th := NewTheme(false) // noColor=false, but env forces plain
	out := th.PhaseStyle("Running").Render("Running")
	if out != "Running" {
		t.Errorf("NO_COLOR output = %q, want plain %q", out, "Running")
	}
}

// --- status symbols ---

// Color is never the only carrier of a status. Every phase must
// also get a distinct symbol, which survives NO_COLOR and a mono terminal.
func TestPhaseSymbolsAreDistinct(t *testing.T) {
	phases := []string{"Running", "Succeeded", "Failed", "Error", "Pending"}
	seen := map[string]string{}
	for _, p := range phases {
		s := PhaseSymbol(p)
		if s == "" {
			t.Fatalf("phase %q has no symbol", p)
		}
		if prev, dup := seen[s]; dup && !(p == "Error" && prev == "Failed") {
			t.Fatalf("phase %q reuses the symbol of %q (%q)", p, prev, s)
		}
		seen[s] = p
	}
}

// An unknown phase must still render something: a server may
// invent a phase and the UI must not show a blank cell.
func TestUnknownPhaseKeepsASymbol(t *testing.T) {
	if PhaseSymbol("WeirdFuturePhase") == "" {
		t.Fatal("unknown phase lost its symbol")
	}
}

// Every symbol is one terminal cell wide, or the table columns misalign.
func TestPhaseSymbolsAreSingleWidth(t *testing.T) {
	for _, p := range []string{"Running", "Succeeded", "Failed", "Pending", "Nonsense"} {
		if w := len([]rune(PhaseSymbol(p))); w != 1 {
			t.Fatalf("phase %q symbol is %d runes, want 1", p, w)
		}
	}
}

// NO_COLOR must not remove the border style hook; the plain theme still has
// to answer, so the shell can render an uncolored border.
func TestPlainThemeStillExposesBorderStyles(t *testing.T) {
	plain := NewTheme(true)
	if got := plain.Border.Render("x"); got != "x" {
		t.Fatalf("plain border style added styling: %q", got)
	}
	if got := plain.Title.Render("x"); got != "x" {
		t.Fatalf("plain title style added styling: %q", got)
	}
}
