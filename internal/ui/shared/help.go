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
		"Global    q quit  ctrl+c quit  ? help  esc back / close  P profile",
		"          f raw full screen (easy to copy)  y copy  o open in Argo UI",
		"Move      j / k  pgup / pgdn  gg / home top  G / end bottom",
		"",
		"List      enter open  l logs  s sort  p phase  r refresh  n namespace",
		"          space mark  a actions (marked, or selected)  w wide columns",
		"          esc clear marks, then filter",
		"          / filter: word  a|b  !word  /regex/  ~fuzzy  (spaces: AND)",
		"            phase=failed  phase!=x  age<2h  dur>10m  tmpl=x  cron=x",
		"            label:k=v  label:k  label:!k",
		"",
		"Detail    tab / shift+tab section  r refresh  a actions",
		"          h show / hide skipped nodes  s sort nodes  p phase filter",
		"          l logs for the selected node  v reveal redacted values",
		"",
		"Logs      t follow (tail)  space pause  c container  G newest",
		"          / search  n / N next / previous match  esc back",
		"          | pipe the retained lines to another program",
		"",
		"Actions   a opens the pane; only verbs that apply are offered",
		"          u resume  z suspend  r retry  b resubmit  s stop",
		"          t terminate (type the name)  d delete (then only D deletes)",
		"          y confirms; enter and esc cancel; one result goes to the footer",
		"          marked: one request per workflow, in order; results stay until esc",
		"          actions need --allow-actions",
	}
}

// helpFitWidth and helpFitHeight are the body of an 80x40 terminal inside
// the shell's border: the smallest common terminal the overlay is written
// to fit whole. Help that clips there hides keys from the reader who most
// needs them.
const (
	helpFitWidth  = 76
	helpFitHeight = 36
)

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
