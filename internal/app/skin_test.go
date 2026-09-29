package app

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// sgr is the escape sequence a style opens with. Two skins never share one
// for the same token, so finding it in a render says which skin drew it.
func sgr(s lipgloss.Style) string {
	out := s.Render("x")
	return out[:strings.Index(out, "x")]
}

// loadThemedDemo is the demo list with colour forced on, whatever the
// environment running the tests says.
func loadThemedDemo(t *testing.T) *Root {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	m := loadDemoList(t)
	return resize(t, m, 120, 30)
}

func applySkin(t *testing.T, m *Root, name string) shared.Theme {
	t.Helper()
	if _, err := m.ApplySkin(name); err != nil {
		t.Fatalf("ApplySkin(%q): %v", name, err)
	}
	return m.Theme()
}

// A skin change reaches every pane at once, including the ones already on
// screen. A pane left on the old theme would draw its rows in one palette
// inside a frame drawn in another.
func TestApplySkinRethemesEveryOpenView(t *testing.T) {
	m := loadThemedDemo(t)
	// Build every child under the default skin first.
	for _, msg := range runCmd(m.openWorkflow(m.listView.SelectedRef())) {
		m.Update(msg)
	}
	m.openLogs(OpenLogsMsg{Ref: m.selection})
	m.profView.Open("", "")

	th := applySkin(t, m, "nord")
	if th.Skin != "nord" || m.Skin() != "nord" {
		t.Fatalf("theme skin = %q, root skin = %q", th.Skin, m.Skin())
	}
	checks := map[string]struct {
		out, want string
	}{
		"frame border": {m.View().Content, sgr(th.Border)},
		"list rows":    {strings.Join(m.listView.BodyLines(testkit.FixtureEpoch), "\n"), sgr(th.Selected)},
		"detail tabs":  {strings.Join(m.detailView.BodyLines(), "\n"), sgr(th.TabActive)},
		"logs":         {strings.Join(m.logsView.BodyLines(), "\n"), sgr(th.Muted)},
		"profiles":     {strings.Join(m.profView.BodyLines(), "\n"), sgr(th.Title)},
		"actions":      {m.actionView.View().Content, sgr(th.Title)},
		"cron list":    {strings.Join(m.kind(RouteCron).pane.BodyLines(testkit.FixtureEpoch), "\n"), sgr(th.TableHeader)},
		"palette":      {strings.Join(m.palView.BodyLines(80, 4), "\n"), sgr(th.Title)},
	}
	for name, c := range checks {
		if !strings.Contains(c.out, c.want) {
			t.Errorf("%s still drawn in the old skin:\n%q", name, c.out)
		}
	}
	m.profView.Close()
	m.help.Toggle()
	if !strings.Contains(m.View().Content, th.Accent.Render("Global")) {
		t.Error("help overlay still drawn in the old skin")
	}
}

// A view built after the change starts in the new skin.
func TestViewsBuiltLaterUseTheCurrentSkin(t *testing.T) {
	m := loadThemedDemo(t)
	th := applySkin(t, m, "dracula")
	m.openLogs(OpenLogsMsg{Ref: m.listView.SelectedRef()})
	if !strings.Contains(strings.Join(m.logsView.BodyLines(), "\n"), sgr(th.Muted)) {
		t.Error("a logs pane opened after the change is drawn in the old skin")
	}
	m.selection = m.listView.SelectedRef()
	m.route = RouteDetail
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !strings.Contains(m.actionView.View().Content, sgr(th.Title)) {
		t.Error("an action pane opened after the change is drawn in the old skin")
	}
}

// An unknown name changes nothing and says which names would work.
func TestApplySkinRejectsAnUnknownName(t *testing.T) {
	m := loadThemedDemo(t)
	applySkin(t, m, "nord")
	_, err := m.ApplySkin("neon")
	if err == nil || !strings.Contains(err.Error(), "tokyo-night") {
		t.Fatalf("ApplySkin(neon) = %v, want an error listing the skins", err)
	}
	if m.Theme().Skin != "nord" || m.Skin() != "nord" {
		t.Errorf("an unknown name changed the skin to %q", m.Theme().Skin)
	}
}

// auto asks the terminal for its background, draws as the default skin
// until the answer, then switches to the variant drawn for it. Once the
// background is known it is not asked for again.
func TestAutoFollowsTheTerminalBackground(t *testing.T) {
	m := loadThemedDemo(t)
	cmd, err := m.ApplySkin(shared.SkinAuto)
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("auto did not ask the terminal for its background")
	}
	if got := m.Theme().Skin; got != shared.SkinAuto {
		t.Fatalf("auto before the answer draws as %q", got)
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if got, want := m.Theme().Skin, shared.AutoSkin(false); got != want {
		t.Errorf("light background: skin %q, want %q", got, want)
	}
	if m.Skin() != shared.SkinAuto {
		t.Errorf("root skin = %q, want it to stay auto", m.Skin())
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if got, want := m.Theme().Skin, shared.AutoSkin(true); got != want {
		t.Errorf("dark background: skin %q, want %q", got, want)
	}
	if cmd, _ := m.ApplySkin(shared.SkinAuto); cmd != nil {
		t.Error("auto asked again for a background it already knows")
	}
	if got, want := m.Theme().Skin, shared.AutoSkin(true); got != want {
		t.Errorf("auto with a known background drew %q, want %q", got, want)
	}
}

// A reported background changes nothing under a named skin.
func TestANamedSkinIgnoresTheBackground(t *testing.T) {
	m := loadThemedDemo(t)
	if cmd, _ := m.ApplySkin("nord"); cmd != nil {
		t.Error("a named skin asked for the terminal background")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if got := m.Theme().Skin; got != "nord" {
		t.Errorf("background report changed the skin to %q", got)
	}
}

// The background query is an escape sequence some terminals print rather
// than answer, so Init sends it only when auto needs it.
func TestInitAsksForTheBackgroundOnlyUnderAuto(t *testing.T) {
	asks := func(m *Root) bool {
		for _, msg := range runCmd(m.Init()) {
			if fmt.Sprintf("%T", msg) == fmt.Sprintf("%T", tea.RequestBackgroundColor()) {
				return true
			}
		}
		return false
	}
	m := loadThemedDemo(t)
	if asks(m) {
		t.Error("the default skin asked for the terminal background")
	}
	m = loadThemedDemo(t)
	if _, err := m.ApplySkin(shared.SkinAuto); err != nil {
		t.Fatal(err)
	}
	if !asks(m) {
		t.Error("auto did not ask for the terminal background at start")
	}
}

// A profile's skin arrives with its connection. A connection naming none
// leaves the current skin alone.
func TestAProfileSkinArrivesWithItsConnection(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	c := mustConnect(t, conn, "dev")
	c.Skin = "dracula"
	m.Adopt(c)
	if got := m.Theme().Skin; got != "dracula" {
		t.Fatalf("skin after adopting = %q, want dracula", got)
	}
	m.Adopt(mustConnect(t, conn, "prod"))
	if got := m.Theme().Skin; got != "dracula" {
		t.Errorf("a connection with no skin changed it to %q", got)
	}
}

// The mode badge is drawn in the armed style only when actions are enabled;
// the words say the same thing in every theme.
func TestHeaderBadgeFollowsTheSafetyMode(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	reader := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	armed := NewRoot(reader, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Second,
		actions.Options{AllowActions: true})
	armed = resize(t, armed, 100, 20)
	th := armed.Theme()
	head := strings.Split(armed.View().Content, "\n")[0]
	if !strings.Contains(head, th.BadgeActions.Render(" ACTIONS ENABLED ")) {
		t.Errorf("armed header lacks the actions badge: %q", head)
	}
	safe := resize(t, loadDemoList(t), 100, 20)
	head = strings.Split(safe.View().Content, "\n")[0]
	if !strings.Contains(head, th.BadgeReadOnly.Render("READ ONLY")) {
		t.Errorf("read-only header lacks its badge: %q", head)
	}
}
