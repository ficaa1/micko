package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ficaa1/micko/internal/savedviews"
	"github.com/ficaa1/micko/internal/ui/shared"
	"github.com/ficaa1/micko/internal/ui/workflowlist"
)

func (m *Root) readViews() (map[string]savedviews.View, bool) {
	if m.actionOpts.Demo {
		m.flash = "saved views are disabled in the demo"
		return nil, false
	}
	if m.viewStore == nil {
		m.viewStore = savedviews.Open()
	}
	views, err := m.viewStore.Read()
	if err != nil {
		m.flash = err.Error()
		return nil, false
	}
	return views, true
}

func (m *Root) viewNames() []string {
	views, ok := m.readViews()
	if !ok {
		return nil
	}
	names := make([]string, 0, len(views))
	for name := range views {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Root) listViews(_ string) tea.Cmd {
	names := m.viewNames()
	if names == nil {
		return nil
	}
	if len(names) == 0 {
		m.flash = "no saved views — :view-save NAME saves this workflow view"
	} else {
		m.flash = "saved views: " + strings.Join(names, ", ") + " — :view NAME loads one"
	}
	return nil
}

func (m *Root) saveView(name string) tea.Cmd {
	if _, ok := m.readViews(); !ok {
		return nil
	}
	if !m.connected() || m.profileCurrent == "" {
		m.flash = "connect a configured profile before saving a view"
		return nil
	}
	if m.drill != nil {
		m.flash = "return to :wf before saving a view"
		return nil
	}
	v := m.listView.SavedView()
	view := savedviews.View{Profile: m.profileCurrent, Namespace: m.deps.namespace, AllNamespaces: m.deps.allNamespaces, Query: v.Query, Phase: string(v.Phase), Sort: string(v.Sort)}
	if err := m.viewStore.Change(name, &view); err != nil {
		m.flash = err.Error()
	} else {
		m.flash = "saved view: " + shared.Sanitize(name)
	}
	return nil
}

func (m *Root) deleteView(name string) tea.Cmd {
	if _, ok := m.readViews(); !ok {
		return nil
	}
	if err := m.viewStore.Change(name, nil); err != nil {
		m.flash = err.Error()
	} else {
		m.flash = "deleted view: " + shared.Sanitize(name)
	}
	return nil
}

func (m *Root) loadView(name string) tea.Cmd {
	if name == "" {
		return m.listViews("")
	}
	views, ok := m.readViews()
	if !ok {
		return nil
	}
	v, ok := views[name]
	if !ok {
		m.flash = "no saved view named " + shared.Sanitize(name)
		return nil
	}
	known := false
	for _, profile := range m.profileCandidates() {
		if profile == v.Profile {
			known = true
		}
	}
	if !known || m.connector == nil {
		m.flash = "saved profile " + shared.Sanitize(v.Profile) + " is unavailable; update the profile or delete this view"
		return nil
	}
	// Validate before changing the connection or the active list.
	probe := workflowlist.New(m.theme)
	if err := probe.RestoreView(localView(v)); err != nil {
		m.flash = err.Error()
		return nil
	}
	if v.Namespace == "" {
		m.flash = "saved view has no namespace"
		return nil
	}
	m.openProfilePicker()
	cmd := m.switchProfile(v.Profile)
	m.pendingView = &v
	return cmd
}

func localView(v savedviews.View) workflowlist.View {
	return workflowlist.View{Query: v.Query, Phase: workflowlist.PhaseFilter(v.Phase), Sort: workflowlist.SortKey(v.Sort)}
}

func (m *Root) restoreView(v savedviews.View) tea.Cmd {
	m.pendingView = nil
	m.route = RouteList
	m.deps.namespace, m.deps.allNamespaces = v.Namespace, v.AllNamespaces
	cmd := m.restartList("saved view loaded")
	if err := m.listView.RestoreView(localView(v)); err != nil {
		m.flash = err.Error()
	}
	return cmd
}
