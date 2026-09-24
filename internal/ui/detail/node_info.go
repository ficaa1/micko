package detail

import (
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// node_info.go is the node info panel: every fact the model holds about the
// node under the cursor. It reads the node map the workflow arrived with and
// never asks the server for more, so opening it costs nothing and shows the
// same snapshot as the tree.

// infoPlacement is where the panel sits.
type infoPlacement int

const (
	infoHidden infoPlacement = iota
	infoRight
	infoBottom
)

// Panel geometry. On a pane at least infoRightMin wide the panel is a column
// on the right, where it costs no tree rows; below that it is a band under
// the tree; below infoMinWidth there is no room for it next to anything.
// Like the column tiers, both are the pane of a terminal of a round width
// (140 and 60 columns) less its four border cells.
const (
	infoRightMin = 136
	infoMinWidth = 56
	infoRightW   = 52
	infoLabelW   = 10
	// infoTwoColumns is the band width from which the bottom panel sets
	// its groups side by side.
	infoTwoColumns = 90
)

func infoPlacementFor(width int) infoPlacement {
	switch {
	case width >= infoRightMin:
		return infoRight
	case width >= infoMinWidth:
		return infoBottom
	default:
		return infoHidden
	}
}

// infoItem is one labelled fact. A multi-line value continues under the
// value column.
type infoItem struct {
	label string
	value string
	style lipgloss.Style
	wrap  bool
}

// nodeInfoGroups collects the facts about one node in four groups: what the
// node is, how it went, where it ran, and what went in and out. Parameter
// values and the script result can hold secrets, so they show the resource
// tab's redaction marker unless the reader revealed values with v; the
// reveal is the same session-only state the resource tab uses.
func nodeInfoGroups(r FlatRow, n core.Node, found bool, now time.Time, reveal bool, t shared.Theme) [][]infoItem {
	row := r.Row
	text := t.Text
	muted := t.Muted
	item := func(label, value string, style lipgloss.Style) infoItem {
		return infoItem{label: label, value: value, style: style}
	}
	if !found {
		return [][]infoItem{{
			item("id", oneLine(row.NodeID), text),
			{label: "note", value: oneLine(row.Message), style: t.Warning, wrap: true},
		}}
	}

	var what []infoItem
	what = append(what, item("name", oneLine(rowDisplayName(row)), text))
	if full := oneLine(n.Name); full != "" && full != oneLine(rowDisplayName(row)) {
		what = append(what, infoItem{label: "full name", value: full, style: text, wrap: true})
	}
	what = append(what, item("id", oneLine(n.ID), muted))
	typ := oneLine(rowType(row))
	if row.Role != "" {
		typ += " (" + row.Role + ")"
	}
	what = append(what, item("type", typ, text))
	if n.TemplateName != "" {
		what = append(what, item("template", oneLine(n.TemplateName), text))
	}
	if n.TemplateRefTemplate != "" {
		what = append(what, item("template", oneLine(n.TemplateRefTemplate)+" (templateRef)", text))
	}

	var how []infoItem
	phase := nodeGlyph(r) + " " + oneLine(rowPhase(row))
	phaseStyle := t.PhaseStyle(row.Phase)
	if r.Suspended() {
		phase += " · AWAITING RESUME"
		phaseStyle = t.Warning
	}
	how = append(how, item("phase", phase, phaseStyle))
	if msg := oneLine(n.Message); msg != "" {
		how = append(how, infoItem{label: "message", value: msg, style: text, wrap: true})
	}
	if n.StartedAt != nil {
		how = append(how, item("started", clockText(*n.StartedAt, now), text))
	}
	if n.FinishedAt != nil {
		how = append(how, item("finished", clockText(*n.FinishedAt, now), text))
	}
	if d, ok := nodeDuration(row, now); ok {
		v := shortDuration(d)
		if _, _, running, _ := nodeInterval(row, now); running {
			v += " (running)"
		}
		how = append(how, item("duration", v, text))
	}
	if n.EstimatedDuration > 0 {
		how = append(how, item("estimate", "~"+humanDuration(n.EstimatedDuration), text))
	}
	if n.Progress != "" {
		how = append(how, item("progress", oneLine(n.Progress), text))
	}

	var where []infoItem
	switch {
	case n.PodName != "":
		where = append(where, infoItem{label: "pod", value: oneLine(n.PodName), style: text, wrap: true})
	case nodeTypeHasPod(n.Type):
		where = append(where, item("pod", "unknown (the server did not state its pod naming)", muted))
	}
	if n.HostNodeName != "" {
		where = append(where, item("host", oneLine(n.HostNodeName), text))
	}
	if n.ExitCode != "" {
		style := text
		if n.ExitCode != "0" {
			style = t.PhaseFailed
		}
		where = append(where, item("exit code", oneLine(n.ExitCode), style))
	}
	where = append(where, resourceItems(n.ResourcesDuration, text)...)
	var flags []string
	if n.Retried {
		flags = append(flags, "retried")
	}
	if n.Hooked {
		flags = append(flags, "hooked")
	}
	if n.MemoizationHit {
		flags = append(flags, "memoized")
	}
	if len(flags) > 0 {
		where = append(where, item("flags", strings.Join(flags, ", "), text))
	}

	var io []infoItem
	io = append(io, ioItems("inputs", n.Inputs, reveal, t)...)
	io = append(io, ioItems("outputs", n.Outputs, reveal, t)...)

	var out [][]infoItem
	for _, g := range [][]infoItem{what, how, where, io} {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// resourceItems states the recorded resource usage the way Argo accounts
// for it: a duration of one unit of the resource, one CPU for cpu and 100Mi
// for memory.
func resourceItems(res map[string]int64, style lipgloss.Style) []infoItem {
	if len(res) == 0 {
		return nil
	}
	keys := make([]string, 0, len(res))
	for k := range res {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := resourceRank(keys[i]), resourceRank(keys[j])
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	out := make([]infoItem, 0, len(keys))
	for _, k := range keys {
		v := shortDuration(time.Duration(res[k]) * time.Second)
		switch k {
		case "cpu":
			v += " × 1 cpu"
		case "memory":
			v += " × 100Mi"
		}
		out = append(out, infoItem{label: oneLine(k), value: v, style: style})
	}
	return out
}

func resourceRank(k string) int {
	switch k {
	case "cpu":
		return 0
	case "memory":
		return 1
	default:
		return 2
	}
}

// ioItems lists one side of a node's inputs or outputs: each parameter, each
// artifact by name, and the script result.
func ioItems(label string, io core.NodeIO, reveal bool, t shared.Theme) []infoItem {
	if io.Empty() {
		return nil
	}
	value := func(v string) (string, lipgloss.Style) {
		if !reveal {
			return redactedMarker, t.Muted
		}
		return oneLine(v), t.Text
	}
	var out []infoItem
	add := func(v string, style lipgloss.Style) {
		l := ""
		if len(out) == 0 {
			l = label
		}
		out = append(out, infoItem{label: l, value: v, style: style})
	}
	for _, p := range io.Parameters {
		v, style := value(p.Value)
		add(oneLine(p.Name)+" = "+v, style)
	}
	for _, a := range io.Artifacts {
		add("artifact "+oneLine(a), t.Text)
	}
	if io.Result != "" {
		v, style := value(io.Result)
		add("result = "+v, style)
	}
	return out
}

// clockText is an absolute time and how long ago it was.
func clockText(at, now time.Time) string {
	s := at.Format("2006-01-02 15:04:05 MST")
	if d := now.Sub(at); d >= 0 {
		s += " (" + humanDuration(d) + " ago)"
	}
	return s
}

// infoGroupLines lays one group out in width cells: labels in a column,
// values beside them, wrapped values continuing under the value.
func infoGroupLines(g []infoItem, width int, t shared.Theme) []string {
	valueW := max(width-infoLabelW, 8)
	var out []string
	for _, it := range g {
		vals := []string{it.value}
		if it.wrap {
			vals = nil
			for _, l := range strings.Split(shared.Wrap(it.value, valueW), "\n") {
				vals = append(vals, hardWrap(l, valueW)...)
			}
			if len(vals) > 4 {
				vals = append(vals[:3], truncCell(strings.Join(vals[3:], " "), valueW))
			}
		}
		for i, v := range vals {
			var p pieces
			label := ""
			if i == 0 {
				label = it.label
			}
			p.add(padCell(label, infoLabelW), t.Muted)
			p.add(v, it.style)
			out = append(out, p.truncate(width).trimRight().render())
		}
	}
	return out
}

// hardWrap splits a line wider than width into width-cell pieces. Word
// wrapping leaves a long identifier whole; a node name or a pod name has no
// spaces to break at, and cutting it would hide the end that tells two
// attempts apart.
func hardWrap(s string, width int) []string {
	if width <= 0 || cellWidth(s) <= width {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	w := 0
	for _, r := range s {
		rw := cellWidth(string(r))
		if w+rw > width && w > 0 {
			out = append(out, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// infoTitle heads the panel with the node's name and the state of the
// value reveal, which is what v changes here.
func infoTitle(r FlatRow, reveal bool, width int, t shared.Theme) string {
	var p pieces
	p.add("NODE ", t.TableHeader)
	p.add(oneLine(rowDisplayName(r.Row)), t.Accent)
	if reveal {
		p.add("  values REVEALED (v redacts)", t.Warning)
	} else {
		p.add("  values redacted (v reveals)", t.Muted)
	}
	return p.truncate(width).trimRight().render()
}

// renderInfo lays the panel out in width × height cells for its placement.
// The right panel stacks the groups; the bottom band sets them in two
// columns once it is wide enough, so it takes fewer tree rows. Content that
// does not fit ends with a line saying how much is cut.
func renderInfo(groups [][]infoItem, r FlatRow, reveal bool, width, height int, place infoPlacement, t shared.Theme) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	lines := []string{infoTitle(r, reveal, width, t)}
	if place == infoBottom && width >= infoTwoColumns && len(groups) > 1 {
		colW := (width - colGap) / 2
		half := (len(groups) + 1) / 2
		left := stackGroups(groups[:half], colW, t)
		right := stackGroups(groups[half:], colW, t)
		for i := 0; i < max(len(left), len(right)); i++ {
			var l, rr string
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				rr = right[i]
			}
			lines = append(lines, padStyled(l, colW)+strings.Repeat(" ", colGap)+rr)
		}
	} else {
		lines = append(lines, stackGroups(groups, width, t)...)
	}
	if len(lines) > height {
		cut := len(lines) - height + 1
		lines = append(lines[:height-1], t.Muted.Render(truncCell("… "+itoaDetail(cut)+" more lines", width)))
	}
	return lines
}

// stackGroups lays groups one under the other, a blank line between them.
func stackGroups(groups [][]infoItem, width int, t shared.Theme) []string {
	var out []string
	for i, g := range groups {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, infoGroupLines(g, width, t)...)
	}
	return out
}

// padStyled pads a styled line with spaces to n visible cells.
func padStyled(s string, n int) string {
	if w := cellWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
