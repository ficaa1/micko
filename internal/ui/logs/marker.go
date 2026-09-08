package logs

// marker.go — stream-context annotations mixed into the retained buffer
// (plan §8 D1 slice: disconnect/reconnect/gap marker; LOG-11: an explicit
// "new stream; overlap/gap possible" marker, legitimate duplicates kept).
// Markers are UI authoring, not data: they never count toward the byte
// cap, and their wording is pinned by the acceptance matrix.

// markerKind enumerates annotation rows stored alongside log lines.
type markerKind int

const (
	// markOpen starts one stream: pod/container context header.
	markOpen markerKind = iota
	// markReconnect marks a new stream after a disconnect (LOG-11).
	markReconnect
	// markEnd marks a normal end-of-stream.
	markEnd
	// markDropped marks oversize records dropped by the record cap (LOG-04).
	markDropped
	// markCanceled marks a user-canceled / disconnected stream.
	markCanceled
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

// markerText returns the visible body of a marker row. Wordings pinned by
// acceptance LOG-11 ("new stream; overlap/gap possible") and LOG-04
// (oversize handling is stated, never silent). The open marker carries
// the stream context plus a live line count (supplied by the buffer at
// render time).
func markerText(kind markerKind, podName, container string) string {
	switch kind {
	case markOpen:
		return markerLine(podName, container)
	case markReconnect:
		return "── reconnect: new stream; overlap/gap possible ──"
	case markEnd:
		return "── stream ended ──"
	case markDropped:
		return "── oversized lines dropped (per-record cap) ──"
	case markCanceled:
		return "── stream canceled ──"
	default:
		return ""
	}
}

// markerLine is the per-stream context header. An empty pod renders as
// "(all pods)" — workflow-wide logs stay visibly distinct (plan §2).
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
	return markerLine(podName, container) + " " + itoaView(lines) + " " + unit
}

// itoaView is the view's minimal integer formatter (no fmt at render).
func itoaView(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// itoa64 formats int64 counters (same minimal formatter, wider input).
func itoa64(n int64) string { return itoaView(int(n)) }
