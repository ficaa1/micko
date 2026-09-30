// Package templatelist is the workflow template and cluster workflow
// template kinds of the list pane: their columns, sort orders, info panel and
// drill-down. The two kinds share one schema and one layout; they differ in
// scope and in the label the controller puts on the workflows they start.
package templatelist

import (
	"cmp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
)

// OwnerLabel and ClusterOwnerLabel are the labels the controller puts on a
// workflow submitted from a WorkflowTemplate or a ClusterWorkflowTemplate,
// with the template's name as the value. The drill-down lists the workflows
// that carry them.
const (
	OwnerLabel        = "workflows.argoproj.io/workflow-template"
	ClusterOwnerLabel = "workflows.argoproj.io/cluster-workflow-template"
)

// Spec is the WorkflowTemplate kind.
func Spec() kindlist.Spec[core.WorkflowTemplate] {
	s := base()
	s.Noun = "workflow templates"
	s.Title = "Workflow templates"
	s.Namespaced = true
	s.Drill = func(t core.WorkflowTemplate) kindlist.DrillMsg {
		return kindlist.DrillMsg{Namespace: t.Namespace, Selector: OwnerLabel + "=" + t.Name, Title: "template " + t.Name}
	}
	s.Info = func(t core.WorkflowTemplate, reveal bool, now time.Time) []kindlist.Field {
		return info(t, reveal, now, OwnerLabel)
	}
	return s
}

// ClusterSpec is the ClusterWorkflowTemplate kind. It has no namespace, and
// the workflows it starts can live in any namespace, so its drill-down asks
// in the session's own scope.
func ClusterSpec() kindlist.Spec[core.WorkflowTemplate] {
	s := base()
	s.Noun = "cluster workflow templates"
	s.Title = "Cluster workflow templates"
	s.Namespaced = false
	s.Drill = func(t core.WorkflowTemplate) kindlist.DrillMsg {
		return kindlist.DrillMsg{Selector: ClusterOwnerLabel + "=" + t.Name, Title: "cluster template " + t.Name}
	}
	s.Info = func(t core.WorkflowTemplate, reveal bool, now time.Time) []kindlist.Field {
		return info(t, reveal, now, ClusterOwnerLabel)
	}
	return s
}

func base() kindlist.Spec[core.WorkflowTemplate] {
	return kindlist.Spec[core.WorkflowTemplate]{
		Key:     func(t core.WorkflowTemplate) (string, string) { return t.Namespace, t.Name },
		Columns: columns,
		Cell:    cell,
		Sorts: []kindlist.SortOrder[core.WorkflowTemplate]{
			{Label: "name", Compare: func(a, b core.WorkflowTemplate, _ time.Time) int {
				return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
			}},
			{Label: "newest", Compare: func(a, b core.WorkflowTemplate, _ time.Time) int {
				return b.CreatedAt.Compare(a.CreatedAt)
			}},
		},
		Manifest: func(t core.WorkflowTemplate) []byte { return t.Resource },
	}
}

// columns lays the table out. NAME takes what the fixed columns leave; on a
// wide pane the description fills the rest of the row, and a narrow one
// drops the entrypoint, which the info panel also shows.
func columns(w int) []kindlist.Column {
	name := func(fixed int) int {
		n := w - fixed
		if n > 40 {
			n = 40
		}
		if n < 12 {
			n = 12
		}
		return n
	}
	switch {
	case w >= 100:
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: 32},
			{ID: "entrypoint", Title: "ENTRYPOINT", Width: 16},
			{ID: "templates", Title: "TEMPLATES", Width: 9, Right: true},
			{ID: "parameters", Title: "PARAMETERS", Width: 10, Right: true},
			{ID: "age", Title: "AGE", Width: 6, Right: true},
			{ID: "description", Title: "DESCRIPTION", Width: 0},
		}
	case w >= 72:
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: name(16 + 9 + 10 + 6 + 4*2)},
			{ID: "entrypoint", Title: "ENTRYPOINT", Width: 16},
			{ID: "templates", Title: "TEMPLATES", Width: 9, Right: true},
			{ID: "parameters", Title: "PARAMETERS", Width: 10, Right: true},
			{ID: "age", Title: "AGE", Width: 6, Right: true},
		}
	default:
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: name(9 + 10 + 6 + 3*2)},
			{ID: "templates", Title: "TEMPLATES", Width: 9, Right: true},
			{ID: "parameters", Title: "PARAMETERS", Width: 10, Right: true},
			{ID: "age", Title: "AGE", Width: 6, Right: true},
		}
	}
}

func cell(t core.WorkflowTemplate, col string, now time.Time) string {
	switch col {
	case "name":
		return t.Name
	case "entrypoint":
		if t.Entrypoint == "" && t.WorkflowTemplateRef != "" {
			return "→ " + t.WorkflowTemplateRef
		}
		return t.Entrypoint
	case "templates":
		return strconv.Itoa(len(t.Templates))
	case "parameters":
		return strconv.Itoa(len(t.Arguments))
	case "age":
		if t.CreatedAt.IsZero() {
			return "-"
		}
		return kindlist.HumanDuration(now.Sub(t.CreatedAt))
	case "description":
		return t.Description
	}
	return ""
}

// info is the panel for one template: what submitting it runs and with what
// arguments, the templates it defines, the account it runs as and its
// labels.
func info(t core.WorkflowTemplate, reveal bool, now time.Time, owner string) []kindlist.Field {
	var f []kindlist.Field
	add := func(label, value string) { f = append(f, kindlist.Field{Label: label, Value: value}) }
	if t.Description != "" {
		add("Description", t.Description)
	}
	if t.WorkflowTemplateRef != "" {
		add("Runs template", t.WorkflowTemplateRef)
	}
	if t.Entrypoint != "" {
		add("Entrypoint", t.Entrypoint)
	} else if t.WorkflowTemplateRef == "" {
		add("Entrypoint", "not set: chosen at submission")
	}
	f = append(f, kindlist.ArgumentFields(t.Arguments, reveal)...)
	if len(t.Templates) == 0 {
		add("Templates", "none")
	}
	for i, tm := range t.Templates {
		label := ""
		if i == 0 {
			label = "Templates"
		}
		line := tm.Name + " · " + tm.Type
		if tm.Name == t.Entrypoint {
			line += " · entrypoint"
		}
		add(label, line)
	}
	sa := t.ServiceAccount
	if sa == "" {
		sa = "not set: the namespace's default, or the one given at submission"
	}
	add("Service acct", sa)
	keys := make([]string, 0, len(t.Labels))
	for k := range t.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		add("Labels", "none")
	}
	for i, k := range keys {
		label := ""
		if i == 0 {
			label = "Labels"
		}
		add(label, k+"="+t.Labels[k])
	}
	if !t.CreatedAt.IsZero() {
		add("Created", t.CreatedAt.UTC().Format("2006-01-02 15:04 MST")+" ("+kindlist.HumanDuration(now.Sub(t.CreatedAt))+" ago)")
	}
	add("Runs", "enter lists the workflows labelled "+owner+"="+t.Name)
	return f
}
