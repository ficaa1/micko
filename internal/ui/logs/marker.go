package logs

import "strconv"

// marker.go — stream-context annotations mixed into the retained buffer
// (stream open, dropped lines, timestamp reopen).
// Markers are UI authoring, not data: they never count toward the byte
// cap, and their wording is pinned by tests.

// markerKind enumerates annotation rows stored alongside log lines.
type markerKind int

const (
	// markOpen starts one stream: pod/container context header.
	markOpen markerKind = iota
	// markDropped marks oversize records dropped by the record cap.
	markDropped
	// markTimestampsOn and markTimestampsOff mark where the stream was
	// reopened with server timestamps switched on or off.
	markTimestampsOn
	markTimestampsOff
)

// entry is one retained item: a log line (kind == markOpen? no — kind ==
// markOpen is a marker; a log line is entry{kind: logEntry}) or a marker.
type entry struct {
	kind    markerKind
	line    logLine // valid only when kind == logEntry
	ctxPod  string  // pod the marker was emitted at (pre-sanitized)
	ctxCont string  // container the marker was emitted at (pre-sanitized)
}

// logEntryKind is the kind value stored for a real log line (distinct from
// every marker kind; markers start at markOpen).
const logEntryKind markerKind = -1

// isMarker reports whether the entry is an annotation row.
func (e entry) isMarker() bool { return e.kind != logEntryKind }

// logLine is one retained, already-sanitized log line.
type logLine struct {
	Content   string
	PodName   string
	Container string
	Truncated bool // per-line allowance cut it; view shows the marker
}

// markerText returns the visible body of a marker row. Wordings are pinned
// by tests: "new stream; overlap/gap possible", and oversize handling is
// stated, never silent. The open marker carries
// the stream context plus a live line count (supplied by the buffer at
// render time).
func markerText(kind markerKind, podName, container string) string {
	switch kind {
	case markOpen:
		return markerLine(podName, container)
	case markDropped:
		return "── oversized lines dropped (per-record cap) ──"
	case markTimestampsOn:
		return "── server timestamps on: stream reopened from the start ──"
	case markTimestampsOff:
		return "── server timestamps off: stream reopened from the start ──"
	default:
		return ""
	}
}

// markerLine is the per-stream context header. An empty pod renders as
// "(all pods)" — workflow-wide logs stay visibly distinct.
func markerLine(podName, container string) string {
	who := podName
	if who == "" {
		who = "(all pods)"
	}
	return "── " + who + ":" + container + " ──"
}

// markerLineWithCount is the rendered open-marker with the running line
// count: "── pod:container ── N line(s) recorded". Singular/plural and
// the zero case stay grammatical (the count is transcript-wide: every
// log line retained so far, not only this stream's).
func markerLineWithCount(podName, container string, lines int) string {
	unit := "line recorded"
	if lines != 1 {
		unit = "lines recorded"
	}
	return markerLine(podName, container) + " " + strconv.Itoa(lines) + " " + unit
}
