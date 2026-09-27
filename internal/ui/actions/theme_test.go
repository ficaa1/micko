package actions

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The themed pane is the plain pane styled: every state keeps its words,
// and the bracketed keys are drawn as keys.
func TestThemedActionPaneKeepsItsWords(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("tokyo-night", false)
	if err != nil {
		t.Fatal(err)
	}
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	opts := Options{AllowActions: true, Server: "https://argo.test", Profile: "dev", Phase: "Failed"}
	plain, themed := NewWithOptions(ref, opts), NewWithOptions(ref, opts)
	themed.SetTheme(th)
	for _, step := range []func(m *Model){
		func(m *Model) { m.OpenMenu() },
		func(m *Model) { m.Open(core.ActionRetry) },
		func(m *Model) { m.Open(core.ActionTerminate) },
		func(m *Model) { m.OpenUnavailable("connection lost") },
	} {
		step(plain)
		step(themed)
		got, want := themed.View().Content, plain.View().Content
		if ansi.Strip(got) != want {
			t.Fatalf("themed pane text differs:\n%s\n---\n%s", ansi.Strip(got), want)
		}
		if got == want {
			t.Fatalf("the theme styled nothing:\n%s", got)
		}
	}
	themed.OpenMenu()
	if v := themed.View().Content; !strings.Contains(v, th.HintKey.Render("[u]")) {
		t.Errorf("menu keys are not drawn as keys: %q", v)
	}
}

// keys styles each bracketed key and leaves the words between them alone,
// including a row with no key and one with an unclosed bracket.
func TestKeysStylesOnlyTheBrackets(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, _ := shared.SkinTheme("nord", false)
	got := keys(th, "[y] Yes  [n] No")
	want := th.HintKey.Render("[y]") + " Yes  " + th.HintKey.Render("[n]") + " No"
	if got != want {
		t.Errorf("keys = %q, want %q", got, want)
	}
	for _, row := range []string{"no keys here", "open [ but never closed"} {
		if got := keys(th, row); got != row {
			t.Errorf("keys(%q) = %q", row, got)
		}
	}
}
