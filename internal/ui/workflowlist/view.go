package workflowlist

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Update handles navigation, local filters and intents for the selected workflow.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return nil
	default:
		return nil
	}
}

// handleKey implements the key matrix above.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if m.SearchOn {
		return m.handleSearchKey(key, msg)
	}
	// vim's gg: the first g arms, the second jumps to the top. Any other key
	// disarms and is then handled normally, so g never swallows a command.
	if m.gPending {
		m.gPending = false
		if key == "g" {
			m.moveTo(0)
			return nil
		}
	} else if key == "g" {
		m.gPending = true
		return nil
	}
	switch key {
	case "j", "down":
		m.move(1)
		return nil
	case "k", "up":
		m.move(-1)
		return nil
	case "pgdown", "ctrl+d":
		m.move(m.pageStep())
		return nil
	case "pgup", "ctrl+u":
		m.move(-m.pageStep())
		return nil
	case "home":
		m.moveTo(0)
		return nil
	case "G", "end":
		m.moveTo(len(m.rows) - 1)
		return nil
	case "enter":
		if intent := m.OpenIntent(); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	case "l":
		if intent := m.LogsIntent(); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	case "T":
		// Open the workflow straight on its timeline: the question "where
		// did the time go" is often the reason to open it at all.
		if intent := m.OpenSectionIntent(shared.SectionTimeline); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	case "X":
		// Open the workflow straight on its explanation: for a failed run,
		// "why" is the first question.
		if intent := m.OpenSectionIntent(shared.SectionExplain); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	case "E":
		// Open the workflow straight on its Kubernetes events: a pod that
		// cannot be scheduled or pulled says why only there.
		if intent := m.OpenSectionIntent(shared.SectionEvents); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	case "/":
		m.SearchOn = true
		m.queryBefore = m.query
		m.searchBuf = m.query
		m.searchCur = len([]rune(m.searchBuf))
		m.histPos = len(m.history)
		m.pickOpen = false
		return nil
	case "s":
		m.CycleSort()
		return nil
	case "p":
		m.CyclePhase()
		return nil
	case "space":
		m.ToggleMark()
		return nil
	case "w":
		m.ToggleWide()
		return nil
	case "esc":
		// Esc clears the marks first and the filter on the next press. The
		// marks go first because they are the state that can act: a bulk
		// action reaches every marked workflow, hidden ones included, and a
		// reader backing out wants that set gone before anything else.
		//
		// A filter matching nothing renders exactly like an empty
		// namespace, so without the second step the only way out is to
		// reopen the search and submit an empty buffer. With neither
		// applied Esc stays inert: it must never become a surprise quit on
		// the top route.
		if len(m.marks) > 0 {
			m.ClearMarks()
			return nil
		}
		if m.query != "" {
			m.SetQuery("")
		}
		return nil
	case "r":
		if intent := m.RefreshIntent(); intent != nil {
			return func() tea.Msg { return intent }
		}
		return nil
	default:
		// Everything else (q, n, ?, ...) belongs to the root; the child
		// must not shadow it.
		return nil
	}
}

// handleSearchKey routes keys while search has focus. Text-entry isolation:
// printable characters reach the buffer; `q` inserts a letter;
// navigation command keys are not interpreted; enter applies; esc cancels.
// Tab completes, up and down walk the history, and Home/End/Backspace/
// arrow-left/right edit; everything else is ignored (never a command). In
// the picker, the arrows and tab move and enter accepts, unless the word
// already reads the highlighted value: a filter typed in full applies on
// the first enter.
func (m *Model) handleSearchKey(key string, _ tea.KeyPressMsg) tea.Cmd {
	if key != "tab" && key != "shift+tab" {
		m.tabs = nil
	}
	if c, ok := m.picker(); ok {
		n := len(c.values)
		switch key {
		case "up", "ctrl+p", "shift+tab":
			if n > 0 {
				m.pickSel = (m.pickSel%n - 1 + n) % n
			}
			return nil
		case "down", "ctrl+n", "tab":
			if n > 0 {
				m.pickSel = (m.pickSel%n + 1) % n
			}
			return nil
		case "enter":
			if n > 0 && !m.typed(c, c.values[m.pickSel%n]) {
				m.accept(c, c.values[m.pickSel%n])
				return nil
			}
		case "esc":
			m.pickOpen = false
			return nil
		}
	}
	switch key {
	case "tab":
		m.complete(1)
		return nil
	case "shift+tab":
		m.complete(-1)
		return nil
	case "up", "ctrl+p":
		m.recall(-1)
		return nil
	case "down", "ctrl+n":
		m.recall(1)
		return nil
	case "esc":
		// Cancel restores the filter that was applied before the input took
		// focus. Typing filters live, so "cancel" has to undo the typing, not
		// clear whatever filter the user already had.
		m.SearchOn = false
		m.searchBuf, m.searchCur = "", 0
		m.SetQuery(m.queryBefore)
		m.queryBefore, m.queryErr = "", ""
		return nil
	case "enter":
		// A query that does not parse cannot be applied, so enter leaves
		// the input open on it with the error beside it. Closing the input
		// would throw the typed text away and leave the reader looking at a
		// filter they did not ask for.
		if m.queryErr != "" {
			return nil
		}
		m.SearchOn = false
		m.applyLiveQuery()
		m.remember(m.query)
		m.searchBuf, m.searchCur = "", 0
		m.queryBefore = ""
		return nil
	case "backspace":
		m.searchBackspace()
		m.applyLiveQuery()
		return nil
	case "left":
		m.searchLeft()
		return nil
	case "right":
		m.searchRight()
		return nil
	case "home", "ctrl+a":
		m.searchCur = 0
		return nil
	case "end", "ctrl+e":
		m.searchCur = len([]rune(m.searchBuf))
		return nil
	case "space":
		// The space bar reports itself by name, not as " ", so the
		// printable branch below would drop it. The filter separates its
		// terms with spaces.
		m.searchInsert(' ')
		m.applyLiveQuery()
		return nil
	default:
		// Printable text goes to the buffer; every non-printable key is
		// dropped (input-isolation: no command interpretation).
		if runes := []rune(key); len(runes) == 1 && !isControlRune(runes[0]) {
			m.searchInsert(runes[0])
			m.applyLiveQuery()
		}
		return nil
	}
}

// applyLiveQuery re-filters on every keystroke, so the list narrows as the
// user types instead of only on Enter. Filtering is local to the collected
// snapshot, so it costs one pass over rows already in memory.
//
// The cursor is parked at the top of each new result set: after typing, the
// first match is what the reader is looking at. Text that does not parse
// leaves the rows and the cursor where they are; the toolbar says why.
func (m *Model) applyLiveQuery() {
	q := strings.TrimSpace(m.searchBuf)
	if q == m.query {
		m.queryErr = ""
		return
	}
	match, err := ParseQuery(q)
	if err != nil {
		m.queryErr = err.Error()
		return
	}
	m.selUID = ""
	m.scrollTop = 0
	m.query, m.match, m.queryErr = q, match, ""
	m.applyView()
}

// pageStep is how far pgup/pgdown move: one visible window minus a line of
// overlap, so the reader keeps a row of context across the jump.
func (m *Model) pageStep() int {
	n := m.winEnd - m.winStart
	if n <= 1 {
		return 1
	}
	return n - 1
}

// moveTo selects the row at index i (clamped). g and G use it to jump to the
// first and last row.
func (m *Model) moveTo(i int) {
	if len(m.rows) == 0 {
		m.selUID = ""
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	m.selUID = m.rows[i].Ref.UID
}

// isControlRune reports whether r is a control character (never inserted
// into the search buffer; the sanitizer keeps \n/	 only for display, and
// a single-line filter needs neither).
func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// rowPhaseText renders the phase cell: text always present (color is
// supplementary).
func (m *Model) rowPhaseText(phase string) string {
	if phase == "" {
		return "(no phase)"
	}
	return phase
}

// ageText renders the AGE cell from the most informative timestamp. Missing
// timestamps render as "-" (sensible, never garbage).
func ageText(s core.Summary, now time.Time) string {
	t := s.CreatedAt
	if s.StartedAt != nil && !s.StartedAt.IsZero() {
		t = *s.StartedAt
	}
	if t.IsZero() || now.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	return humanDuration(d)
}

// durationText renders the DURATION cell; missing endpoints render "-".
func durationText(s core.Summary) string {
	start := s.StartedAt
	if start == nil || start.IsZero() {
		return "-"
	}
	if s.FinishedAt == nil || s.FinishedAt.IsZero() {
		return "ongoing"
	}
	d := s.FinishedAt.Sub(*start)
	if d < 0 {
		d = 0
	}
	return humanDuration(d)
}

// progressCell renders the server's "done/total" count right-aligned in
// five cells, then a bar of the remaining width. The bar is made of glyphs,
// full blocks for done and light shade for the rest, so it reads under
// NO_COLOR; the row's phase style colours it. A count that does not parse
// is shown as sent, without a bar, and an empty one as "-" in the count's
// place.
func progressCell(p string, width int) string {
	p = sanitizeOne(p)
	if p == "" {
		return padLeft("-", 5)
	}
	done, total, ok := parseProgress(p)
	if !ok {
		return truncateRight(p, width)
	}
	count := padLeft(p, 5)
	bar := width - ansi.StringWidth(count) - 1
	if bar < 1 {
		return truncateRight(p, width)
	}
	filled := 0
	if total > 0 {
		filled = done * bar / total
	}
	// Only a finished count fills the bar. Rounding down means a workflow
	// one pod short of done never looks complete.
	return count + " " + strings.Repeat("█", filled) + strings.Repeat("░", bar-filled)
}

// parseProgress reads "done/total" with 0 <= done <= total.
func parseProgress(p string) (done, total int, ok bool) {
	a, b, found := strings.Cut(p, "/")
	if !found {
		return 0, 0, false
	}
	done, err1 := strconv.Atoi(a)
	total, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || done < 0 || total < 0 || done > total {
		return 0, 0, false
	}
	return done, total, true
}

// clockText renders a timestamp as local "01-02 15:04" for the STARTED and
// FINISHED columns: month and day because the snapshot spans days, minutes
// because the AGE and DURATION columns already give the finer view. A
// missing timestamp is "-".
func (m *Model) clockText(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.In(time.Local).Format("01-02 15:04")
}

// orDash stands in "-" for an empty cell, so an empty column reads as "none"
// rather than as a rendering gap.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// truncateRight clips s to width cells, appending a literal ellipsis when
// cut (never mid-sequence: ansi-aware).
func truncateRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	// Walk grapheme-ish via runes until width-1 for the ellipsis.
	var b strings.Builder
	w := 0
	limit := width - 1
	for _, r := range s {
		rw := ansi.StringWidth(string(r))
		if w+rw > limit {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// padRight pads text to width terminal cells.
func padRight(s string, width int) string {
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// padLeft right-aligns text in width terminal cells.
func padLeft(s string, width int) string {
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

// humanDuration renders compact list durations without seconds above a minute.
func humanDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		days := int(d / (24 * time.Hour))
		hours := int(d%(24*time.Hour)) / int(time.Hour)
		if hours > 0 {
			return strconv.Itoa(days) + "d" + strconv.Itoa(hours) + "h"
		}
		return strconv.Itoa(days) + "d"
	case d >= time.Hour:
		h := int(d / time.Hour)
		mins := int(d%time.Hour) / int(time.Minute)
		if mins > 0 {
			return strconv.Itoa(h) + "h" + strconv.Itoa(mins) + "m"
		}
		return strconv.Itoa(h) + "h"
	case d >= time.Minute:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	default:
		return strconv.Itoa(int(d/time.Second)) + "s"
	}
}
