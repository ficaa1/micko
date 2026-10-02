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

// completion is what tab can put in place of the word at the cursor: the
// rune range it replaces and the ranked replacements.
type completion struct {
	start, end int
	values     []string
}

// tabCycle is the completion tab is stepping through, kept from the first
// tab so that the next one moves to the following value instead of
// completing the value just inserted.
type tabCycle struct {
	base string
	completion
	i int
}

// completions returns what the word at the cursor can complete to: a field
// for a bare word, then a phase, template, cron workflow, label key or label
// value from the loaded workflows. A regex, a fuzzy name or a duration
// completes to nothing.
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
		return completion{start, end, ranked(word, fieldStarts)}
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
	return completion{start, end, ranked(value, pool)}
}

// ranked returns the values that match query, best first, leaving out the
// one already typed in full.
func ranked(query string, values []string) []string {
	cands := make([]palette.Candidate, len(values))
	for i, v := range values {
		cands[i] = palette.Candidate{Terms: []string{v}}
	}
	var out []string
	for _, r := range palette.Rank(query, cands) {
		if r.Term != query {
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

// complete puts the next (step 1) or previous (step -1) completion in place
// of the word at the cursor.
func (m *Model) complete(step int) {
	if m.tabs == nil {
		c := m.completions()
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
	runes := []rune(t.base)
	value := []rune(t.values[t.i])
	m.searchBuf = string(runes[:t.start]) + string(value) + string(runes[t.end:])
	m.searchCur = t.start + len(value)
	// A single value leaves nothing to cycle through, so the next tab
	// completes the word that value began, such as label: to a key.
	if len(t.values) == 1 {
		m.tabs = nil
	}
	m.applyLiveQuery()
}

// remember adds an applied filter to the history. A filter already there
// moves to the newest place, so recalling steps through different filters.
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

// recall walks the history. Walking forward past the newest entry returns the
// text the reader was typing before they started walking.
func (m *Model) recall(delta int) {
	pos := m.histPos + delta
	if len(m.history) == 0 || pos < 0 || pos > len(m.history) {
		return
	}
	if m.histPos == len(m.history) {
		m.draft = m.searchBuf
	}
	m.histPos = pos
	if pos == len(m.history) {
		m.searchBuf = m.draft
	} else {
		m.searchBuf = m.history[pos]
	}
	m.searchCur = len([]rune(m.searchBuf))
	m.applyLiveQuery()
}
