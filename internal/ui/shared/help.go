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
	open  bool
	theme Theme
}

// SetTheme replaces the style set the overlay is drawn in. The zero theme
// draws plain text.
func (h *HelpOverlay) SetTheme(t Theme) { h.theme = t }

// IsOpen reports whether the overlay is currently shown.
func (h *HelpOverlay) IsOpen() bool { return h.open }

// Toggle opens a closed overlay and closes an open one (the `?` key).
func (h *HelpOverlay) Toggle() { h.open = !h.open }

// Open shows the overlay whatever its state (the palette's help command).
func (h *HelpOverlay) Open() { h.open = true }

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
// must actually press. The whole body fits the pane of a 40-row terminal and
// every line the pane of an 80-column one, so a common terminal shows the
// overlay without clipping it.
func helpLines() []string {
	return []string{
		"KEYS                                       ? or esc to close",
		"",
		"Global    q quit   ctrl+c quit   ? help   esc back / close   : command",
		"          P switch profile   f raw full screen   y copy   o open in Argo UI",
		"Move      j / k or arrows   pgup / pgdn   gg / home top   G / end bottom",
		"",
		"List      enter open   l logs   / search (live)   s sort   p phase",
		"          n namespace   0 all namespaces   r refresh   esc clear filter",
		"          T / X / E open on the timeline / explanation / events",
		"",
		"Command   : then a command; tab completes, enter runs, ctrl+p / n history",
		"          wf  cron  tmpl  cwftmpl  aw  ns [name]  all  ctx [name]  help  q",
		"Kinds     :cron :tmpl :cwftmpl :aw take the list keys; enter lists runs",
		"          i info panel   v hide/reveal values   f manifest",
		"",
		"Detail    tab / shift+tab section   1-9 section by position",
		"          T timeline   X explain   E events   r refresh",
		"          a actions (requires --allow-actions)   v hide/reveal values",
		"",
		"Nodes     enter / l logs   space fold   left fold or parent",
		"          right unfold   i info panel   / find, n / N next / previous",
		"          h skipped   s sort (pipeline/started/name/phase)   p phase",
		"",
		"Timeline  ◆ critical path   ░ waited   │ now   l logs   i info   space fold",
		"Explain   why it ended, as findings   y copy report   l failing log",
		"Events    live Kubernetes events   s warnings first   / filter",
		"",
		"Logs      t follow   space pause   c container   G newest line",
		"          / search   n / N next / previous match   | pipe to a program",
		"",
		"Actions   a opens the pane: u resume  r retry  b resubmit  s stop",
		"          y confirms; enter and esc cancel; the footer reports",
	}
}

// styleLine draws one overlay line: the title and each section's name stand
// out, the keys and their meanings stay plain. Styling runs after clipping,
// so a cut line never loses the reset at its end.
func (h *HelpOverlay) styleLine(i int, l string) string {
	if l == "" || strings.HasPrefix(l, " ") {
		return l
	}
	label, rest, _ := strings.Cut(l, " ")
	if i == 0 {
		return h.theme.Title.Render(label) + h.theme.Muted.Render(" "+rest)
	}
	if rest == "" {
		return h.theme.Accent.Render(label)
	}
	return h.theme.Accent.Render(label) + " " + rest
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
	styled := make([]string, len(lines))
	for i, l := range lines {
		if width > 0 {
			l = ansi.Truncate(l, width, "…")
		}
		styled[i] = h.styleLine(i, l)
	}
	return strings.Join(styled, "\n")
}
