package workflowlist

import (
	"fmt"
	"slices"
)

// View is the local query, phase bucket and sort order saved by the shell.
type View struct {
	Query string
	Phase PhaseFilter
	Sort  SortKey
}

func (m *Model) SavedView() View { return View{m.query, m.phase, m.sort} }

func (m *Model) RestoreView(v View) error {
	if v.Sort != SortPhaseName && v.Sort != SortName && v.Sort != SortTime {
		return fmt.Errorf("unknown saved sort order")
	}
	if !slices.Contains(phaseCycle, v.Phase) {
		return fmt.Errorf("unknown saved phase filter")
	}
	if _, err := ParseQuery(v.Query); err != nil {
		return fmt.Errorf("invalid saved query")
	}
	m.phase, m.sort = v.Phase, v.Sort
	m.SearchOn = false
	m.searchBuf, m.queryBefore = "", ""
	m.tabs = nil
	m.pickOpen = false
	return m.SetQuery(v.Query)
}
