// Package shell composes the three bands every route shares: a context
// header, a bordered content pane, and a key-hint footer. When asked, and on
// a large enough terminal, Mićko the mascot perches on the pane's top border
// (perch.go).
//
// The point of the package is stable geometry. The alternate screen has no
// scrollback, so a frame that changes height between renders does not scroll
// — it leaves stale rows behind or loses its footer. Render therefore always
// produces exactly Height lines of exactly Width cells: a short body is
// padded and a long one is clipped, but the border and the footer never move.
// Children ask BodyHeight/BodyWidth for their budget and size themselves to
// it, so clipping is a safety net rather than the normal path.
//
// The package holds no state, performs no I/O, and never reaches the network.
// It is a pure function of a Frame and a Theme.
package shell

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// minBorderWidth is the narrowest terminal that still gets a border. Below
// it the four columns the border costs are worth more as content, so the
// layout degrades to plain bands (the route bodies keep their own narrow
// notices below 60 columns).
const minBorderWidth = 60

// borderRows is the vertical cost of the pane border (top and bottom).
const borderRows = 2

// bandRows is the vertical cost of the header and footer bands.
const bandRows = 2

// Frame is one fully-specified screen. Every field is data; Render adds no
// information of its own.
type Frame struct {
	// Width and Height are the terminal size. Zero means "not known yet"
	// (before the first WindowSizeMsg) and disables that axis of clipping.
	Width, Height int

	// Context band.
	App       string // program name and version
	Server    string // server identity, or the demo source
	Namespace string
	Mode      string // READ ONLY / ACTIONS ENABLED — the safety state
	// ActionsEnabled draws the mode badge in the theme's armed style. The
	// Mode words carry the meaning; the badge colour only repeats it.
	ActionsEnabled bool

	// Mascot asks for Mićko, the mascot, to perch on the pane (perch.go).
	// He only perches on a terminal with room for him.
	Mascot bool
	// MascotFloor sits him on the floor of the pane instead, in its bottom
	// corner (floor.go).
	MascotFloor bool
	// MascotPose is the pose he is in, one of shared.MickoPerchBeats or
	// shared.MickoFloorBeats. The zero Art is his resting pose there.
	MascotPose shared.Art

	// Pane.
	Title      string   // left of the top border, e.g. "Workflows"
	TitleRight string   // right of the top border, e.g. "5 collected"
	Body       []string // already sanitized content lines

	// Footer band.
	Route string // the active route name
	Hints string // route key hints, "key description" pairs, most useful first
	// Notice replaces Hints for one frame with the result of the last
	// command, such as a copy. It is drawn as a message, not as keys.
	Notice string
	Help   string // the global help hint, e.g. "? help"
	Status string // right-aligned state, e.g. "watch • 1/5"
}

// bordered reports whether this frame is wide enough for the pane border.
// A width of 0 means "not known yet", which cannot be bordered safely: the
// border needs a column count to close on.
func (f Frame) bordered() bool { return f.Width >= minBorderWidth }

// titleRows is the vertical cost of the pane heading: two for a border, one
// for the plain title line the narrow and unsized layouts use instead.
//
// The narrow layout still needs the title. Dropping it left a pane with no
// statement of what it held and no count — the geometry saving was one row
// and the cost was the pane's identity.
func (f Frame) titleRows() int {
	if f.bordered() {
		return borderRows
	}
	if f.Title == "" && f.TitleRight == "" {
		return 0
	}
	return 1
}

// BodyWidth is the number of cells a body line may occupy. Children should
// lay out to exactly this width.
func (f Frame) BodyWidth() int {
	if f.Width <= 0 {
		return 0
	}
	if !f.bordered() {
		return f.Width
	}
	// "│ " on the left and " │" on the right.
	return f.Width - 4
}

// BodyHeight is the number of body lines the pane shows. Children should
// window their content to exactly this many lines.
//
// It returns 0 when the height is unknown, which children read as "render
// everything" — the same convention SetSize already uses.
func (f Frame) BodyHeight() int {
	if f.Height <= 0 {
		return 0
	}
	chrome := bandRows + f.titleRows() + f.perchRows() + f.floorRows()
	if n := f.Height - chrome; n > 0 {
		return n
	}
	// Never zero: a frame this short is unusable anyway, and one content
	// line is more honest than an empty pane.
	return 1
}

// Render composes the frame into terminal text.
func (f Frame) Render(t shared.Theme) string {
	var out []string
	out = append(out, f.headerBand(t))

	body := f.bodyLines()
	b := t.Borders()
	if f.bordered() {
		top, perchX := f.topBorder(t)
		out = append(out, f.perchLines(t, perchX)...)
		out = append(out, top)
		inner := f.BodyWidth()
		left, right := t.Border.Render(b.Left), t.Border.Render(b.Right)
		for _, l := range append(body, f.floorLines(t)...) {
			out = append(out, left+" "+fit(l, inner)+" "+right)
		}
		out = append(out, f.bottomLine(t, b))
	} else {
		if f.titleRows() > 0 {
			out = append(out, band(
				[]span{{oneLine(f.Title), t.Title}},
				[]span{{oneLine(f.TitleRight), t.Dim}},
				f.Width, lipgloss.NewStyle()))
		}
		for _, l := range body {
			out = append(out, fit(l, f.Width))
		}
	}

	out = append(out, f.footerBand(t))
	return strings.Join(out, "\n")
}

// bodyLines clips or pads the body to the pane height. Padding is what keeps
// the bottom border and the footer still while rows arrive and leave.
func (f Frame) bodyLines() []string {
	src := make([]string, 0, len(f.Body))
	for _, l := range f.Body {
		// A body line carrying an embedded newline would silently add a row
		// and push the footer off the screen.
		src = append(src, strings.Split(l, "\n")...)
	}
	n := f.BodyHeight()
	if n <= 0 {
		return src
	}
	if len(src) > n {
		return src[:n]
	}
	for len(src) < n {
		src = append(src, "")
	}
	return src
}

// minTitleRight is the smallest right-hand status worth keeping on the border.
// Below it the text says nothing useful, so the title wins the space instead.
const minTitleRight = 16

// topBorder draws the title row: ┌ Title ──── TitleRight ─┐
//
// The title and the count ride the border so the pane says what it holds
// without spending a content row on a heading. The corner and line
// characters come from the theme, so a skin can round the corners.
//
// When the frame is perched, Mićko's feet and beak are drawn into the line
// between the title and the count. perchX is the column his drawing starts
// at, or -1 when he has no room on this border.
func (f Frame) topBorder(t shared.Theme) (line string, perchX int) {
	b := t.Borders()
	inner := f.Width - 2 // between the corners
	title := oneLine(f.Title)
	right := oneLine(f.TitleRight)

	left := t.Border.Render(b.Top)
	if title != "" {
		left += " " + t.Title.Render(title) + " "
	}
	tail := t.Border.Render(b.Top)
	if right != "" {
		tail = " " + t.Dim.Render(right) + " " + t.Border.Render(b.Top)
	}

	used := ansi.StringWidth(left) + ansi.StringWidth(tail)
	if used > inner {
		// No room for both. The right side carries the live state — during an
		// outage it is the stale warning — so shorten it to fit instead of
		// dropping it, and only drop it when even a short form has no room.
		room := inner - ansi.StringWidth(left) - 3 // " " + " ─"
		if room >= minTitleRight {
			tail = " " + t.Dim.Render(ansi.Truncate(right, room, "…")) + " " + t.Border.Render(b.Top)
		} else {
			tail = t.Border.Render(b.Top)
			left = ansi.Truncate(left, inner-1, "…")
		}
		used = ansi.StringWidth(left) + ansi.StringWidth(tail)
	}
	fill := inner - used
	if fill < 0 {
		fill = 0
	}
	fillStart := 1 + ansi.StringWidth(left) // after the corner and the title
	perchX = f.perchColumn(fillStart, fill)
	return t.Border.Render(b.TopLeft) + left + f.perchFill(t, b.Top, fillStart, fill, perchX) +
		tail + t.Border.Render(b.TopRight), perchX
}

func (f Frame) bottomBorder(b lipgloss.Border) string {
	inner := f.Width - 2
	if inner < 0 {
		inner = 0
	}
	return b.BottomLeft + strings.Repeat(b.Bottom, inner) + b.BottomRight
}

// headerBand names the program, where it is pointed, and the safety mode.
// The mode is right-aligned so a reader always finds it in the same place,
// and it is drawn as a badge: the one word on the screen that says whether a
// key can change the cluster.
//
// When the theme paints the band, the band gets a one-cell margin at each
// end so its text does not touch the screen edge. The plain theme adds
// nothing, so a plain header is the same text as the segments joined.
func (f Frame) headerBand(t shared.Theme) string {
	painted := shared.IsBlock(t.Band)
	var left []span
	if painted {
		left = append(left, span{" ", t.Band})
	}
	left = append(left, span{oneLine(f.App), t.AppName})
	if f.Server != "" {
		left = append(left, span{"   ", t.Band}, span{"server: ", t.Muted}, span{oneLine(f.Server), t.Text})
	}
	if f.Namespace != "" {
		left = append(left, span{"   ", t.Band}, span{"ns: ", t.Muted}, span{oneLine(f.Namespace), t.Text})
	}
	var right []span
	if mode := oneLine(f.Mode); mode != "" {
		badge := t.BadgeReadOnly
		if f.ActionsEnabled {
			badge = t.BadgeActions
		}
		if shared.IsBlock(badge) {
			mode = " " + mode + " "
		}
		right = append(right, span{mode, badge})
	}
	if painted {
		right = append(right, span{" ", t.Band})
	}
	return band(left, right, f.Width, t.Band)
}

// footerBand carries the route name and its key hints on the left, and the
// global help hint plus the route state on the right.
//
// Help sits on the protected right side deliberately. `?` is the one key
// that leads to every other key, so a narrow terminal must drop route hints
// before it drops the way to find them again.
//
// A Notice replaces the hints for the frame it is set on: it answers the key
// the reader just pressed, so it is text, not a list of keys.
func (f Frame) footerBand(t shared.Theme) string {
	var rest []span
	if n := oneLine(f.Notice); n != "" {
		rest = []span{{n, t.Accent}}
	} else {
		rest = hintSpans(oneLine(f.Hints), t)
	}
	var left []span
	if f.Route != "" {
		left = append(left, span{oneLine(f.Route), t.Accent})
		if len(rest) > 0 {
			left = append(left, span{"   ", t.Footer})
		}
	}
	left = append(left, rest...)
	right := hintSpans(oneLine(f.Help), t)
	if s := oneLine(f.Status); s != "" {
		if len(right) > 0 {
			right = append(right, span{"   ", t.Footer})
		}
		right = append(right, span{s, t.Footer})
	}
	return band(left, right, f.Width, lipgloss.NewStyle())
}

// hintSpans splits a hint line into keys and descriptions. Hints are written
// as "key description" pairs separated by two or more spaces, so the first
// word of each pair is the key. The separators are kept exactly, which is
// what makes the plain rendering identical to the hint text.
func hintSpans(hints string, t shared.Theme) []span {
	if hints == "" {
		return nil
	}
	var out []span
	for hints != "" {
		gap := strings.Index(hints, "  ")
		pair := hints
		if gap >= 0 {
			pair = hints[:gap]
		}
		if key, desc, ok := strings.Cut(pair, " "); ok {
			out = append(out, span{key, t.HintKey}, span{" " + desc, t.HintDesc})
		} else if pair != "" {
			out = append(out, span{pair, t.HintKey})
		}
		if gap < 0 {
			break
		}
		rest := hints[gap:]
		trimmed := strings.TrimLeft(rest, " ")
		out = append(out, span{rest[:len(rest)-len(trimmed)], lipgloss.NewStyle()})
		hints = trimmed
	}
	return out
}

// span is one run of text in a band and the style it is drawn in.
type span struct {
	text  string
	style lipgloss.Style
}

func spansWidth(ss []span) int {
	w := 0
	for _, s := range ss {
		w += ansi.StringWidth(s.text)
	}
	return w
}

// truncSpans cuts a run of spans to width cells, ending in "…" when anything
// was cut. The cut falls inside whichever span crosses the edge, and that
// span keeps its style.
func truncSpans(ss []span, width int) []span {
	if spansWidth(ss) <= width {
		return ss
	}
	if width <= 0 {
		return nil
	}
	room := width - 1 // the ellipsis
	out := make([]span, 0, len(ss))
	for _, s := range ss {
		w := ansi.StringWidth(s.text)
		if w <= room {
			out = append(out, s)
			room -= w
			continue
		}
		out = append(out, span{ansi.Truncate(s.text, room, "") + "…", s.style})
		break
	}
	return out
}

// renderSpans draws spans on a band. A span without a background of its own
// takes the band's, so a painted band has no holes behind its text.
func renderSpans(ss []span, base lipgloss.Style) string {
	var b strings.Builder
	bg := base.GetBackground()
	painted := shared.HasBackground(base)
	for _, s := range ss {
		if s.text == "" {
			continue
		}
		st := s.style
		if painted && !shared.HasBackground(st) {
			st = st.Background(bg)
		}
		b.WriteString(st.Render(s.text))
	}
	return b.String()
}

// band lays out a left run and a right-aligned run in exactly width cells.
// The right run is the one that survives a squeeze: it holds the safety
// mode and the position, which are shorter and less recoverable than hints.
// base paints the gap between them, and the background of any span that has
// none of its own.
func band(left, right []span, width int, base lipgloss.Style) string {
	if width <= 0 {
		if len(right) == 0 {
			return renderSpans(left, base)
		}
		return renderSpans(left, base) + "   " + renderSpans(right, base)
	}
	rw := spansWidth(right)
	if rw >= width {
		right = truncSpans(right, width)
		pad := width - spansWidth(right)
		return renderSpans(right, base) + gapOf(pad, base)
	}
	avail := width - rw
	if rw > 0 {
		avail-- // at least one space between the runs
	}
	left = truncSpans(left, avail)
	gap := width - spansWidth(left) - rw
	return renderSpans(left, base) + gapOf(gap, base) + renderSpans(right, base)
}

// gapOf is n cells of band background.
func gapOf(n int, base lipgloss.Style) string {
	if n <= 0 {
		return ""
	}
	return base.Render(strings.Repeat(" ", n))
}

// fit clips or pads s to exactly width cells. Width 0 means "unknown", which
// leaves the line alone.
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	s = ansi.Truncate(s, width, "…")
	if pad := width - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// oneLine sanitizes untrusted text and flattens it to a single line. Server
// text reaches the frame through the title and the status cells; a newline
// there would add a row and push the footer off the alternate screen.
func oneLine(s string) string {
	s = shared.Sanitize(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "\t", " ")
}
