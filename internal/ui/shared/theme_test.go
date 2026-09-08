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

func TestThemePhaseStylesCoverUnknownPhases(t *testing.T) {
	// Unknown phases must return a style without panicking (LIST-11),
	// in both color and plain modes.
	for _, phase := range []string{"Running", "Succeeded", "Failed", "Error", "Pending", "WeirdFuturePhase", ""} {
		th := NewTheme(false)
		_ = th.PhaseStyle(phase).Render(phase)
		pl := NewTheme(true)
		_ = pl.PhaseStyle(phase).Render(phase)
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
