package workflowlist

import (
	"sort"
	"strings"

	"github.com/ficaa1/micko/internal/ui/palette"
)

// fieldStarts are the field-and-operator openings a bare word completes to.
var fieldStarts = []string{"phase=", "phase!=", "age<", "age>", "dur<", "dur>", "label:", "tmpl=", "tmpl!=", "cron=", "cron!="}

// phaseValues are the phases phase= completes to, in the list's own order.
var phaseValues = []string{"suspended", "running", "pending", "succeeded", "failed", "error"}

// historyCap bounds the session's filter history.
const historyCap = 50

// pickerMin is the number of completions that opens the picker.
const pickerMin = 4

// completion is what tab can put in place of the word at the cursor: the
// rune range it replaces, the ranked replacements and the number of values
// they were ranked from. Only values after an operator go to the picker: a
// bare word may be a name search, and a picker over it would cover the list.
type completion struct {
	start, end int
	values     []string
	total      int
	pickable   bool
}

// tabCycle is the completion tab steps through, fixed at the first tab.
type tabCycle struct {
	base string
	completion
	i int
}

// completions returns what the word at the cursor can complete to: a field
// for a bare word, or a phase, template, cron workflow, label key or label
// value from the loaded workflows.
func (m *Model) completions() completion {
	runes := []rune(m.searchBuf)
	cur := min(max(m.searchCur, 0), len(runes))
	start := cur
	for start > 0 && runes[start-1] != ' ' && runes[start-1] != '|' {
		start--
	}
	end := cur
	for end < len(runes) && runes[end] != ' ' && runes[end] != '|' {
		end++
	}
	word := string(runes[start:cur])
	if strings.HasPrefix(word, "!") {
		word, start = word[1:], start+1
	}
	if word == "" || word[0] == '/' || word[0] == '~' {
		return completion{}
	}
	i := 0
	for i < len(word) && (word[i] >= 'a' && word[i] <= 'z' || word[i] >= 'A' && word[i] <= 'Z') {
		i++
	}
	if i == len(word) {
		return completion{start, end, ranked(word, fieldStarts, false), len(fieldStarts), false}
	}
	field := strings.ToLower(word[:i])
	op := ""
	for _, o := range fieldOps[field] {
		if strings.HasPrefix(word[i:], o) {
			op = o
			break
		}
	}
	if op == "" {
		return completion{}
	}
	value := word[i+len(op):]
	start += len([]rune(word[:i+len(op)]))
	var pool []string
	switch field {
	case "phase":
		pool = phaseValues
	case "tmpl":
		pool = m.labelValues(templateLabels...)
	case "cron":
		pool = m.labelValues(cronLabel)
	case "label":
		key, partial, hasValue := strings.Cut(value, "=")
		switch {
		case strings.HasPrefix(value, "!"):
			value, start = value[1:], start+1
			pool = m.labelKeys("")
		case hasValue:
			value, start = partial, start+len([]rune(key))+1
			pool = m.labelValues(key)
		default:
			pool = m.labelKeys("=")
		}
	}
	return completion{start, end, ranked(value, pool, true), len(pool), true}
}

// ranked returns the values that match query, best first. keepTyped keeps a
// value equal to query, so the picker's highlight can rest on what was typed.
func ranked(query string, values []string, keepTyped bool) []string {
	cands := make([]palette.Candidate, len(values))
	for i, v := range values {
		cands[i] = palette.Candidate{Terms: []string{v}}
	}
	var out []string
	for _, r := range palette.Rank(query, cands) {
		if keepTyped || r.Term != query {
			out = append(out, r.Term)
		}
	}
	return out
}

// labelKeys returns every label key in the snapshot, sorted, each followed
// by suffix.
func (m *Model) labelKeys(suffix string) []string {
	seen := map[string]bool{}
	for _, it := range m.items {
		for k := range it.Labels {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k+suffix)
	}
	sort.Strings(out)
	return out
}

// labelValues returns the values the snapshot holds for the first of keys
// each workflow carries, sorted.
func (m *Model) labelValues(keys ...string) []string {
	seen := map[string]bool{}
	for _, it := range m.items {
		if v := firstLabel(it, keys...); v != "" {
			seen[v] = true
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// picker returns the picker's completions and whether it is open. It stays
// open as the matches narrow, even to none.
func (m *Model) picker() (completion, bool) {
	if !m.pickOpen {
		return completion{}, false
	}
	c := m.completions()
	return c, c.pickable
}

// syncPicker opens or closes the picker after an edit.
func (m *Model) syncPicker() {
	c := m.completions()
	switch {
	case !c.pickable:
		m.pickOpen = false
	case len(c.values) >= pickerMin:
		m.pickOpen = true
	}
	m.pickSel = 0
}

// complete puts the next (step 1) or previous (step -1) completion in place
// of the word at the cursor. With enough of them it opens the picker instead.
func (m *Model) complete(step int) {
	if m.tabs == nil {
		c := m.completions()
		if c.pickable && len(c.values) >= pickerMin {
			m.pickOpen, m.pickSel = true, 0
			return
		}
		if len(c.values) == 0 {
			return
		}
		m.tabs = &tabCycle{base: m.searchBuf, completion: c, i: -1}
		if step < 0 {
			m.tabs.i = 0
		}
	}
	t := m.tabs
	t.i = (t.i + step + len(t.values)) % len(t.values)
	m.replaceWord(t.base, t.completion, t.values[t.i])
	// A single value leaves nothing to cycle through, so the next tab
	// completes the word that value began, such as label: to a key.
	if len(t.values) == 1 {
		m.tabs = nil
		m.pickOpen, m.pickSel = opensNext(t.values[0]), 0
	}
}

// accept puts the picked value in place of the word at the cursor, and keeps
// the picker open on what follows a value such as label: or stack=.
func (m *Model) accept(c completion, value string) {
	m.replaceWord(m.searchBuf, c, value)
	m.pickOpen, m.pickSel = opensNext(value), 0
}

// typed reports whether the word c replaces already reads value, ignoring
// case and the operator a key or field ends in.
func (m *Model) typed(c completion, value string) bool {
	word := string([]rune(m.searchBuf)[c.start:c.end])
	return strings.EqualFold(word, value) || strings.EqualFold(word, strings.TrimRight(value, ":=<>"))
}

// opensNext reports whether value ends in an operator, after which another
// value is completed.
func opensNext(value string) bool {
	return value != "" && strings.ContainsAny(value[len(value)-1:], ":=<>")
}

// replaceWord sets the buffer to base with c's range replaced by value, the
// cursor after it, and applies the result.
func (m *Model) replaceWord(base string, c completion, value string) {
	runes := []rune(base)
	v := []rune(value)
	m.searchBuf = string(runes[:c.start]) + value + string(runes[c.end:])
	m.searchCur = c.start + len(v)
	m.applyLiveQuery()
}

// remember adds an applied filter to the history, moving a repeat to the
// newest place.
func (m *Model) remember(q string) {
	if q == "" {
		return
	}
	for i, h := range m.history {
		if h == q {
			m.history = append(m.history[:i], m.history[i+1:]...)
			break
		}
	}
	m.history = append(m.history, q)
	if len(m.history) > historyCap {
		m.history = m.history[len(m.history)-historyCap:]
	}
}

// recall walks the history, then the draft, then an empty filter, skipping
// steps that would not change the text.
func (m *Model) recall(delta int) {
	if m.histPos == len(m.history) {
		m.draft = m.searchBuf
	}
	pos, last := m.histPos, len(m.history)+1
	for {
		pos += delta
		if pos < 0 || pos > last {
			return
		}
		if pos == last || m.recalled(pos) != m.searchBuf {
			break
		}
	}
	m.histPos = pos
	m.searchBuf = m.recalled(pos)
	m.searchCur = len([]rune(m.searchBuf))
	m.pickOpen = false
	m.applyLiveQuery()
}

// recalled is the text at a history position: an entry, the draft, or the
// empty filter past it.
func (m *Model) recalled(pos int) string {
	switch {
	case pos < len(m.history):
		return m.history[pos]
	case pos == len(m.history):
		return m.draft
	}
	return ""
}
