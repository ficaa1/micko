package diagnose

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// unit is one root failure. A failing pod is its own unit; the attempts of
// a Retry node are one unit, the Retry, because they are one step failing
// again and again.
type unit struct {
	id string
	// leaves are the failed nodes with no failed node under them, in the
	// order they finished.
	leaves []string
	// first is when the first of them finished.
	first time.Time
}

// rootFailures finds the nodes that failed on their own, first failure
// first. A node counts when it failed and nothing under it did: a DAG or
// Steps node that failed because a child failed is not a cause, it is the
// news travelling up. A failure that waited for another failure, through
// continueOn or a depends expression, followed from it and is not a root
// either. Failures in the exit handler and in hooks are left to their own
// rules, a Retry that succeeded in the end to the retried rule, and every
// failure in a workflow that succeeded anyway to the tolerated rule.
func (x *index) rootFailures() []unit {
	if x.in.Workflow.Summary.Phase == "Succeeded" {
		return nil
	}
	byID := map[string]*unit{}
	var units []*unit
	for _, id := range x.order {
		if !x.failedLeaf(id) {
			continue
		}
		uid := x.unitOf(id)
		u := byID[uid]
		if u == nil {
			u = &unit{id: uid}
			byID[uid] = u
			units = append(units, u)
		}
		u.leaves = append(u.leaves, id)
	}
	out := make([]unit, 0, len(units))
	for _, u := range units {
		if x.waitedOnFailure(u.id) || x.nodes[u.id].Phase == "Succeeded" {
			continue
		}
		sort.SliceStable(u.leaves, func(i, j int) bool {
			return x.finishedAt(u.leaves[i]).Before(x.finishedAt(u.leaves[j]))
		})
		u.first = x.finishedAt(u.leaves[0])
		out = append(out, *u)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].first, out[j].first
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return a.Before(b)
	})
	return out
}

// failedLeaf reports whether a node failed on its own account: it failed,
// nothing under it failed, it does work rather than only group other nodes,
// and it belongs to the workflow's own tree. A DAG or Steps node that failed
// with no failed child was failed from above, by a stop or a deadline, and
// the workflow's message tells that story.
func (x *index) failedLeaf(id string) bool {
	n := x.nodes[id]
	if x.role[id] != "" || !failedPhase(n.Phase) || x.failedBelow[id] {
		return false
	}
	switch n.Type {
	case "DAG", "Steps", "StepGroup", "TaskGroup":
		return false
	}
	return true
}

// unitOf is the root-failure unit a failed node belongs to: the nearest
// Retry above it, or the node itself.
func (x *index) unitOf(id string) string {
	for cur := x.parent[id]; cur != ""; cur = x.parent[cur] {
		if x.nodes[cur].Type == "Retry" {
			return cur
		}
	}
	return id
}

// waitedOnFailure reports whether id, or a group it sits in, waited for a
// node that failed, or held a failure, before it could start.
func (x *index) waitedOnFailure(id string) bool {
	for cur := id; cur != ""; cur = x.parent[cur] {
		for _, a := range x.branch[cur].After {
			if failedPhase(x.nodes[a].Phase) || x.failedBelow[a] {
				return true
			}
		}
	}
	return false
}

// finishedAt is when a node stopped, or the zero time when it never ran.
func (x *index) finishedAt(id string) time.Time {
	if n := x.nodes[id]; n.FinishedAt != nil {
		return *n.FinishedAt
	}
	_, end, _ := x.ran(id)
	return end
}

// cause is what the evidence says stopped one node.
type cause struct {
	kind string
	// short is the cause in a few words, for a headline.
	short string
	// meaning explains it in a sentence; next is what to do about it.
	meaning, next string
	// reason is the Kubernetes reason a message named, such as
	// ImagePullBackOff.
	reason string
}

var (
	oomPattern      = regexp.MustCompile(`(?i)\bOOMKilled\b|\bout of memory\b`)
	deadlinePattern = regexp.MustCompile(`(?i)deadline exceeded|exceeded its deadline|\bDeadlineExceeded\b|longer than the specified deadline|max duration limit exceeded`)
	imagePattern    = regexp.MustCompile(`\b(ErrImagePull|ImagePullBackOff|InvalidImageName|ErrImageNeverPull)\b`)
	pendingPattern  = regexp.MustCompile(`\b(Unschedulable|CreateContainerConfigError|CreateContainerError|CrashLoopBackOff|ContainerCreating|PodInitializing|FailedMount|FailedScheduling|RunContainerError)\b`)
)

// causeOf reads a node's exit code and message for what stopped it. The
// message outranks a bare exit code: 137 alone is any SIGKILL, while
// "OOMKilled" says which one.
func causeOf(n core.Node) cause {
	msg := n.Message
	switch {
	case oomPattern.MatchString(msg) || (n.ExitCode == "137" && strings.Contains(strings.ToLower(msg), "oom")):
		return cause{
			kind: CauseOOM, short: "out of memory (OOMKilled, exit code 137)",
			meaning: "the container used more memory than its limit and the kernel killed it",
			next:    "Raise the memory limit of template " + quoteOr(n.TemplateName, "the step") + " (resources.limits.memory), or make the step hold less in memory.",
		}
	case deadlinePattern.MatchString(msg):
		return cause{
			kind: CauseDeadline, short: "deadline exceeded",
			meaning: "the step ran longer than its activeDeadlineSeconds or the retry's maxDuration allowed",
			next:    "If the work is legitimately slow, raise activeDeadlineSeconds; otherwise find what made it slow on the Timeline (T).",
		}
	}
	if m := imagePattern.FindString(msg); m != "" {
		return cause{
			kind: CauseImagePull, short: "could not pull its image (" + m + ")", reason: m,
			meaning: "the kubelet could not pull the container image",
			next:    "Check that the image name and tag exist, and that the pod's service account or imagePullSecrets can read the registry.",
		}
	}
	if m := pendingPattern.FindString(msg); m != "" {
		c := pendingCause(m)
		c.kind = CausePodPending
		c.short = "its pod never started (" + m + ")"
		return c
	}
	if n.ExitCode != "" && n.ExitCode != "0" {
		code, _ := strconv.Atoi(n.ExitCode)
		meaning, next := exitCodeMeaning(code)
		return cause{
			kind: CauseExitCode, short: "exit code " + n.ExitCode,
			meaning: meaning, next: next,
		}
	}
	return cause{
		kind: CauseMessage, short: firstLine(msg),
		next: "Read the node's message and its log (enter on the node in Nodes) for what stopped it.",
	}
}

// pendingCause explains a pod-pending reason Kubernetes reports.
func pendingCause(reason string) cause {
	c := cause{reason: reason}
	switch reason {
	case "Unschedulable", "FailedScheduling":
		c.meaning = "no node could take the pod"
		c.next = "Compare the pod's CPU and memory requests, node selectors, affinity and tolerations with the nodes available, and check the namespace's quota."
	case "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
		c.meaning = "the kubelet could not create the container from its spec"
		c.next = "Check that every secret, config map and volume the template references exists in the namespace."
	case "CrashLoopBackOff":
		c.meaning = "the container keeps exiting as soon as it starts"
		c.next = "Read the container's log for why it exits at start-up."
	case "FailedMount":
		c.meaning = "a volume could not be mounted"
		c.next = "Check the persistent volume claims and secrets the template mounts."
	default:
		c.meaning = "the pod is still being set up: volumes, init containers or the image"
		c.next = "Check the pod's events (E) for what it is waiting on."
	}
	return c
}

// exitCodeMeaning explains the exit codes worth knowing by heart, and what
// to do about each.
func exitCodeMeaning(code int) (meaning, next string) {
	switch code {
	case 1:
		return "the program reported a general error",
			"Fix the error the program logged."
	case 2:
		return "a usage error or a failed check (many CLIs and test tools exit 2 for either)",
			"Check the command's arguments and inputs, and the log for the check that failed."
	case 126:
		return "the command was found but could not be executed",
			"Check the entrypoint's execute permission and shebang, and that the image matches the node's CPU architecture."
	case 127:
		return "the command was not found",
			"Check the template's command and args, and that the image has that program on its PATH."
	case 137:
		return "killed by SIGKILL: out of memory without the OOMKilled reason, an eviction, a preemption or a forced stop",
			"Check the pod's events (E) for an eviction or preemption, and its memory use against its limit."
	case 139:
		return "a segmentation fault (SIGSEGV): the program crashed in native code",
			"Read the log for the crash, and check the image's native libraries and versions."
	case 143:
		return "terminated by SIGTERM: the workflow was stopped or terminated, hit a deadline, or the pod was evicted or preempted",
			"Check whether someone stopped the workflow, and the pod's events (E) for an eviction or preemption."
	}
	if code > 128 && code < 160 {
		return fmt.Sprintf("killed by signal %d", code-128),
			"Check the pod's events (E) and the log for what sent the signal."
	}
	return "the program's own exit status",
		"Fix the error the program logged; its documentation says what the status means."
}

// failureFinding is the card for one root failure: which node, when, what
// stopped it, and for a Retry every attempt and whether they failed alike.
func (x *index) failureFinding(u unit, first, several bool) Finding {
	last := u.leaves[len(u.leaves)-1]
	n := x.nodes[last]
	c := causeOf(n)
	f := Finding{Rule: RuleRootFailure, Severity: Error, Cause: c.kind, NodeID: u.id}

	f.Headline = x.name(last) + " failed" + colonThen(c.short)
	if x.nodes[u.id].Type == "Retry" {
		var story retryOutcome
		f.Headline, f.Evidence, story = x.retryStory(u, c)
		switch story {
		case retriesSame:
			f.Next = "Retrying did not help: every attempt failed the same way, so the cause is not transient. "
		case retriesDiffer:
			f.Next = "The attempts failed in different ways, which points at something transient, such as node pressure or a flaky dependency. "
		case retriesGoing:
			f.Severity = Warning
		}
	}

	ev := []Evidence{{Label: "node", Text: x.nodeLine(last)}}
	if p := x.path(last); len(p) > 1 {
		ev = append(ev, Evidence{Label: "path", Text: strings.Join(p, " › ")})
	}
	when := x.whenLine(last)
	if several && first {
		when = joinSentence(when, "the first failure")
	}
	if when != "" {
		ev = append(ev, Evidence{Label: "when", Text: when})
	}
	if n.ExitCode != "" {
		text := n.ExitCode
		if c.meaning != "" && c.kind != CauseMessage {
			text += ": " + c.meaning
		}
		ev = append(ev, Evidence{Label: "exit code", Text: text})
	} else if c.meaning != "" {
		ev = append(ev, Evidence{Label: "reason", Text: c.meaning})
	}
	if n.Message != "" {
		ev = append(ev, Evidence{Label: "message", Text: firstLine(n.Message)})
	}
	if c.kind == CauseDeadline {
		if d := x.spec.ActiveDeadlineSeconds; d != nil {
			ev = append(ev, Evidence{Label: "deadline", Text: "activeDeadlineSeconds " + strconv.FormatInt(*d, 10) + " on the workflow"})
		}
	}
	if n.PodName != "" {
		pod := n.PodName
		if n.HostNodeName != "" {
			pod += " on " + n.HostNodeName
		}
		ev = append(ev, Evidence{Label: "pod", Text: pod})
	}
	if len(n.Inputs.Parameters) > 0 {
		ev = append(ev, Evidence{Label: "inputs", Params: n.Inputs.Parameters})
	}
	f.Evidence = append(ev, f.Evidence...)
	f.Next += c.next
	if finishedPhase(x.in.Workflow.Summary.Phase) {
		f.Next = joinSentence(f.Next, "Then retry the workflow (a, r) to rerun only what failed.")
	}
	return f
}

// retryOutcome is how a Retry node's attempts went.
type retryOutcome int

const (
	// retriesOne means a single attempt failed: nothing to compare.
	retriesOne retryOutcome = iota
	// retriesSame means every attempt failed with the same exit code and
	// message.
	retriesSame
	// retriesDiffer means the attempts failed in different ways.
	retriesDiffer
	// retriesGoing means the Retry node is still retrying.
	retriesGoing
)

// retryStory tells a Retry node's attempts: the headline, one evidence line
// listing each attempt with its exit code and duration, and whether they
// failed alike.
func (x *index) retryStory(u unit, c cause) (string, []Evidence, retryOutcome) {
	retry := x.name(u.id)
	var attempts []string
	var failedAttempts []core.Node
	for _, b := range x.branch[u.id].Children {
		n := x.nodes[b.ID]
		leaf := n
		for _, l := range u.leaves {
			if x.under(l, b.ID) {
				leaf = x.nodes[l]
				break
			}
		}
		s := x.name(b.ID) + " " + phaseWord(n.Phase)
		if leaf.ExitCode != "" {
			s += " exit " + leaf.ExitCode
		}
		if start, end, ok := x.ran(b.ID); ok {
			s += " after " + shared.ShortDuration(end.Sub(start))
		}
		attempts = append(attempts, s)
		if failedPhase(n.Phase) {
			failedAttempts = append(failedAttempts, leaf)
		}
	}
	ev := []Evidence{{Label: "attempts", Text: strings.Join(attempts, " · ")}}
	// Attempts that failed with no exit code and no message failed in
	// ways nothing recorded, and cannot be called the same.
	same := len(failedAttempts) > 1 && (failedAttempts[0].ExitCode != "" || failedAttempts[0].Message != "")
	for _, a := range failedAttempts[min(1, len(failedAttempts)):] {
		if a.ExitCode != failedAttempts[0].ExitCode || firstLine(a.Message) != firstLine(failedAttempts[0].Message) {
			same = false
		}
	}
	nFailed := len(failedAttempts)
	if !finishedPhase(x.nodes[u.id].Phase) {
		return retry + " failed " + plural(nFailed, "attempt") + " so far and is retrying" + colonThen(c.short), ev, retriesGoing
	}
	if nFailed <= 1 {
		return retry + " failed" + colonThen(c.short), ev, retriesOne
	}
	if same {
		how := c.short
		if c.kind == CauseExitCode {
			how += " each time"
		}
		return retry + " failed all " + strconv.Itoa(nFailed) + " attempts: " + how, ev, retriesSame
	}
	var codes []string
	recorded := false
	for _, a := range failedAttempts {
		codes = append(codes, orDash(a.ExitCode))
		recorded = recorded || a.ExitCode != ""
	}
	if !recorded {
		return retry + " failed all " + strconv.Itoa(nFailed) + " attempts" + colonThen(c.short), ev, retriesOne
	}
	return retry + " failed " + strconv.Itoa(nFailed) + " attempts in different ways: exit codes " + strings.Join(codes, ", "), ev, retriesDiffer
}

// nodeLine names a node with its type and template.
func (x *index) nodeLine(id string) string {
	n := x.nodes[id]
	s := x.name(id)
	if n.Type != "" {
		s += " · " + n.Type
	}
	if t := x.template(id); t != "" {
		s += " · template " + t
	}
	return s
}

// whenLine says when a node ran, as offsets into the run.
func (x *index) whenLine(id string) string {
	start, end, ok := x.ran(id)
	if !ok {
		return "never started"
	}
	n := x.nodes[id]
	if !finishedPhase(n.Phase) {
		return "started " + x.offset(start) + " into the run, running for " + shared.ShortDuration(end.Sub(start))
	}
	verb := "ended"
	if failedPhase(n.Phase) {
		verb = "failed"
	}
	return verb + " " + x.offset(end) + " into the run, after running " + shared.ShortDuration(end.Sub(start))
}

// logTarget is the pod whose log explains a failure: the last failing pod
// of the unit, which for a Retry is its final attempt. node is set and pod
// empty when that node runs a pod whose name the server did not give.
func (x *index) logTarget(u unit) (node, pod string) {
	for i := len(u.leaves) - 1; i >= 0; i-- {
		n := x.nodes[u.leaves[i]]
		if n.PodName != "" {
			return n.ID, n.PodName
		}
		if n.Type == "Pod" && node == "" {
			node = n.ID
		}
	}
	return node, ""
}

// withLog adds the log evidence to the first failure's card: the picked
// lines once they are read, or what the read is doing.
func (x *index) withLog(f Finding, node, pod string) Finding {
	l := x.in.Log
	if l == nil || node == "" {
		return f
	}
	if pod == "" {
		f.Evidence = append(f.Evidence, Evidence{Label: "log",
			Text: "not read: the server did not state its pod naming, so the pod cannot be named safely"})
		return f
	}
	who := x.name(node)
	if l.PodName != pod || l.State == LogLoading {
		f.Evidence = append(f.Evidence, Evidence{Label: "log",
			Text: "reading the last " + strconv.Itoa(max(l.Tail, 1)) + " lines of " + who + "'s log…"})
		return f
	}
	switch {
	case l.State == LogFailed:
		f.Evidence = append(f.Evidence, Evidence{Label: "log", Text: "could not be read (see below)"})
	case len(l.Lines) == 0:
		f.Evidence = append(f.Evidence, Evidence{Label: "log", Text: "empty (see below)"})
	default:
		picked := pickLog(l.Lines)
		intro := "picked from the last " + plural(len(l.Lines), "line") + " of " + who + "'s log (container " + orMain(l.Container) + ")"
		if !anyMatch(picked) {
			intro = "no error in the last " + plural(len(l.Lines), "line") + " of " + who + "'s log (container " + orMain(l.Container) + "); it ends"
		}
		f.Evidence = append(f.Evidence, Evidence{Label: "log", Text: intro, Log: picked})
	}
	return f
}

// logFinding reports a log that could not be read or came back empty. A
// missing log is evidence too: it usually means the pod is gone.
func (x *index) logFinding(node, pod string) (Finding, bool) {
	l := x.in.Log
	if l == nil || pod == "" || l.PodName != pod || l.State == LogLoading {
		return Finding{}, false
	}
	who := x.name(node)
	switch {
	case l.State == LogFailed && l.Gone:
		return Finding{Rule: RuleLog, Severity: Warning, NodeID: node,
			Headline: "The log of " + who + " is gone",
			Evidence: []Evidence{{Label: "pod", Text: pod}, {Label: "error", Text: l.Err}},
			Next:     "The pod was most likely deleted by pod garbage collection. Keep logs with archiveLogs and an artifact repository, or read them where the cluster ships its logs.",
		}, true
	case l.State == LogFailed:
		return Finding{Rule: RuleLog, Severity: Warning, NodeID: node,
			Headline: "The log of " + who + " could not be read",
			Evidence: []Evidence{{Label: "pod", Text: pod}, {Label: "error", Text: l.Err}},
			Next:     "Open the log (enter on the node in Nodes) to try again; if the pod was deleted, its log lives only where the cluster archives logs.",
		}, true
	case len(l.Lines) == 0:
		return Finding{Rule: RuleLog, Severity: Info, NodeID: node,
			Headline: "The log of " + who + " is empty",
			Evidence: []Evidence{{Label: "pod", Text: pod}, {Label: "container", Text: orMain(l.Container)}},
			Next:     "The container printed nothing, or its log was rotated away. The exit code and message above are the evidence left.",
		}, true
	}
	return Finding{}, false
}

// moreFailures counts the root failures past the cards, with a few names.
func moreFailures(x *index, rest []unit) Finding {
	var names []string
	for _, u := range rest {
		names = append(names, x.name(u.id))
	}
	return Finding{Rule: RuleMoreFailures, Severity: Error,
		Headline: plural(len(rest), "more node") + " failed on their own",
		Evidence: []Evidence{{Label: "nodes", Text: listNames(names, 8)}},
		Next:     "Filter the Nodes tab to failures (p) to go through them.",
	}
}

// validation is the finding for a workflow that failed before any node ran:
// the controller rejected its spec.
func validation(x *index) (Finding, bool) {
	s := x.in.Workflow.Summary
	if !failedPhase(s.Phase) || len(x.nodes) > 0 {
		return Finding{}, false
	}
	f := Finding{Rule: RuleValidation, Severity: Error,
		Headline: "The workflow failed before any node ran: the controller rejected it",
		Next:     "Fix the spec where the message points, check it with `argo lint`, then submit it again.",
	}
	if s.Message != "" {
		f.Evidence = append(f.Evidence, Evidence{Label: "message", Text: firstLine(s.Message)})
	} else {
		f.Evidence = append(f.Evidence, Evidence{Label: "message", Text: "(none: the server gave no reason)"})
	}
	if refs := unresolvedRefs(s.Message); len(refs) > 0 {
		f.Headline = "The workflow failed validation before any node ran: " + refs[0] + " did not resolve"
		f.Evidence = append(f.Evidence, Evidence{Label: "unresolved", Text: strings.Join(refs, ", ")})
		f.Next = "Supply the parameter at submit time (-p name=value) or give it a default in the template, check the spec with `argo lint`, then submit it again."
	}
	return f, true
}

var refPattern = regexp.MustCompile(`\{\{[^{}]{1,200}\}\}`)

// unresolvedRefs are the template references a validation message names.
func unresolvedRefs(msg string) []string {
	if !strings.Contains(strings.ToLower(msg), "resolve") {
		return nil
	}
	return refPattern.FindAllString(msg, 4)
}

// workflowFailed is the finding for a failed workflow in which no node
// failed on its own: the controller failed it from above.
func workflowFailed(x *index) Finding {
	s := x.in.Workflow.Summary
	f := Finding{Rule: RuleWorkflowFailed, Severity: Error,
		Headline: "The workflow " + strings.ToLower(s.Phase) + " without a failing node",
		Next:     "Read the workflow's message; the controller failed the run itself, for example on a deadline, a stop or a missing template.",
	}
	msg := "(none)"
	if s.Message != "" {
		msg = firstLine(s.Message)
		f.Headline = "The workflow " + strings.ToLower(s.Phase) + ": " + msg
	}
	f.Evidence = append(f.Evidence, Evidence{Label: "message", Text: msg})
	return f
}

// deadline is the finding for a workflow its own deadline stopped, when no
// failing node already carries that cause.
func (x *index) deadline(roots []unit) (Finding, bool) {
	s := x.in.Workflow.Summary
	if !deadlinePattern.MatchString(s.Message) {
		return Finding{}, false
	}
	for _, u := range roots {
		if causeOf(x.nodes[u.leaves[len(u.leaves)-1]]).kind == CauseDeadline {
			return Finding{}, false
		}
	}
	f := Finding{Rule: RuleDeadline, Severity: Error, Cause: CauseDeadline,
		Headline: "The workflow exceeded its deadline",
		Evidence: []Evidence{{Label: "message", Text: firstLine(s.Message)}},
		Next:     "If the run is legitimately slow, raise activeDeadlineSeconds; otherwise find what made it slow on the Timeline (T).",
	}
	if d := x.spec.ActiveDeadlineSeconds; d != nil {
		f.Evidence = append(f.Evidence, Evidence{Label: "deadline", Text: "activeDeadlineSeconds " + strconv.FormatInt(*d, 10)})
	}
	if s.StartedAt != nil && s.FinishedAt != nil {
		f.Evidence = append(f.Evidence, Evidence{Label: "ran", Text: shared.ShortDuration(s.FinishedAt.Sub(*s.StartedAt))})
	}
	return f, true
}

// dependents lists what did not run because of the failures: nodes the
// controller omitted, nodes skipped for a reason other than their own
// condition, nodes a finished run never started, and failures that only
// followed an earlier one.
func (x *index) dependents(roots []unit) (Finding, bool) {
	if len(roots) == 0 {
		return Finding{}, false
	}
	isRoot := map[string]bool{}
	for _, u := range roots {
		isRoot[u.id] = true
		for _, l := range u.leaves {
			isRoot[l] = true
		}
	}
	finished := finishedPhase(x.in.Workflow.Summary.Phase)
	var names []string
	var ev []Evidence
	for _, id := range x.order {
		if x.role[id] != "" || isRoot[id] {
			continue
		}
		n := x.nodes[id]
		var why string
		switch {
		case n.Phase == "Omitted":
			why = "omitted"
		case n.Phase == "Skipped" && !strings.HasPrefix(strings.TrimSpace(n.Message), "when "):
			why = "skipped"
		case finished && !finishedPhase(n.Phase) && n.StartedAt == nil && n.Type != "StepGroup":
			why = "not started"
		case x.failedLeaf(id) && x.waitedOnFailure(x.unitOf(id)):
			why = "failed after"
		default:
			continue
		}
		names = append(names, x.name(id))
		if len(ev) < 6 {
			text := x.name(id)
			if w := x.waitsFor(id); w != "" {
				text += ", waits for " + w
			}
			msg := firstLine(n.Message)
			msg = strings.TrimSpace(strings.TrimPrefix(msg, strings.ToLower(n.Phase)+":"))
			if msg != "" {
				text += " (" + msg + ")"
			}
			ev = append(ev, Evidence{Label: why, Text: text})
		}
	}
	if len(names) == 0 {
		return Finding{}, false
	}
	head := plural(len(names), "node") + " did not run because of the failure: " + listNames(names, 4)
	if len(names) == 1 {
		head = "1 node did not run because of the failure: " + names[0]
	}
	if len(names) > len(ev) {
		ev = append(ev, Evidence{Label: "and", Text: plural(len(names)-len(ev), "more")})
	}
	next := "They run once the failure is fixed: retrying the workflow (a, r) reruns the failed steps and everything after them."
	if len(names) == 1 {
		next = "It runs once the failure is fixed: retrying the workflow (a, r) reruns the failed steps and everything after them."
	}
	return Finding{Rule: RuleDependents, Severity: Info, Headline: head, Evidence: ev, Next: next}, true
}

// waitsFor names what a node waited for, from the tree's After data.
func (x *index) waitsFor(id string) string {
	b := x.branch[id]
	if b == nil || len(b.After) == 0 {
		return ""
	}
	var names []string
	for _, a := range b.After {
		names = append(names, x.name(a))
	}
	return listNames(names, 3)
}

// exitHandlers reports how each exit handler run went, and hooks that
// failed.
func (x *index) exitHandlers() []Finding {
	var out []Finding
	for i := range x.in.Tree {
		b := &x.in.Tree[i]
		if b.Role == "" {
			continue
		}
		n := x.nodes[b.ID]
		rule, what := RuleExitHandler, "The exit handler"
		if b.Role != "exit handler" {
			rule, what = RuleHook, "The "+x.name(b.ID)+" hook"
		}
		ev := []Evidence{{Label: "node", Text: x.nodeLine(b.ID)}}
		if w := x.whenLine(b.ID); w != "" {
			ev = append(ev, Evidence{Label: "when", Text: w})
		}
		switch {
		case failedPhase(n.Phase):
			leaf := b.ID
			for _, id := range x.order {
				if x.under(id, b.ID) && failedPhase(x.nodes[id].Phase) && !x.failedBelow[id] {
					leaf = id
					break
				}
			}
			c := causeOf(x.nodes[leaf])
			if leaf != b.ID {
				ev = append(ev, Evidence{Label: "failed", Text: x.nodeLine(leaf)})
			}
			if code := x.nodes[leaf].ExitCode; code != "" {
				ev = append(ev, Evidence{Label: "exit code", Text: code + ": " + c.meaning})
			}
			if m := x.nodes[leaf].Message; m != "" {
				ev = append(ev, Evidence{Label: "message", Text: firstLine(m)})
			}
			out = append(out, Finding{Rule: rule, Severity: Error, Cause: c.kind, NodeID: b.ID,
				Headline: what + " failed: " + c.short,
				Evidence: ev,
				Next:     "Whatever it was to notify or clean up did not happen. Read its log (enter on the node in Nodes); " + c.next,
			})
		case rule == RuleHook:
			continue
		case n.Phase == "Succeeded":
			out = append(out, Finding{Rule: rule, Severity: Info, NodeID: b.ID,
				Headline: what + " ran and succeeded",
				Evidence: ev,
				Next:     "Nothing to do: it ran after the workflow " + strings.ToLower(orWord(x.in.Workflow.Summary.Phase, "ended")) + ".",
			})
		default:
			out = append(out, Finding{Rule: rule, Severity: Info, NodeID: b.ID,
				Headline: what + " is " + strings.ToLower(orWord(n.Phase, "pending")),
				Evidence: ev,
				Next:     "Nothing to do yet: the workflow's outcome is final once it finishes.",
			})
		}
	}
	return out
}

// podPending reports pods of an unfinished run held up by a reason
// Kubernetes named, such as ImagePullBackOff or Unschedulable.
func (x *index) podPending() []Finding {
	if finishedPhase(x.in.Workflow.Summary.Phase) {
		return nil
	}
	var out []Finding
	for _, id := range x.order {
		n := x.nodes[id]
		if finishedPhase(n.Phase) || n.Phase == "Running" && n.Type != "Pod" {
			continue
		}
		c := causeOf(n)
		if c.kind != CauseImagePull && c.kind != CausePodPending {
			continue
		}
		ev := []Evidence{{Label: "node", Text: x.nodeLine(id)}, {Label: "message", Text: firstLine(n.Message)}}
		if n.PodName != "" {
			ev = append(ev, Evidence{Label: "pod", Text: n.PodName})
		}
		if start, _, ok := x.ran(id); ok {
			ev = append(ev, Evidence{Label: "waiting", Text: "since " + x.offset(start) + " into the run, " + shared.ShortDuration(x.in.Now.Sub(start)) + " ago"})
		}
		out = append(out, Finding{Rule: RulePodPending, Severity: Warning, Cause: c.kind, NodeID: id,
			Headline: x.name(id) + " cannot start: " + c.reason + ", " + c.meaning,
			Evidence: ev, Next: c.next,
		})
	}
	return out
}

// retriedThenSucceeded reports retried steps that got there in the end: the
// run passed, but the step is flaky.
func (x *index) retriedThenSucceeded() []Finding {
	var out []Finding
	for _, id := range x.order {
		n := x.nodes[id]
		if n.Type != "Retry" || n.Phase != "Succeeded" || !x.failedBelow[id] {
			continue
		}
		kids := x.branch[id].Children
		var attempts []string
		var failedLast core.Node
		for _, b := range kids {
			a := x.nodes[b.ID]
			s := x.name(b.ID) + " " + phaseWord(a.Phase)
			if a.ExitCode != "" && a.ExitCode != "0" {
				s += " exit " + a.ExitCode
			}
			attempts = append(attempts, s)
			if failedPhase(a.Phase) {
				failedLast = a
			}
		}
		ev := []Evidence{{Label: "attempts", Text: strings.Join(attempts, " · ")}}
		if failedLast.Message != "" {
			ev = append(ev, Evidence{Label: "last failure", Text: firstLine(failedLast.Message)})
		}
		out = append(out, Finding{Rule: RuleRetried, Severity: Warning, NodeID: id,
			Headline: x.name(id) + " succeeded on attempt " + strconv.Itoa(len(kids)) + " of " + strconv.Itoa(len(kids)) + " after failing " + plural(len(kids)-1, "time"),
			Evidence: ev,
			Next:     "The step is flaky: read the failed attempt's log (enter on it in Nodes) for why it failed before retrying helped.",
		})
	}
	return out
}

// tolerated reports nodes that failed in a workflow that succeeded anyway,
// which continueOn or a depends expression allows.
func (x *index) tolerated() (Finding, bool) {
	if x.in.Workflow.Summary.Phase != "Succeeded" {
		return Finding{}, false
	}
	var names []string
	for _, id := range x.order {
		n := x.nodes[id]
		if x.role[id] != "" || !failedPhase(n.Phase) || x.failedBelow[id] {
			continue
		}
		if p := x.parent[id]; p != "" && x.nodes[p].Type == "Retry" {
			continue
		}
		names = append(names, x.name(id))
	}
	if len(names) == 0 {
		return Finding{}, false
	}
	return Finding{Rule: RuleTolerated, Severity: Warning,
		Headline: plural(len(names), "node") + " failed, and the workflow succeeded past them",
		Evidence: []Evidence{{Label: "nodes", Text: listNames(names, 6)}},
		Next:     "Their templates allow failure (continueOn or a depends expression). Check that ignoring it was intended.",
	}, true
}

// nodesUnavailable is the finding for a workflow whose node map the server
// did not send.
func nodesUnavailable(x *index) Finding {
	reason := x.in.Workflow.NodesUnavailableReason
	if reason == "" {
		reason = "the server did not send node status for this workflow"
	}
	return Finding{Rule: RuleNodesUnavailable, Severity: Warning,
		Headline: "Node status is unavailable, so the nodes cannot be explained",
		Evidence: []Evidence{{Label: "reason", Text: reason}, {Label: "phase", Text: orWord(x.in.Workflow.Summary.Phase, "unknown")}},
		Next:     "The node status is probably offloaded; check that the server can read the offload database.",
	}
}

// unknownPhase is the finding when no rule applies, so the section never
// renders empty.
func unknownPhase(x *index) Finding {
	s := x.in.Workflow.Summary
	f := Finding{Rule: RuleUnknownPhase, Severity: Info,
		Headline: "The workflow is " + orWord(s.Phase, "in no phase yet") + "; no rule explains more",
		Next:     "The Summary and Nodes sections show everything the server sent.",
	}
	if s.Message != "" {
		f.Evidence = append(f.Evidence, Evidence{Label: "message", Text: firstLine(s.Message)})
	}
	return f
}

// colonThen is ": s", or nothing when s is empty, for a headline that names
// a cause only when there is one.
func colonThen(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

func quoteOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orMain(s string) string {
	if s == "" {
		return "main"
	}
	return s
}

func orWord(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// phaseWord is a phase in lower case for a sentence.
func phaseWord(p string) string {
	if p == "" {
		return "pending"
	}
	return strings.ToLower(p)
}

// firstLine is the first line of a message, trimmed.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

// joinSentence joins two clauses with a separator fit for the first.
func joinSentence(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.HasSuffix(a, ".") || strings.HasSuffix(a, " "):
		return strings.TrimRight(a, " ") + " " + b
	default:
		return a + "; " + b
	}
}

// listNames joins names, keeping at most n and counting the rest.
func listNames(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + " and " + strconv.Itoa(len(names)-n) + " more"
}

// plural is "1 node" or "3 nodes".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
