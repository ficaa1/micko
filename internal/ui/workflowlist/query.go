package workflowlist

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ficaa1/argo-tui/internal/core"
)

// The list filter is a small query language, parsed once per keystroke into
// a Matcher and then applied to every row of the collected snapshot:
//
//	etl                 name contains "etl" (case-insensitive)
//	/^demo-.*-\d+$/     name matches the regular expression (case-insensitive)
//	~dpl                name contains d, p, l in that order (fuzzy)
//	phase=failed        phase is Failed; phase!=succeeded negates
//	age<2h  age>1d      time since the workflow started (or was created)
//	dur>10m             run time; a workflow still running counts until now
//	label:team=data     label team is "data"; label:team has it; label:!team lacks it
//	tmpl=nightly        started from this WorkflowTemplate or ClusterWorkflowTemplate
//	cron=etl-hourly     started by this CronWorkflow
//
// Spaces separate terms, and every term must match; a lone `&` between terms
// is accepted and means the same. Within a term, `|`
// separates alternatives, and one of them must match. A leading `!` negates
// one alternative, so `!a|b` reads "not a, or b", and "neither a nor b" is
// the two terms `!a !b`. A regular expression is delimited by slashes and may
// hold spaces and `|`; `\/` is a literal slash inside it.
//
// There is no quoting. Workflow names are Kubernetes names, which hold no
// space, `|`, `:`, `=`, `<`, `>` or `!`, so a word holding one of the
// operator characters is always a field predicate, and an unknown field is
// an error rather than a name search that can never match.

// Matcher is a parsed filter. The zero Matcher matches every workflow.
type Matcher struct {
	terms [][]alt // AND of ORs
	// acrossNS is set while the list spans namespaces. A name predicate then
	// also sees the row as "namespace/name".
	acrossNS bool
}

// alt is one alternative of a term: a predicate, possibly negated.
type alt struct {
	neg  bool
	pred predicate
}

// predicate is one test against a workflow summary. now is the snapshot
// time the age and duration predicates measure from.
type predicate interface {
	match(s core.Summary, now time.Time) bool
	String() string
}

// Empty reports whether the matcher has no terms and so matches everything.
func (q Matcher) Empty() bool { return len(q.terms) == 0 }

// Match reports whether s passes every term.
func (q Matcher) Match(s core.Summary, now time.Time) bool {
	for _, term := range q.terms {
		ok := false
		for _, a := range term {
			if q.test(a.pred, s, now) != a.neg {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// AcrossNamespaces returns the matcher set for a list that spans every
// namespace, or for one that does not.
func (q Matcher) AcrossNamespaces(on bool) Matcher {
	q.acrossNS = on
	return q
}

// test runs one predicate. Across namespaces a name predicate matches the
// bare name or "namespace/name", so "team-a/" narrows the list to one
// namespace while an anchored regex such as /^demo-/ still matches names.
// Within one namespace every row shares the namespace, and matching it
// would match everything.
func (q Matcher) test(p predicate, s core.Summary, now time.Time) bool {
	if p.match(s, now) {
		return true
	}
	if !q.acrossNS {
		return false
	}
	switch p.(type) {
	case nameSubstring, nameRegex, nameFuzzy:
		s.Ref.Name = s.Ref.Namespace + "/" + s.Ref.Name
		return p.match(s, now)
	}
	return false
}

// String is the parsed filter in canonical form: terms joined by " & ",
// alternatives by "|", phases capitalised and durations normalised. The
// toolbar shows it, so the reader sees how the query was read rather than
// only what they typed.
func (q Matcher) String() string {
	terms := make([]string, len(q.terms))
	for i, term := range q.terms {
		alts := make([]string, len(term))
		for j, a := range term {
			if a.neg {
				alts[j] = "!" + a.pred.String()
			} else {
				alts[j] = a.pred.String()
			}
		}
		terms[i] = strings.Join(alts, "|")
	}
	return strings.Join(terms, " & ")
}

// ParseQuery parses a filter. An empty or all-space query gives the zero
// Matcher. The error names the first term that could not be read; the
// caller keeps whatever filter it had, so a half-typed query never blanks
// the list.
func ParseQuery(s string) (Matcher, error) {
	p := queryParser{src: []rune(s)}
	var q Matcher
	for {
		p.skipSpace()
		if p.done() {
			return q, nil
		}
		// A lone & is the AND the canonical form spells out between terms.
		// It adds nothing to the space, but accepting it lets the toolbar's
		// form be typed back in.
		if p.peek() == '&' && (p.pos+1 == len(p.src) || unicode.IsSpace(p.src[p.pos+1])) {
			p.pos++
			continue
		}
		term, err := p.term()
		if err != nil {
			return Matcher{}, err
		}
		q.terms = append(q.terms, term)
	}
}

// queryParser walks the query one rune at a time. The regular expression
// is the one construct that can hold a space or `|`, so the split into
// terms and alternatives cannot be a plain strings.Fields.
type queryParser struct {
	src []rune
	pos int
}

func (p *queryParser) done() bool { return p.pos >= len(p.src) }

func (p *queryParser) peek() rune {
	if p.done() {
		return 0
	}
	return p.src[p.pos]
}

func (p *queryParser) skipSpace() {
	for !p.done() && unicode.IsSpace(p.peek()) {
		p.pos++
	}
}

// term reads alternatives separated by `|` up to the next space.
func (p *queryParser) term() ([]alt, error) {
	var out []alt
	for {
		a, err := p.alt()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		if p.peek() != '|' {
			return out, nil
		}
		p.pos++
		if p.done() || unicode.IsSpace(p.peek()) || p.peek() == '|' {
			return nil, errors.New("empty alternative after |")
		}
	}
}

// alt reads one alternative: an optional `!`, then a regular expression or
// a word running to the next space or `|`.
func (p *queryParser) alt() (alt, error) {
	var a alt
	if p.peek() == '|' {
		return a, errors.New("empty alternative before |")
	}
	if p.peek() == '!' {
		a.neg = true
		p.pos++
		if p.done() || unicode.IsSpace(p.peek()) || p.peek() == '|' {
			return a, errors.New("! needs a term after it")
		}
		if p.peek() == '!' {
			return a, errors.New("!! is not a term; use one !")
		}
	}
	if p.peek() == '/' {
		pred, err := p.regex()
		a.pred = pred
		return a, err
	}
	start := p.pos
	for !p.done() && !unicode.IsSpace(p.peek()) && p.peek() != '|' {
		p.pos++
	}
	pred, err := parseWord(string(p.src[start:p.pos]))
	a.pred = pred
	return a, err
}

// regex reads /.../ with `\/` as an escaped slash. What follows the closing
// slash must end the alternative, so `/a/b` is an error rather than a regex
// that silently lost its tail.
func (p *queryParser) regex() (predicate, error) {
	p.pos++ // opening slash
	var b strings.Builder
	for {
		if p.done() {
			return nil, fmt.Errorf("unterminated regex /%s", b.String())
		}
		r := p.src[p.pos]
		p.pos++
		if r == '\\' && p.peek() == '/' {
			b.WriteRune('/')
			p.pos++
			continue
		}
		if r == '/' {
			break
		}
		b.WriteRune(r)
	}
	src := b.String()
	if !p.done() && !unicode.IsSpace(p.peek()) && p.peek() != '|' {
		return nil, fmt.Errorf("text after the closing / of /%s/", src)
	}
	if src == "" {
		return nil, errors.New("empty regex //")
	}
	re, err := regexp.Compile("(?i)" + src)
	if err != nil {
		var se *syntax.Error
		if errors.As(err, &se) {
			return nil, fmt.Errorf("bad regex /%s/: %s", src, se.Code)
		}
		return nil, fmt.Errorf("bad regex /%s/", src)
	}
	return nameRegex{re: re, src: src}, nil
}

// fieldOps lists the operators each field accepts, longest first so `!=`
// is not read as `!` followed by `=`.
var fieldOps = map[string][]string{
	"phase": {"!=", "="},
	"tmpl":  {"!=", "="},
	"cron":  {"!=", "="},
	"age":   {"<", ">"},
	"dur":   {"<", ">"},
	"label": {":"},
}

// parseWord reads a word that is not a regular expression: a fuzzy
// pattern, a field predicate, or a plain name search.
func parseWord(w string) (predicate, error) {
	if w == "" {
		return nil, errors.New("empty term")
	}
	if strings.HasPrefix(w, "~") {
		pat := strings.TrimPrefix(w, "~")
		if pat == "" {
			return nil, errors.New("~ needs letters after it")
		}
		return nameFuzzy{pat: strings.ToLower(pat)}, nil
	}
	if !strings.ContainsAny(w, ":=<>!") {
		return nameSubstring{sub: w}, nil
	}
	// A field name is the run of letters before the operator.
	i := 0
	for i < len(w) && (w[i] >= 'a' && w[i] <= 'z' || w[i] >= 'A' && w[i] <= 'Z') {
		i++
	}
	field := strings.ToLower(w[:i])
	if field == "" {
		return nil, fmt.Errorf("%q is not a term", w)
	}
	ops, known := fieldOps[field]
	if !known {
		return nil, fmt.Errorf("unknown field %q (phase age dur label tmpl cron)", field)
	}
	rest := w[i:]
	op := ""
	for _, o := range ops {
		if strings.HasPrefix(rest, o) {
			op = o
			break
		}
	}
	if op == "" {
		return nil, fmt.Errorf("%s takes %s", field, strings.Join(ops, " or "))
	}
	value := rest[len(op):]
	if value == "" {
		return nil, fmt.Errorf("%s%s needs a value", field, op)
	}
	// No phase, name, duration or label value holds an operator
	// character, so one here is a missing space between two terms.
	if field != "label" {
		if i := strings.IndexAny(value, ":=<>!"); i >= 0 {
			return nil, fmt.Errorf("unexpected %q in %s (a space between terms?)", string(value[i]), w)
		}
	}
	switch field {
	case "phase":
		return phaseIs{want: strings.ToLower(value), not: op == "!="}, nil
	case "tmpl":
		return labelNamed{field: "tmpl", keys: templateLabels, want: value, not: op == "!="}, nil
	case "cron":
		return labelNamed{field: "cron", keys: []string{cronLabel}, want: value, not: op == "!="}, nil
	case "age", "dur":
		d, err := parseSpan(value)
		if err != nil {
			return nil, err
		}
		return timeSpan{field: field, less: op == "<", d: d}, nil
	default: // label
		return parseLabel(value)
	}
}

// parseLabel reads what follows `label:`: `key=value`, `key` or `!key`.
func parseLabel(v string) (predicate, error) {
	if strings.HasPrefix(v, "!") {
		key := v[1:]
		if key == "" || strings.Contains(key, "=") {
			return nil, errors.New("label:! takes a key alone")
		}
		return labelHas{key: key, absent: true}, nil
	}
	key, value, hasValue := strings.Cut(v, "=")
	if key == "" {
		return nil, errors.New("label: needs a key")
	}
	if strings.ContainsAny(key, ":<>!") || strings.ContainsAny(value, ":=<>!") {
		return nil, fmt.Errorf("label:%s is not key, key=value or !key", v)
	}
	if hasValue && value == "" {
		return nil, fmt.Errorf("label:%s= needs a value", key)
	}
	if !hasValue {
		return labelHas{key: key}, nil
	}
	return labelIs{key: key, value: value}, nil
}

// parseSpan reads a duration made of integer parts with the units s, m, h
// and d, such as 90s, 2h or 1d12h. Go's own duration syntax has no day,
// and a day is the unit an age filter most often wants.
func parseSpan(v string) (time.Duration, error) {
	bad := fmt.Errorf("bad duration %q (units s m h d)", v)
	var total time.Duration
	for len(v) > 0 {
		i := 0
		for i < len(v) && v[i] >= '0' && v[i] <= '9' {
			i++
		}
		if i == 0 || i == len(v) {
			return 0, bad
		}
		n, err := strconv.ParseInt(v[:i], 10, 64)
		if err != nil {
			return 0, bad
		}
		var unit time.Duration
		switch v[i] {
		case 's':
			unit = time.Second
		case 'm':
			unit = time.Minute
		case 'h':
			unit = time.Hour
		case 'd':
			unit = 24 * time.Hour
		default:
			return 0, bad
		}
		// A span past a century is a typo, and multiplying it out could
		// overflow into a negative duration that matches the wrong rows.
		if n > int64(maxSpan/unit) || total+time.Duration(n)*unit > maxSpan {
			return 0, fmt.Errorf("duration %q is too long", v)
		}
		total += time.Duration(n) * unit
		v = v[i+1:]
	}
	return total, nil
}

// maxSpan bounds a parsed duration.
const maxSpan = 100 * 365 * 24 * time.Hour

// spanText renders a duration exactly, in the units parseSpan reads, so the
// canonical filter parses back to the same value.
func spanText(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	var b strings.Builder
	for _, u := range []struct {
		size time.Duration
		name string
	}{{24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if n := d / u.size; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10) + u.name)
			d -= n * u.size
		}
	}
	return b.String()
}

// The labels the Argo controller writes on a workflow it starts from a
// template or a CronWorkflow. The list reads the origin from them because
// the list request fetches no spec.
const (
	workflowTemplateLabel        = "workflows.argoproj.io/workflow-template"
	clusterWorkflowTemplateLabel = "workflows.argoproj.io/cluster-workflow-template"
	cronLabel                    = "workflows.argoproj.io/cron-workflow"
	phaseLabel                   = "workflows.argoproj.io/phase"
	completedLabel               = "workflows.argoproj.io/completed"
)

// templateLabels is checked in order: a workflow names at most one of them.
var templateLabels = []string{workflowTemplateLabel, clusterWorkflowTemplateLabel}

// nameSubstring is the plain word: a case-insensitive substring of the name.
type nameSubstring struct{ sub string }

func (p nameSubstring) match(s core.Summary, _ time.Time) bool { return nameMatches(s.Ref.Name, p.sub) }
func (p nameSubstring) String() string                         { return p.sub }

// nameRegex matches the name against a case-insensitive regular expression.
type nameRegex struct {
	re  *regexp.Regexp
	src string
}

func (p nameRegex) match(s core.Summary, _ time.Time) bool { return p.re.MatchString(s.Ref.Name) }
func (p nameRegex) String() string                         { return "/" + strings.ReplaceAll(p.src, "/", `\/`) + "/" }

// nameFuzzy matches when the pattern's runes appear in the name in order,
// case-insensitively: ~dpl finds demo-data-pull.
type nameFuzzy struct{ pat string }

func (p nameFuzzy) match(s core.Summary, _ time.Time) bool {
	want := []rune(p.pat)
	i := 0
	for _, r := range strings.ToLower(s.Ref.Name) {
		if i < len(want) && r == want[i] {
			i++
		}
	}
	return i == len(want)
}
func (p nameFuzzy) String() string { return "~" + p.pat }

// phaseIs compares the phase case-insensitively. "suspended" is the list's
// own reading of a workflow parked on a manual gate. Such a workflow is
// Running to the server, so phase=running includes it, as the Running
// bucket of the phase filter does.
type phaseIs struct {
	want string
	not  bool
}

func (p phaseIs) match(s core.Summary, _ time.Time) bool {
	hit := strings.EqualFold(s.Phase, p.want)
	if p.want == "suspended" {
		hit = s.Suspended
	}
	return hit != p.not
}

func (p phaseIs) String() string {
	op := "="
	if p.not {
		op = "!="
	}
	return "phase" + op + canonicalPhase(p.want)
}

// canonicalPhase spells a known phase the way the list shows it; anything
// else stays as typed, because a server can report phases this build does
// not know.
func canonicalPhase(lower string) string {
	for _, p := range []string{"Suspended", "Running", "Pending", "Succeeded", "Failed", "Error"} {
		if strings.EqualFold(p, lower) {
			return p
		}
	}
	return lower
}

// timeSpan compares the workflow's age or run time with d. A workflow with
// no timestamp to measure matches neither < nor >: it has no age to
// compare, and guessing one would put it on either side of any bound.
type timeSpan struct {
	field string // "age" or "dur"
	less  bool
	d     time.Duration
}

func (p timeSpan) match(s core.Summary, now time.Time) bool {
	var got time.Duration
	switch p.field {
	case "age":
		t := ageKey(s)
		if t.IsZero() || now.IsZero() {
			return false
		}
		got = now.Sub(t)
	default:
		if s.StartedAt == nil || s.StartedAt.IsZero() {
			return false
		}
		end := now
		if s.FinishedAt != nil && !s.FinishedAt.IsZero() {
			end = *s.FinishedAt
		}
		if end.IsZero() {
			return false
		}
		got = end.Sub(*s.StartedAt)
	}
	if p.less {
		return got < p.d
	}
	return got > p.d
}

func (p timeSpan) String() string {
	op := ">"
	if p.less {
		op = "<"
	}
	return p.field + op + spanText(p.d)
}

// labelIs matches a label with exactly this value. Label keys and values
// are case-sensitive in Kubernetes, so the comparison is exact.
type labelIs struct{ key, value string }

func (p labelIs) match(s core.Summary, _ time.Time) bool {
	v, ok := s.Labels[p.key]
	return ok && v == p.value
}
func (p labelIs) String() string { return "label:" + p.key + "=" + p.value }

// labelHas matches a workflow that carries the key, or, with absent, one
// that does not.
type labelHas struct {
	key    string
	absent bool
}

func (p labelHas) match(s core.Summary, _ time.Time) bool {
	_, ok := s.Labels[p.key]
	return ok != p.absent
}

func (p labelHas) String() string {
	if p.absent {
		return "label:!" + p.key
	}
	return "label:" + p.key
}

// labelNamed matches the value of the first of keys the workflow carries,
// case-insensitively: tmpl= and cron= name Kubernetes objects, whose names
// are lowercase, so a capital typed by habit should still match.
type labelNamed struct {
	field string
	keys  []string
	want  string
	not   bool
}

func (p labelNamed) match(s core.Summary, _ time.Time) bool {
	return strings.EqualFold(firstLabel(s, p.keys...), p.want) != p.not
}

func (p labelNamed) String() string {
	op := "="
	if p.not {
		op = "!="
	}
	return p.field + op + p.want
}

// firstLabel returns the value of the first key s carries, or "".
func firstLabel(s core.Summary, keys ...string) string {
	for _, k := range keys {
		if v, ok := s.Labels[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// otherLabels renders the labels no other column shows, sorted by key, as
// key=value pairs. The template and cron labels have their own columns, and
// the phase and completed labels restate PHASE and FINISHED.
func otherLabels(s core.Summary) string {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		switch k {
		case workflowTemplateLabel, clusterWorkflowTemplateLabel, cronLabel, phaseLabel, completedLabel:
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + s.Labels[k]
	}
	return strings.Join(pairs, ",")
}
