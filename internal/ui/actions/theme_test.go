package actions

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Themes preserve pane text and style the keys shown to the operator.
func TestThemedActionPaneKeepsItsWords(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	skin, ok := shared.LookupSkin("tokyo-night")
	if !ok {
		t.Fatal("tokyo-night skin missing")
	}
	th := skin.Theme(false)
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	opts := Options{AllowActions: true, Server: "https://argo.test", Profile: "dev", Phase: "Failed"}
	plain, themed := NewWithOptions(ref, opts), NewWithOptions(ref, opts)
	themed.SetTheme(th)
	for _, c := range []struct {
		name string
		step func(*Model)
		keys []string
	}{
		{"menu", func(m *Model) { m.OpenMenu() }, []string{"[u]", "[esc]"}},
		{"confirmation", func(m *Model) { m.Open(core.ActionRetry) }, []string{"[y]", "[n/esc/enter]"}},
		{"terminate", func(m *Model) { m.Open(core.ActionTerminate) }, []string{"[enter]", "[esc]"}},
		{"unavailable", func(m *Model) { m.OpenUnavailable("connection lost") }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.step(plain)
			c.step(themed)
			got, want := themed.View().Content, plain.View().Content
			if ansi.Strip(got) != want {
				t.Fatalf("themed pane text differs:\n%s\n---\n%s", ansi.Strip(got), want)
			}
			if got == want {
				t.Fatalf("the theme styled nothing:\n%s", got)
			}
			for _, key := range c.keys {
				if !strings.Contains(got, th.HintKey.Render(key)) {
					t.Fatalf("unstyled %q: %q", key, got)
				}
			}
		})
	}
}
