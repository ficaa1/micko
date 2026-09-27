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
	// mascot signs the overlay with the mascot's wordmark when there is room.
	mascot bool
}

// SetTheme replaces the style set the overlay is drawn in. The zero theme
// draws plain text.
func (h *HelpOverlay) SetTheme(t Theme) { h.theme = t }

// SetMascot turns the mascot's wordmark at the foot of the overlay on or off.
func (h *HelpOverlay) SetMascot(on bool) { h.mascot = on }

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
// overlay without clipping it. That pane is the one left under Mićko's perch.
func helpLines() []string {
	return []string{
		"KEYS                                       ? or esc to close",
		"Global    q quit  ctrl+c quit  ? help  esc back / close  : command",
		"          P profile  f raw full screen  y copy  o open in Argo UI",
		"Move      j / k  pgup / pgdn  gg / home top  G / end bottom",
		"",
		"List      enter open  l logs  s sort  p phase  r refresh  n namespace",
		"          0 all ns  T / X / E open on the timeline / explanation / events",
		"          space mark  a actions (marked, or selected)  w wide columns",
		"          esc clear marks, then filter",
		"          / filter: word  a|b  !word  /regex/  ~fuzzy  (spaces: AND)",
		"          phase=failed  age<2h  dur>10m  tmpl=x  cron=x  label:k=v  label:!k",
		"Command   : then a command; tab completes, enter runs, ctrl+p / n history",
		"          wf  cron  tmpl  cwftmpl  aw  ns [name]  all  ctx [name]  help  q",
		"Kinds     :cron :tmpl :cwftmpl :aw take the list keys; enter lists runs",
		"          i info panel  v hide / reveal values  f manifest",
		"",
		"Detail    tab / shift+tab section  1-9 section  T / X / E jump  r refresh",
		"          a actions  v hide / reveal values  y copy  f raw",
		"Nodes     enter / l logs  space fold  left / right fold, parent, unfold",
		"          i info  / find  n / N next / previous  h skipped  s sort  p phase",
		"Timeline  ◆ critical path  ░ waited  │ now  l logs  i info  space fold",
		"Explain   why it ended, as findings  y copy report  l failing log",
		"Events    live Kubernetes events  s warnings first  / filter",
		"Logs      t follow  space pause  c container  G newest  esc back",
		"          / search  n / N next / previous match  & only matching lines",
		"          w wrap long lines  L source labels  | pipe to a program",
		"          ctrl+t server timestamps (reopens the stream, keeps the lines)",
		"Actions   a opens the pane; only verbs that apply are offered",
		"          u resume  z suspend  r retry  b resubmit  s stop",
		"          t terminate (type the name)  d delete (then only D deletes)",
		"          y confirms; enter and esc cancel; one result goes to the footer",
		"          marked: one request per workflow, in order; results stay until esc",
		"          actions need --allow-actions",
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

// helpFitWidth and helpFitHeight are the body of an 80x40 terminal inside
// the shell's border, under the three rows Mićko perches in: the smallest
// common terminal the overlay is written to fit whole. Help that clips there
// hides keys from the reader who most needs them.
const (
	helpFitWidth  = 76
	helpFitHeight = 33
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
	styled := make([]string, len(lines))
	for i, l := range lines {
		if width > 0 {
			l = ansi.Truncate(l, width, "…")
		}
		styled[i] = h.styleLine(i, l)
	}
	return strings.Join(append(styled, h.wordmark(width, height-len(lines))...), "\n")
}

// wordmark signs the overlay with Mićko and the project name, below the
// keys and a blank line, when he is turned on and the pane has room left
// for all of it. It is
// never clipped: a part of the drawing is worth less than none. An unsized
// overlay gets none, so unsized renders stay the key text alone.
func (h *HelpOverlay) wordmark(width, room int) []string {
	art := MickoWordmark
	if !h.mascot || width < art.Width() || room < len(art.Lines)+1 {
		return nil
	}
	out := []string{""}
	for row := range art.Lines {
		out = append(out, art.RenderRow(h.theme, row, h.theme.Title))
	}
	return out
}
