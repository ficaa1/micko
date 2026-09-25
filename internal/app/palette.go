package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/ui/palette"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// palette.go owns the `:` key: the command registry and what each command
// does to the session.
//
// The palette package draws the input and ranks the suggestions; it knows
// commands only by name. This file is the other half: the registry the palette
// is loaded from, and the conversion of its RunMsg into an effect. A command
// never reaches the network itself. It calls the same root methods as the key
// that does the same thing, so `:ns x` and choosing x in the picker are one
// code path with one set of generation rules.
//
// Two tables feed the registry:
//
//   - listKinds: every resource kind the list pane can show. Each kind is its
//     own route; registering a kind registers the palette command that
//     switches to it, and makes the route a list route for the `n` and `0`
//     keys.
//   - builtinCommands: the commands that move the session without changing
//     what kind of thing it lists.

// command is one registry entry: what the palette shows and completes, and
// what running it does.
type command struct {
	palette.Command
	// args supplies the values the argument completes from. Nil for a command
	// whose argument has no known values, or that takes none.
	args func(m *Root) []string
	// run executes the command. arg is the trimmed text after the command
	// word, empty when none was typed; run is only called with a non-empty
	// arg for a command whose Arg is set.
	run func(m *Root, arg string) tea.Cmd
}

// listKind is one resource kind the list pane can show.
type listKind struct {
	// name, aliases and desc are the palette entry that switches to the kind.
	name    string
	aliases []string
	desc    string
	// route is the kind's own list route.
	route Route
	// show makes the route active and starts whatever it needs to load. It
	// runs on the update loop like every other root method.
	show func(m *Root) tea.Cmd
}

// listKinds is every kind the palette can switch to, in the order the empty
// palette lists them. Adding a kind is one entry here plus its route.
func listKinds() []listKind {
	return []listKind{
		{
			name: "workflows", aliases: []string{"wf"}, desc: "the workflow list",
			route: RouteList, show: (*Root).showWorkflows,
		},
		{
			name: "cronworkflows", aliases: []string{"cwf", "cron"}, desc: "the cron workflow list",
			route: RouteCron, show: (*Root).showCron,
		},
		{
			name: "workflowtemplates", aliases: []string{"wftmpl", "tmpl"}, desc: "the workflow template list",
			route: RouteTemplates, show: (*Root).showTemplates,
		},
		{
			name: "clusterworkflowtemplates", aliases: []string{"cwftmpl"}, desc: "the cluster workflow template list",
			route: RouteClusterTemplates, show: (*Root).showClusterTemplates,
		},
		{
			name: "archived", aliases: []string{"aw"}, desc: "the archived workflow list",
			route: RouteArchived, show: (*Root).showArchived,
		},
	}
}

// builtinCommands are the commands that are not a resource kind.
func builtinCommands() []command {
	return []command{
		{
			Command: palette.Command{Name: "ns", Arg: "namespace",
				Desc: "switch namespace; no name opens the picker"},
			args: (*Root).namespaceCandidates,
			run:  (*Root).runNamespace,
		},
		{
			Command: palette.Command{Name: "all", Desc: "toggle all namespaces"},
			run:     func(m *Root, _ string) tea.Cmd { return m.toggleAllNamespaces() },
		},
		{
			Command: palette.Command{Name: "profile", Aliases: []string{"ctx"}, Arg: "profile",
				Desc: "switch profile (cluster); no name opens the picker"},
			args: (*Root).profileCandidates,
			run:  (*Root).runProfile,
		},
		{
			Command: palette.Command{Name: "help", Desc: "show every key"},
			run: func(m *Root, _ string) tea.Cmd {
				m.help.Open()
				return nil
			},
		},
		{
			Command: palette.Command{Name: "quit", Aliases: []string{"q"}, Desc: "leave argo-tui"},
			run:     func(m *Root, _ string) tea.Cmd { return m.quit() },
		},
	}
}

// newRegistry builds the palette registry: the kinds first, since switching
// what the pane lists is what the palette is used for most, then the rest.
func newRegistry() []command {
	var out []command
	for _, k := range listKinds() {
		show := k.show
		out = append(out, command{
			Command: palette.Command{Name: k.name, Aliases: k.aliases, Desc: k.desc},
			run:     func(m *Root, _ string) tea.Cmd { return show(m) },
		})
	}
	return append(out, builtinCommands()...)
}

// paletteSpecs is the registry as the palette package sees it.
func paletteSpecs(reg []command) []palette.Command {
	out := make([]palette.Command, len(reg))
	for i, c := range reg {
		out[i] = c.Command
	}
	return out
}

// isListRoute reports whether r is the route of a registered kind. The keys
// that act on "the list" — `n`, `0` — are bound on every such route.
func isListRoute(r Route) bool {
	for _, k := range listKinds() {
		if k.route == r {
			return true
		}
	}
	return false
}

// paletteOpen reports whether the palette owns the keyboard.
func (m *Root) paletteOpen() bool { return m.palView != nil && m.palView.IsOpen() }

// openPalette shows the palette. The namespace names are fetched in the
// background the first time, so `ns` completes from what the server reports
// and not only from the configured list.
func (m *Root) openPalette() tea.Cmd {
	if m.palView == nil {
		return nil
	}
	m.palView.Open()
	if m.nsDiscovered == nil && m.deps.nsLister != nil && !m.hasInflight("namespaces") {
		return m.fetchNamespaces()
	}
	return nil
}

// paletteArgs is the palette's argument source: the named command's values.
func (m *Root) paletteArgs(name string) []string {
	for _, c := range m.registry {
		if c.Name == name && c.args != nil {
			return c.args(m)
		}
	}
	return nil
}

// runCommand turns the palette's RunMsg into an effect. The palette only emits
// names it found in the registry, so a miss here means the registry changed
// under it; it is reported like any unknown command rather than ignored.
func (m *Root) runCommand(msg palette.RunMsg) tea.Cmd {
	for _, c := range m.registry {
		if c.Name != msg.Name {
			continue
		}
		if msg.Arg != "" && c.Arg == "" {
			m.flash = c.Name + " takes no argument"
			return nil
		}
		if strings.ContainsAny(msg.Arg, " \t") {
			m.flash = c.Name + " takes one " + c.Arg + " name"
			return nil
		}
		return c.run(m, msg.Arg)
	}
	m.flash = "unknown command: " + shared.Sanitize(msg.Name)
	return nil
}

// unknownCommand reports a line that names no command. The palette never
// guesses a near match, so the footer says what was typed and how to find
// what exists.
func (m *Root) unknownCommand(msg palette.UnknownMsg) {
	word, _, _ := palette.Parse(msg.Input)
	m.flash = "unknown command: " + shared.Sanitize(word) + " — : lists every command"
}

// showWorkflows is the workflows kind's show: the workflow list, from any
// route. It leaves detail and logs the way esc does, so their requests stop,
// and it is the plain list: a drill-down in progress ends.
func (m *Root) showWorkflows() tea.Cmd {
	m.leaveDetailAndLogs()
	m.endDrill()
	m.route = RouteList
	m.flash = "workflows"
	if m.connected() && !m.listState.loading && !terminalWatchMode(m.watchMode) {
		return m.startListGeneration()
	}
	return nil
}

// runNamespace is `ns`: the picker with no argument, a switch with one.
func (m *Root) runNamespace(arg string) tea.Cmd {
	if !m.connected() {
		m.flash = "no profile connected"
		return nil
	}
	if arg == "" {
		return m.openNamespacePicker()
	}
	if arg == m.deps.namespace && !m.deps.allNamespaces {
		m.flash = "already in namespace " + shared.Sanitize(arg)
		return nil
	}
	return m.switchNamespace(arg)
}

// runProfile is `profile`/`ctx`: the picker with no argument, a switch with a
// configured profile's name. A name the config file does not hold is refused:
// it names no server, so there is nothing to connect to.
//
// A switch opens the picker first. The picker is where a reconnection shows
// its progress and its failure; without it a failed `:ctx x` would leave the
// session disconnected with nothing on screen saying why.
func (m *Root) runProfile(arg string) tea.Cmd {
	if arg == "" {
		return m.openProfilePicker()
	}
	if m.connector == nil {
		m.flash = "this session has no profiles to switch between"
		return nil
	}
	known := false
	for _, n := range m.profileCandidates() {
		if n == arg {
			known = true
			break
		}
	}
	if !known {
		m.flash = "no profile named " + shared.Sanitize(arg)
		return nil
	}
	if arg == m.profileCurrent {
		m.flash = "already connected to " + shared.Sanitize(arg)
		return nil
	}
	m.openProfilePicker()
	return m.switchProfile(arg)
}

// namespaceCandidates is what `ns` completes from: the configured namespaces,
// the ones the server reported, the ones in the collected snapshot, and the
// current one, de-duplicated and sorted.
func (m *Root) namespaceCandidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = shared.Sanitize(strings.TrimSpace(n))
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range m.nsSeed {
		add(n)
	}
	for _, n := range m.nsDiscovered {
		add(n)
	}
	for _, it := range m.listState.items {
		add(it.Ref.Namespace)
	}
	add(m.deps.namespace)
	sort.Strings(out)
	return out
}

// profileCandidates is what `profile` completes from: the config file's
// profile names.
func (m *Root) profileCandidates() []string {
	if m.profView == nil {
		return nil
	}
	items := m.profView.Items()
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}
