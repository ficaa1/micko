package shell

import (
	"strings"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// Mićko perches on the pane's top border: his body sits in the rows above
// it, and his feet and bowed head are drawn into the border line, so he is
// resting his beak on the edge of the screen he is looking into.
//
// He is opt-in (Frame.Mascot). His rows are chrome like the header, so they
// belong to the frame, not to a route. Whether he perches depends only on
// the setting and the terminal size, never on the route, which keeps the
// border and the footer in the same rows on every route: opening a workflow
// never moves the pane. Where on the border he sits may change with
// the title and the count, because he never covers either.

// perchMinWidth and perchMinHeight are the smallest terminal Mićko perches
// on. His rows cost the body three lines, which a small terminal cannot
// spare. The help overlay is written to fit the body a perched 80x40
// terminal leaves.
const (
	perchMinWidth  = 80
	perchMinHeight = 40
)

// perchRight is how many columns Mićko's drawing keeps clear of the right
// corner when the border has room: enough for a typical count, so he stays
// put while the count changes.
const perchRight = 30

// perched reports whether Mićko is asked for on the perch and this frame
// has room for him.
func (f Frame) perched() bool {
	return f.Mascot && !f.MascotFloor && f.bordered() && PerchFits(f.Width, f.Height)
}

// PerchFits reports whether a terminal of width by height has room for
// Mićko, on his perch or on the floor.
func PerchFits(width, height int) bool {
	return width >= perchMinWidth && height >= perchMinHeight
}

// perchRows is the vertical cost of Mićko: the rows above the border. His
// last row is drawn on the border itself.
func (f Frame) perchRows() int {
	if !f.perched() {
		return 0
	}
	return len(shared.MickoPerch.Lines) - 1
}

// perchColumn places Mićko on a border whose line runs fill cells from
// column fillStart. He sits perchRight from the right corner, moves left as
// far as the count needs, and keeps a cell of line between himself and the
// title or the count. It returns -1 when he is not perched or does not fit.
func (f Frame) perchColumn(fillStart, fill int) int {
	if !f.perched() {
		return -1
	}
	art := shared.MickoPerch
	lo, hi := footSpan(art)
	x := f.Width - perchRight - art.Width()
	if last := fillStart + fill - 2 - hi; x > last {
		x = last
	}
	if x+lo < fillStart+1 || x < 0 {
		return -1
	}
	return x
}

// footSpan is the first and last column of the drawing's border row that
// belong to Mićko.
func footSpan(a shared.Art) (lo, hi int) {
	row := len(a.Lines) - 1
	lo, hi = -1, -1
	for col := range []rune(a.Lines[row]) {
		if _, _, ok := a.Cell(shared.Theme{}, row, col); ok {
			if lo < 0 {
				lo = col
			}
			hi = col
		}
	}
	return lo, hi
}

// pose is the drawing of Mićko in his current pose. Every pose shares the
// resting pose's width and feet, so where he sits is worked out from the
// resting pose alone and does not change as he moves.
func (f Frame) pose() shared.Art {
	if len(f.MascotPose.Lines) == 0 {
		return shared.MickoPerch
	}
	return f.MascotPose
}

// perchFill draws fill cells of border line from column fillStart, with
// Mićko's border row laid over it when he sits at column x.
func (f Frame) perchFill(t shared.Theme, line string, fillStart, fill, x int) string {
	if x < 0 {
		return t.Border.Render(strings.Repeat(line, fill))
	}
	return artOnLine(t, f.pose(), line, fillStart, fill, x)
}

// artOnLine draws fill cells of border line from column fillStart, with the
// last row of art laid over it from column x: only the cells that belong to
// Mićko replace line.
func artOnLine(t shared.Theme, art shared.Art, line string, fillStart, fill, x int) string {
	row := len(art.Lines) - 1
	var b strings.Builder
	run := 0 // border cells waiting to be drawn as one run
	for i := 0; i < fill; i++ {
		ch, st, ok := art.Cell(t, row, fillStart+i-x)
		if !ok {
			run++
			continue
		}
		if run > 0 {
			b.WriteString(t.Border.Render(strings.Repeat(line, run)))
			run = 0
		}
		b.WriteString(st.Render(ch))
	}
	if run > 0 {
		b.WriteString(t.Border.Render(strings.Repeat(line, run)))
	}
	return b.String()
}

// perchLines are the rows above the border, with Mićko drawn from column x.
// They are blank when he has no room on this border, so the rows stay and
// the pane does not move.
func (f Frame) perchLines(t shared.Theme, x int) []string {
	n := f.perchRows()
	out := make([]string, 0, n)
	for row := 0; row < n; row++ {
		l := ""
		if x >= 0 {
			l = strings.Repeat(" ", x) + f.pose().RenderRow(t, row, t.Text)
		}
		out = append(out, fit(l, f.Width))
	}
	return out
}
