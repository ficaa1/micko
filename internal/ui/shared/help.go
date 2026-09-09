package shared

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// HelpOverlay is the `?` key overlay. Every route footer advertises `? help`,
// so the overlay is the single place that documents the key contract frozen
// in keys.go. It owns no data and performs no I/O: the root toggles it and
// renders it inside the frame, so the surrounding header/footer geometry
// never moves when help opens.
//
// While open the overlay is a dialog (KeyCtxDialog): it consumes navigation
// keys so the view behind it cannot move, `q` closes it instead of quitting,
// and only Ctrl-C still quits globally.
type HelpOverlay struct {
	open bool
}

// IsOpen reports whether the overlay is currently shown.
func (h *HelpOverlay) IsOpen() bool { return h.open }

// Toggle opens a closed overlay and closes an open one (the `?` key).
func (h *HelpOverlay) Toggle() { h.open = !h.open }

// Close hides the overlay (Esc, or `q` within the dialog).
func (h *HelpOverlay) Close() { h.open = false }

// ClampLines enforces a height budget on an assembled frame. It keeps the top
// of the frame and always keeps the final line, because the final line is the
// footer carrying the key hints — the part a naive truncation destroys first.
//
// The alternate screen has no scrollback, so anything past the last row is
// not merely off-screen, it is gone.
func ClampLines(lines []string, height int) []string {
	if height < 1 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	if height == 1 {
		return lines[len(lines)-1:]
	}
	out := make([]string, 0, height)
	out = append(out, lines[:height-1]...)
	return append(out, lines[len(lines)-1])
}

// helpLines is the overlay body, most global first. Keys are written exactly
// as tea.KeyPressMsg.String() reports them, so the text matches what a reader
// must actually press.
func helpLines() []string {
	return []string{
		"KEYS                                       ? or esc to close",
		"",
		"Global    q quit        ctrl+c quit        ? help",
		"          esc back / close",
		"",
		"List      j / k move    up / down move     enter open",
		"          l logs        / search           s sort",
		"          r refresh     esc clear filter",
		"",
		"Detail    tab next section                 esc back",
		"          v reveal redacted resource values",
		"          a actions (requires --allow-actions)",
		"",
		"Logs      space pause   f follow           c container",
		"          / search      pgup / pgdn scroll esc back",
	}
}

// View renders the overlay clipped to the given box. A closed overlay renders
// nothing, so callers can concatenate it unconditionally.
//
// width/height of 0 mean "unknown" and disable that axis of clipping, which
// keeps unsized golden renders byte-stable. When the box is too short the
// body is cut from the bottom; the title line always survives, so a very
// short terminal still shows what the overlay is and how to leave it.
func (h *HelpOverlay) View(width, height int) string {
	if !h.open {
		return ""
	}
	lines := helpLines()
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	if width > 0 {
		clipped := make([]string, len(lines))
		for i, l := range lines {
			clipped[i] = ansi.Truncate(l, width, "…")
		}
		lines = clipped
	}
	return strings.Join(lines, "\n")
}
