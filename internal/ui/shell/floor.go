package shell

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// On the floor, Mićko sits in the pane's bottom-right corner with his
// mirror against its right edge. His rows are the pane's last three, kept
// clear of content the way the perch keeps the rows above the border, so
// the list never runs under him and the border and the footer stay in the
// same rows on every route. His feet, his tail and the mirror's stand are
// drawn into the bottom border.

// floored reports whether Mićko is asked for on the floor and this frame has
// room for him.
func (f Frame) floored() bool {
	return f.Mascot && f.MascotFloor && f.bordered() && PerchFits(f.Width, f.Height)
}

// floorRows is the vertical cost of Mićko on the floor: the pane rows above
// the bottom border. His last row is drawn on the border itself.
func (f Frame) floorRows() int {
	if !f.floored() {
		return 0
	}
	return len(shared.MickoFloor.Lines) - 1
}

// floorPose is his drawing in his current pose on the floor.
func (f Frame) floorPose() shared.Art {
	if len(f.MascotPose.Lines) == 0 {
		return shared.MickoFloor
	}
	return f.MascotPose
}

// floorColumn is where his drawing starts within the pane's content, so
// that the mirror ends at the content's right edge.
func (f Frame) floorColumn() int {
	return f.BodyWidth() - f.floorPose().Width()
}

// floorLines are the pane's last content rows, with Mićko at their right.
func (f Frame) floorLines(t shared.Theme) []string {
	n := f.floorRows()
	art, x := f.floorPose(), f.floorColumn()
	out := make([]string, 0, n)
	for row := 0; row < n; row++ {
		out = append(out, strings.Repeat(" ", x)+art.RenderRow(t, row, t.Text))
	}
	return out
}

// bottomLine is the pane's bottom border, with Mićko's feet on it when he
// is on the floor. The content starts two cells in, after the left border
// and its padding cell.
func (f Frame) bottomLine(t shared.Theme, b lipgloss.Border) string {
	if !f.floored() {
		return t.Border.Render(f.bottomBorder(b))
	}
	fill := f.Width - 2
	return t.Border.Render(b.BottomLeft) +
		artOnLine(t, f.floorPose(), b.Bottom, 1, fill, 2+f.floorColumn()) +
		t.Border.Render(b.BottomRight)
}
