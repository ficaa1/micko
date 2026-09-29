package detail

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// nodes_tab.go is the nodes tab's state machine and layout: folding, the
// find input, the info panel, and how the progress header, the column heads,
// the tree and the panel share the pane.

// nodeFind is the find-by-name state. While editing, the input owns every
// key; once committed, n and N step through the matches and esc clears them.
type nodeFind struct {
	editing   bool
	committed bool
	query     string
	// ids are the matching node IDs in tree order, with every fold open.
	ids []string
	// pos is the current match, an index into ids.
	pos int
	// inSkipped counts matches the tree is hiding as skipped, so "no
	// match" can say where the name is.
	inSkipped int
}

func (f nodeFind) active() bool { return f.editing || f.committed }

// nodesChromeRows is the tab strip, the progress header and the status line.
const nodesChromeRows = 3

// Fold and panel sizing.
const (
	// colHeadMinRows is the tree height from which the column heads are
	// worth a row.
	colHeadMinRows = 6
	// infoBottomMin and infoBottomMax bound the bottom panel's height; it
	// takes two fifths of the tree's rows in between.
	infoBottomMin = 6
	infoBottomMax = 16
	// minTreeRows is the fewest tree rows the bottom panel leaves.
	minTreeRows = 4
)

// nodesLayout is how the nodes tab splits its pane.
type nodesLayout struct {
	treeW     int
	treeRows  int
	colHead   bool
	place     infoPlacement
	panelW    int
	panelRows int
}

func (m *Model) nodesLayout() nodesLayout {
	return m.treeLayout(nodesChromeRows, len(m.nodes), colHeadMinRows)
}

// treeLayout splits a pane that draws the node tree: chrome rows above it,
// rows lines of tree, column heads from headMin rows of tree, and the info
// panel beside or below the tree while it is open.
func (m *Model) treeLayout(chrome, rows, headMin int) nodesLayout {
	l := nodesLayout{treeW: m.width}
	area := m.height - chrome
	if m.showInfo {
		l.place = infoPlacementFor(m.width)
	}
	switch l.place {
	case infoRight:
		l.panelW = infoRightW
		l.treeW = m.width - infoRightW - cellWidth(infoSeparator)
	case infoBottom:
		// The band takes two fifths of the rows, within bounds. When the
		// whole tree fits above a bigger band, the band takes the rest, so
		// a short tree leaves no blank rows while facts are cut below it.
		ph := min(max(infoBottomMin, area*2/5), infoBottomMax)
		if spare := area - rows - 1; spare > ph {
			ph = spare
		}
		if area-ph < minTreeRows {
			ph = area - minTreeRows
		}
		if ph < 3 {
			l.place = infoHidden
		} else {
			l.panelRows = ph
			area -= ph
		}
	}
	l.colHead = area >= headMin && rows > 0
	if l.colHead {
		area--
	}
	l.treeRows = max(area, 1)
	return l
}

// infoSeparator divides the tree from the right-hand panel.
const infoSeparator = " │ "

// renderer lays rows out for a tree width.
func (m *Model) renderer(width int, theme shared.Theme) rowRenderer {
	return rowRenderer{
		theme: theme,
		cols:  columnsFor(width, m.nameNeed, m.templateNeed),
		span:  workflowSpan(m.workflow(), m.now),
		now:   m.now,
	}
}

// workflow reassembles the parts of the workflow the tab keeps.
func (m *Model) workflow() core.Workflow {
	return core.Workflow{Summary: m.state.Summary, Nodes: m.nodeMap, NodesAvailable: m.state.Outline.Available}
}

// rebuildNodes re-flattens the outline and places the cursor. keep is the
// node the cursor should stay on, wherever the rebuild puts it; when it is
// empty or gone, the cursor keeps its index, clamped to the new rows, so it
// can never point past the rows the view will draw.
func (m *Model) rebuildNodes(keep string) {
	// A phase filter searches the whole tree, so it starts from every row:
	// hiding the skipped branches first would make the Skipped bucket empty
	// and could drop a match under a hidden parent. Folds are part of the
	// tree drawing and do not apply to the flat filtered list either.
	all := m.nodePhase == NodePhaseAll
	opts := FlattenOptions{HideSkipped: m.hideSkipped && all}
	if all {
		opts.Folded = m.folded
	}
	SortOutline(&m.state.Outline, m.nodeSort)
	f := Flatten(m.state.Outline, opts)
	m.nodes, m.hiddenSkipped = f.Rows, f.HiddenSkipped
	m.folds, m.foldedNodes = f.Folds, f.FoldedNodes
	m.totalNodes = len(m.nodes)
	m.nodes = FilterFlatRows(m.nodes, m.nodePhase)

	rr := rowRenderer{theme: m.theme}
	m.nameNeed, m.templateNeed = 0, 0
	for _, r := range m.nodes {
		m.nameNeed = max(m.nameNeed, rr.nameCellWidth(r))
		m.templateNeed = max(m.templateNeed, cellWidth(oneLine(r.Row.Template)))
	}

	if keep != "" {
		if i := m.indexOf(keep); i >= 0 {
			m.nodeCursor = i
		}
	}
	if m.nodeCursor >= len(m.nodes) {
		m.nodeCursor = len(m.nodes) - 1
	}
	if m.nodeCursor < 0 {
		m.nodeCursor = 0
	}
	m.refreshFind()
	m.timelineChanged()
}

// selectedID is the node ID under the cursor, or "" when there is none.
func (m *Model) selectedID() string {
	if m.nodeCursor < 0 || m.nodeCursor >= len(m.nodes) {
		return ""
	}
	return m.nodes[m.nodeCursor].Row.NodeID
}

func (m *Model) indexOf(id string) int {
	for i, r := range m.nodes {
		if r.Row.NodeID == id {
			return i
		}
	}
	return -1
}

// applyDefaultFolds folds each finished retry attempt that is not the last,
// when it has a subtree to fold. The last attempt is the one that decided
// the outcome; the earlier ones are history a reader opens on purpose. Each
// node is folded by default once only, so a reader who opens it keeps it
// open through every refresh.
func (m *Model) applyDefaultFolds() {
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			if r.Type == "Retry" {
				m.foldOlderAttempts(r)
			}
			walk(r.Children)
		}
	}
	walk(m.state.Outline.Rows)
}

func (m *Model) foldOlderAttempts(retry OutlineRow) {
	parent := m.nodeMap[retry.NodeID]
	last, lastSeq := "", -1
	for _, c := range retry.Children {
		if isMember(parent, m.nodeMap[c.NodeID]) && c.Seq > lastSeq {
			last, lastSeq = c.NodeID, c.Seq
		}
	}
	for _, c := range retry.Children {
		if c.NodeID == last || len(c.Children) == 0 || m.autoFolded[c.NodeID] {
			continue
		}
		if !isMember(parent, m.nodeMap[c.NodeID]) {
			continue
		}
		if c.FinishedAt == nil && !phaseFinished(c.Phase) {
			continue
		}
		m.folded[c.NodeID] = true
		m.autoFolded[c.NodeID] = true
	}
}

// handleNodesKey handles the keys only the nodes tab binds.
func (m *Model) handleNodesKey(key string) {
	switch key {
	case "space", "left", "right":
		if id, fold, ok := treeFoldKey(key, m.nodes, &m.nodeCursor); ok {
			m.setFold(id, fold)
		}
	case "i":
		m.showInfo = !m.showInfo
	case "/":
		m.find = nodeFind{editing: true}
	case "n":
		m.stepMatch(1)
	case "N":
		m.stepMatch(-1)
	}
}

// treeFoldKey applies a fold key to a tree drawn as rows with the cursor at
// *cursor. space folds or opens the row. left folds an open row, and on a
// folded row or a leaf moves to the parent, so repeated presses climb the
// tree. right opens a folded row, and on an open one steps to its first
// child. It moves the cursor itself and returns the fold to set, if any.
func treeFoldKey(key string, rows []FlatRow, cursor *int) (id string, fold, ok bool) {
	if *cursor < 0 || *cursor >= len(rows) {
		return "", false, false
	}
	r := rows[*cursor]
	switch key {
	case "space":
		if r.HasChildren {
			return r.Row.NodeID, !r.Folded, true
		}
	case "left":
		switch {
		case r.HasChildren && !r.Folded:
			return r.Row.NodeID, true, true
		case r.Parent >= 0:
			*cursor = r.Parent
		}
	case "right":
		switch {
		case r.Folded:
			return r.Row.NodeID, false, true
		case r.HasChildren && *cursor+1 < len(rows):
			*cursor++
		}
	}
	return "", false, false
}

func (m *Model) cursorRow() (FlatRow, bool) {
	if m.nodeCursor < 0 || m.nodeCursor >= len(m.nodes) {
		return FlatRow{}, false
	}
	return m.nodes[m.nodeCursor], true
}

// setFold folds or opens one node and keeps the cursor on it.
func (m *Model) setFold(id string, fold bool) {
	if fold {
		m.folded[id] = true
	} else {
		delete(m.folded, id)
	}
	m.rebuildNodes(id)
}

// handleFindKey edits the find input. Printable keys type, backspace
// deletes, enter jumps to the first match, esc abandons the find.
func (m *Model) handleFindKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.clearFind()
	case "enter":
		q := strings.TrimSpace(m.find.query)
		if q == "" {
			m.clearFind()
			return nil
		}
		m.find.query = q
		m.find.editing = false
		m.find.committed = true
		m.refreshFind()
		m.find.pos = 0
		if len(m.find.ids) > 0 {
			m.jumpToMatch(m.find.ids[0])
		}
	case "backspace":
		if r := []rune(m.find.query); len(r) > 0 {
			m.find.query = string(r[:len(r)-1])
		}
		m.refreshFind()
	default:
		if ch, ok := printableKey(key); ok {
			m.find.query += ch
			m.refreshFind()
		}
	}
	return nil
}

// printableKey is the text a key types, if it types any.
func printableKey(key string) (string, bool) {
	if key == "space" {
		return " ", true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 0x20 && r[0] != 0x7f {
		return key, true
	}
	return "", false
}

func (m *Model) clearFind() { m.find = nodeFind{} }

// refreshFind recomputes the matches for the current query, keeping the
// current match when it still matches. It runs on every rebuild, so a
// refresh that adds nodes updates the count.
func (m *Model) refreshFind() {
	if !m.find.active() {
		return
	}
	cur := ""
	if m.find.pos >= 0 && m.find.pos < len(m.find.ids) {
		cur = m.find.ids[m.find.pos]
	}
	m.find.ids, m.find.inSkipped = m.findMatches(m.find.query)
	m.find.pos = 0
	for i, id := range m.find.ids {
		if id == cur {
			m.find.pos = i
		}
	}
}

// findMatches lists the rows whose display name contains q, ignoring case,
// in tree order with every fold open: a match under a folded parent is
// still a match, and jumping to it opens the way. Rows hidden as skipped
// are not jumped to, but they are counted.
func (m *Model) findMatches(q string) (ids []string, inSkipped int) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, 0
	}
	match := func(r FlatRow) bool {
		return strings.Contains(strings.ToLower(rowDisplayName(r.Row)), q)
	}
	rows := m.nodes
	if m.nodePhase == NodePhaseAll {
		rows = Flatten(m.state.Outline, FlattenOptions{HideSkipped: m.hideSkipped}).Rows
	}
	for _, r := range rows {
		if match(r) {
			ids = append(ids, r.Row.NodeID)
		}
	}
	if m.nodePhase == NodePhaseAll && m.hideSkipped && m.hiddenSkipped > 0 {
		n := 0
		for _, r := range Flatten(m.state.Outline, FlattenOptions{}).Rows {
			if match(r) {
				n++
			}
		}
		inSkipped = n - len(ids)
	}
	return ids, inSkipped
}

// stepMatch moves to the next (+1) or previous (-1) match, wrapping.
func (m *Model) stepMatch(delta int) {
	if !m.find.committed || len(m.find.ids) == 0 {
		return
	}
	n := len(m.find.ids)
	m.find.pos = ((m.find.pos+delta)%n + n) % n
	m.jumpToMatch(m.find.ids[m.find.pos])
}

// jumpToMatch puts the cursor on a node, opening every fold above it.
func (m *Model) jumpToMatch(id string) {
	if m.nodePhase == NodePhaseAll {
		rows := Flatten(m.state.Outline, FlattenOptions{HideSkipped: m.hideSkipped}).Rows
		for i, r := range rows {
			if r.Row.NodeID != id {
				continue
			}
			for p := rows[i].Parent; p >= 0; p = rows[p].Parent {
				delete(m.folded, rows[p].Row.NodeID)
			}
			break
		}
	}
	m.rebuildNodes(id)
}

// nodesBody is the nodes tab under the tab strip: the progress header, the
// status line, then the tree with its column heads and the info panel.
func (m *Model) nodesBody() []string {
	l := m.nodesLayout()
	lines := []string{progressLine(m.workflow(), m.now, m.width, m.theme), m.nodesStatusLine()}
	return m.withInfoPanel(lines, m.nodeTreeLines(l), l)
}

// withInfoPanel appends the tree to lines with the info panel where the
// layout places it: a column on the right, a band below, or nowhere.
func (m *Model) withInfoPanel(lines, tree []string, l nodesLayout) []string {
	switch l.place {
	case infoRight:
		rows := l.treeRows
		if l.colHead {
			rows++
		}
		if m.height <= 0 {
			rows = max(rows, len(tree))
		}
		panel := m.infoPanel(l.panelW, rows, infoRight)
		sep := m.theme.TreeGuide.Render(infoSeparator)
		// A blank panel line ends at the rule, so no line carries
		// trailing spaces a mouse selection would pick up.
		bare := m.theme.TreeGuide.Render(strings.TrimRight(infoSeparator, " "))
		for i := 0; i < rows; i++ {
			var t, p string
			if i < len(tree) {
				t = tree[i]
			}
			if i < len(panel) {
				p = panel[i]
			}
			if p == "" {
				lines = append(lines, padStyled(t, l.treeW)+bare)
				continue
			}
			lines = append(lines, padStyled(t, l.treeW)+sep+p)
		}
	case infoBottom:
		rows := l.treeRows
		if l.colHead {
			rows++
		}
		for len(tree) < rows {
			tree = append(tree, "")
		}
		lines = append(lines, tree...)
		lines = append(lines, m.infoPanel(m.width, l.panelRows, infoBottom)...)
	default:
		lines = append(lines, tree...)
	}
	return lines
}

// panelRow is the row the info panel describes: the one under the cursor
// of the section on screen.
func (m *Model) panelRow() (FlatRow, bool) {
	if m.tab == "timeline" {
		r, ok := m.tlCursorRow()
		return r.FlatRow, ok
	}
	return m.cursorRow()
}

// infoPanel renders the info panel for the row under the cursor.
func (m *Model) infoPanel(width, height int, place infoPlacement) []string {
	r, ok := m.panelRow()
	if !ok {
		return []string{m.theme.Muted.Render(truncCell("no node selected", width))}
	}
	n, found := m.nodeMap[r.Row.NodeID]
	groups := nodeInfoGroups(r, n, found && r.Section == "", m.now, m.revealResource, m.theme)
	lines := renderInfo(groups, r, m.revealResource, width, height, place, m.theme)
	if place == infoBottom && len(lines) > 0 {
		// The band's title is also its top edge: a rule across the pane
		// separates it from the tree without spending a row on a border.
		title := lines[0]
		if w := cellWidth(title); w+3 < width {
			title = m.theme.TreeGuide.Render("─ ") + title + " " +
				m.theme.TreeGuide.Render(strings.Repeat("─", width-w-3))
		}
		lines[0] = title
	}
	return lines
}

// nodeTreeLines renders the column heads and exactly the rows that fit,
// scrolled to keep the cursor visible. The window moves only as far as the
// cursor needs, so paging stays steady instead of jumping.
func (m *Model) nodeTreeLines(l nodesLayout) []string {
	if !m.state.Outline.Available {
		reason := m.state.Outline.UnavailableReason
		if reason == "" {
			return []string{"the server did not send node status for this workflow"}
		}
		return []string{shared.Sanitize(reason)}
	}
	if len(m.nodes) == 0 {
		// An empty filter result is not an empty workflow.
		if m.nodePhase != NodePhaseAll && m.totalNodes > 0 {
			return []string{"(no " + string(m.nodePhase) + " nodes — p changes the filter)"}
		}
		return []string{"(no nodes yet — workflow not started)"}
	}
	h := l.treeRows
	top := windowTop(m.nodeTop, m.nodeCursor, h, len(m.nodes))
	m.nodeTop = top
	end := min(top+h, len(m.nodes))
	rr := m.renderer(l.treeW, m.theme)
	matches := m.matchSet()
	out := make([]string, 0, end-top+1)
	if l.colHead {
		out = append(out, rr.header())
	}
	for i := top; i < end; i++ {
		r := m.nodes[i]
		out = append(out, rr.render(r, i == m.nodeCursor, matches[r.Row.NodeID]))
	}
	return out
}

func (m *Model) matchSet() map[string]bool {
	if !m.find.active() || len(m.find.ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(m.find.ids))
	for _, id := range m.find.ids {
		set[id] = true
	}
	return set
}

// nodesStatusLine states what the tab shows and what it is not showing:
// the rows hidden as skipped, the rows inside folds, the phase filter, and
// the find. A pane that quietly shows a subset of the data is the defect
// this line prevents. While the find input is open, the line is the input.
func (m *Model) nodesStatusLine() string {
	t := m.theme
	if !m.state.Outline.Available {
		return t.Dim.Render("node status unavailable")
	}
	if m.find.editing {
		var p pieces
		p.add("find ", t.Accent)
		p.add(oneLine(m.find.query), t.Text)
		p.add("▏", t.Accent)
		if m.find.query != "" {
			p.add("  "+matchCount(len(m.find.ids)), t.Muted)
		}
		if m.width > 0 {
			p = p.truncate(m.width)
		}
		return p.render()
	}
	parts := []string{plural(len(m.nodes), "node")}
	if m.nodePhase != NodePhaseAll {
		parts[0] = strconv.Itoa(len(m.nodes)) + " of " + strconv.Itoa(m.totalNodes) + " nodes"
		parts = append(parts, "phase "+string(m.nodePhase))
	}
	if m.hiddenSkipped > 0 {
		parts = append(parts, strconv.Itoa(m.hiddenSkipped)+" skipped hidden")
	}
	if m.folds > 0 {
		parts = append(parts, strconv.Itoa(m.foldedNodes)+" in "+plural(m.folds, "fold"))
	}
	if m.showInfo && infoPlacementFor(m.width) == infoHidden {
		parts = append(parts, "no room for info")
	}
	if m.find.committed {
		q := "\"" + oneLine(m.find.query) + "\""
		switch {
		case len(m.find.ids) > 0:
			parts = append(parts, "match "+strconv.Itoa(m.find.pos+1)+"/"+strconv.Itoa(len(m.find.ids))+" "+q)
		case m.find.inSkipped > 0:
			parts = append(parts, "no match for "+q+" ("+strconv.Itoa(m.find.inSkipped)+" among hidden skipped)")
		default:
			parts = append(parts, "no match for "+q)
		}
	}
	parts = append(parts, "sort "+string(m.nodeSort))
	if len(m.nodes) > 0 {
		parts = append(parts, strconv.Itoa(m.nodeCursor+1)+"/"+strconv.Itoa(len(m.nodes)))
	}
	if row, ok := m.SelectedNode(); ok && row.HasPod && row.PodName != "" {
		parts = append(parts, "l logs")
	}
	s := strings.Join(parts, " · ")
	if m.width > 0 {
		s = truncCell(s, m.width)
	}
	return t.Dim.Render(s)
}

func matchCount(n int) string {
	switch n {
	case 0:
		return "no match"
	case 1:
		return "1 match"
	default:
		return strconv.Itoa(n) + " matches"
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// nodesHints is the footer for the nodes tab. It names what the keys would
// change right now: h says whether it shows or hides the skipped rows and
// how many there are, and a find puts its own keys first.
func (m *Model) nodesHints() string {
	if m.find.editing {
		return "enter jump  esc cancel"
	}
	var h []string
	if m.find.committed {
		h = append(h, "n next", "N previous", "esc clear find")
	}
	h = append(h, "tab section", "space fold")
	if m.showInfo {
		h = append(h, "i hide info", "v reveal")
	} else {
		h = append(h, "i info")
	}
	h = append(h, "/ find")
	switch {
	case !m.hideSkipped:
		h = append(h, "h hide skipped")
	case m.hiddenSkipped > 0:
		h = append(h, "h show "+strconv.Itoa(m.hiddenSkipped)+" skipped")
	default:
		h = append(h, "h show skipped")
	}
	h = append(h, "s sort", "p phase", "l logs", "a actions", "r refresh", "f raw")
	if !m.find.committed {
		h = append(h, "esc back")
	}
	return strings.Join(h, "  ")
}

// nodesRawLines is the tab as plain text for the full-screen view and the
// clipboard: the header, the column heads and every row at its natural
// width, with ASCII bars.
func (m *Model) nodesRawLines() []string {
	if !m.state.Outline.Available {
		reason := m.state.Outline.UnavailableReason
		if reason == "" {
			reason = "the server did not send node status for this workflow"
		}
		return []string{"node status unavailable: " + shared.Sanitize(reason)}
	}
	plain := shared.NewTheme(true)
	out := []string{progressLine(m.workflow(), m.now, 0, plain)}
	if len(m.nodes) == 0 {
		return append(out, "(no nodes yet — workflow not started)")
	}
	rr := m.renderer(0, plain)
	out = append(out, rr.header())
	for _, r := range m.nodes {
		out = append(out, rr.render(r, false, false))
	}
	return out
}
