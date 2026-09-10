// Package shell composes the three bands every route shares: a context
// header, a bordered content pane, and a key-hint footer.
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

	"github.com/charmbracelet/x/ansi"

	"argo-tui/internal/ui/shared"
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

	// Pane.
	Title      string   // left of the top border, e.g. "Workflows"
	TitleRight string   // right of the top border, e.g. "5 collected"
	Body       []string // already sanitized content lines

	// Footer band.
	Route  string // the active route name
	Hints  string // route key hints, most useful first
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
	chrome := bandRows + f.titleRows()
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
	out = append(out, t.Header.Render(f.headerBand()))

	body := f.bodyLines()
	if f.bordered() {
		out = append(out, f.topBorder(t))
		inner := f.BodyWidth()
		for _, l := range body {
			out = append(out, t.Border.Render("│")+" "+fit(l, inner)+" "+t.Border.Render("│"))
		}
		out = append(out, t.Border.Render(f.bottomBorder()))
	} else {
		if f.titleRows() > 0 {
			out = append(out, t.Title.Render(band(oneLine(f.Title), oneLine(f.TitleRight), f.Width)))
		}
		for _, l := range body {
			out = append(out, fit(l, f.Width))
		}
	}

	out = append(out, t.Footer.Render(f.footerBand()))
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
// without spending a content row on a heading.
func (f Frame) topBorder(t shared.Theme) string {
	inner := f.Width - 2 // between the corners
	title := oneLine(f.Title)
	right := oneLine(f.TitleRight)

	left := "─"
	if title != "" {
		left = "─ " + t.Title.Render(title) + " "
	}
	tail := "─"
	if right != "" {
		tail = " " + t.Dim.Render(right) + " ─"
	}

	used := ansi.StringWidth(left) + ansi.StringWidth(tail)
	if used > inner {
		// No room for both. The right side carries the live state — during an
		// outage it is the stale warning — so shorten it to fit instead of
		// dropping it, and only drop it when even a short form has no room.
		room := inner - ansi.StringWidth(left) - 3 // " " + " ─"
		if room >= minTitleRight {
			tail = " " + t.Dim.Render(ansi.Truncate(right, room, "…")) + " ─"
		} else {
			tail = "─"
			left = ansi.Truncate(left, inner-1, "…")
		}
		used = ansi.StringWidth(left) + ansi.StringWidth(tail)
	}
	fill := inner - used
	if fill < 0 {
		fill = 0
	}
	return t.Border.Render("┌") + left + t.Border.Render(strings.Repeat("─", fill)) +
		tail + t.Border.Render("┐")
}

func (f Frame) bottomBorder() string {
	inner := f.Width - 2
	if inner < 0 {
		inner = 0
	}
	return "└" + strings.Repeat("─", inner) + "┘"
}

// headerBand names the program, where it is pointed, and the safety mode.
// The mode is right-aligned so a reader always finds it in the same place.
func (f Frame) headerBand() string {
	left := oneLine(f.App)
	var ctx []string
	if f.Server != "" {
		ctx = append(ctx, "server: "+oneLine(f.Server))
	}
	if f.Namespace != "" {
		ctx = append(ctx, "ns: "+oneLine(f.Namespace))
	}
	if len(ctx) > 0 {
		left += "   " + strings.Join(ctx, "   ")
	}
	return band(left, oneLine(f.Mode), f.Width)
}

// footerBand carries the route name and its key hints on the left, and the
// global help hint plus the route state on the right.
//
// Help sits on the protected right side deliberately. `?` is the one key
// that leads to every other key, so a narrow terminal must drop route hints
// before it drops the way to find them again.
func (f Frame) footerBand() string {
	left := oneLine(f.Hints)
	if f.Route != "" {
		left = oneLine(f.Route) + "   " + left
	}
	right := oneLine(f.Help)
	if s := oneLine(f.Status); s != "" {
		if right != "" {
			right += "   "
		}
		right += s
	}
	return band(left, right, f.Width)
}

// band lays out a left cell and a right-aligned cell in exactly width cells.
// The right cell is the one that survives a squeeze: it holds the safety
// mode and the position, which are shorter and less recoverable than hints.
func band(left, right string, width int) string {
	if width <= 0 {
		if right == "" {
			return left
		}
		return left + "   " + right
	}
	rw := ansi.StringWidth(right)
	if rw >= width {
		return fit(right, width)
	}
	avail := width - rw
	if right != "" {
		avail-- // at least one space between the cells
	}
	left = ansi.Truncate(left, avail, "…")
	gap := width - ansi.StringWidth(left) - rw
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
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
