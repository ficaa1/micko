// Package archivedlist is the archived workflow kind of the list pane: the
// workflow archive's rows, with the workflow list's columns, and enter
// opening a row in the detail route.
package archivedlist

import (
	"cmp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// OpenMsg asks the root to open an archived workflow in the detail route,
// read from the archive by its UID.
type OpenMsg struct{ Ref core.Ref }

// Spec is the archived workflow kind. Rows are keyed by UID: the archive
// keeps every run, and a workflow name reused over time appears once per run.
func Spec() kindlist.Spec[core.Workflow] {
	return kindlist.Spec[core.Workflow]{
		Noun:       "archived workflows",
		Title:      "Archived workflows",
		Namespaced: true,
		Key: func(w core.Workflow) (string, string) {
			return w.Summary.Ref.Namespace, w.Summary.Ref.Name
		},
		ID:       func(w core.Workflow) string { return w.Summary.Ref.UID },
		Columns:  columns,
		Cell:     cell,
		RowStyle: func(w core.Workflow, th shared.Theme) lipgloss.Style { return th.PhaseStyle(w.Summary.Phase) },
		Sorts: []kindlist.SortOrder[core.Workflow]{
			{Label: "newest", Compare: func(a, b core.Workflow, _ time.Time) int { return newestFirst(a, b) }},
			{Label: "phase, newest first", Compare: func(a, b core.Workflow, _ time.Time) int {
				if c := cmp.Compare(phaseRank(a.Summary.Phase), phaseRank(b.Summary.Phase)); c != 0 {
					return c
				}
				return newestFirst(a, b)
			}},
			{Label: "name", Compare: func(a, b core.Workflow, _ time.Time) int {
				return cmp.Compare(strings.ToLower(a.Summary.Ref.Name), strings.ToLower(b.Summary.Ref.Name))
			}},
		},
		Info:     info,
		Manifest: func(w core.Workflow) []byte { return w.Resource },
		Open: func(w core.Workflow) tea.Msg {
			return OpenMsg{Ref: w.Summary.Ref}
		},
		// The server's archive answers an empty page when no archive is
		// configured, exactly as it answers an empty archive, so an empty
		// list cannot say which it is.
		EmptyNote: "(a server with no workflow archive configured answers the same way)",
	}
}

// columns are the workflow list's: NAME, PHASE, AGE, DURATION and, from 80
// cells, MESSAGE in the rest of the row.
func columns(w int) []kindlist.Column {
	name, phase, age, dur := 30, 12, 8, 10
	if w >= 100 {
		name = 44
	}
	if w < 80 {
		name, phase, age, dur = 20, 11, 6, 10
		if extra := w - (name + phase + age + dur + 3*2); extra > 0 {
			name += extra
		}
	}
	cols := []kindlist.Column{
		{ID: "name", Title: "NAME", Width: name},
		{ID: "phase", Title: "PHASE", Width: phase},
		{ID: "age", Title: "AGE", Width: age, Right: true},
		{ID: "duration", Title: "DURATION", Width: dur},
	}
	if w >= 80 {
		cols = append(cols, kindlist.Column{ID: "message", Title: "MESSAGE", Width: 0})
	}
	return cols
}

func cell(w core.Workflow, col string, now time.Time) string {
	s := w.Summary
	switch col {
	case "name":
		return s.Ref.Name
	case "phase":
		p := s.Phase
		if p == "" {
			p = "(no phase)"
		}
		return shared.PhaseSymbol(s.Phase) + " " + p
	case "age":
		t := started(s)
		if t.IsZero() {
			return "-"
		}
		return kindlist.HumanDuration(now.Sub(t))
	case "duration":
		return duration(s)
	case "message":
		return s.Message
	}
	return ""
}

func started(s core.Summary) time.Time {
	if s.StartedAt != nil && !s.StartedAt.IsZero() {
		return *s.StartedAt
	}
	return s.CreatedAt
}

func duration(s core.Summary) string {
	if s.StartedAt == nil || s.StartedAt.IsZero() {
		return "-"
	}
	if s.FinishedAt == nil || s.FinishedAt.IsZero() {
		return "unfinished"
	}
	return kindlist.HumanDuration(s.FinishedAt.Sub(*s.StartedAt))
}

func newestFirst(a, b core.Workflow) int {
	ta, tb := started(a.Summary), started(b.Summary)
	switch {
	case ta.IsZero() && tb.IsZero():
		return 0
	case ta.IsZero():
		return 1
	case tb.IsZero():
		return -1
	}
	return tb.Compare(ta)
}

// phaseRank puts failures first: they are what a reader searches the
// archive for.
func phaseRank(p string) int {
	switch p {
	case "Failed", "Error":
		return 0
	case "Succeeded":
		return 2
	default:
		return 1
	}
}

// info is the panel for one archived run.
func info(w core.Workflow, _ bool, now time.Time) []kindlist.Field {
	s := w.Summary
	var f []kindlist.Field
	add := func(label, value string) { f = append(f, kindlist.Field{Label: label, Value: value}) }
	f = append(f, kindlist.Field{Label: "Source", Value: "the workflow archive: a record kept after the run; actions do not apply", Warn: true})
	add("Phase", shared.PhaseSymbol(s.Phase)+" "+s.Phase)
	if s.Message != "" {
		add("Message", s.Message)
	}
	if t := started(s); !t.IsZero() {
		add("Started", t.UTC().Format("2006-01-02 15:04 MST")+" ("+kindlist.HumanDuration(now.Sub(t))+" ago)")
	}
	if s.FinishedAt != nil && !s.FinishedAt.IsZero() {
		add("Finished", s.FinishedAt.UTC().Format("2006-01-02 15:04 MST"))
	}
	add("Duration", duration(s))
	if s.Progress != "" {
		add("Progress", s.Progress)
	}
	add("UID", s.Ref.UID)
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		label := ""
		if i == 0 {
			label = "Labels"
		}
		add(label, k+"="+s.Labels[k])
	}
	add("Open", "enter reads the whole run from the archive")
	return f
}
