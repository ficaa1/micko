package shared

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// HelpOverlay documents the keys available on each route.
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
// of the frame and always keeps the final line.
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

// helpLines is the overlay body, most global first.
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
		"          esc clear marks, then filter  in /: tab completes  ↑ / ↓ history",
		"          / filter: word  a|b  !word  /regex/  ~fuzzy  (spaces: AND)",
		"          phase=failed  age<2h  dur>10m  tmpl=x  cron=x  label:k=v  label:!k",
		"Command   : command  tab completes  enter runs  ctrl+p / n history",
		"          wf  cron  tmpl  cwftmpl  aw  ns [name]  all  ctx [name]  help  q",
		"Kinds     :cron :tmpl :cwftmpl enter lists runs; :aw enter opens run",
		"          i info panel  v hide / reveal values  f manifest",
		"",
		"Detail    tab / shift+tab section  1-9 section  T / X / E jump  r refresh",
		"          a actions  v hide / reveal values  y copy  f raw",
		"Nodes     enter / l logs  space fold  left / right fold, parent, unfold",
		"          i info  / find  n / N next / previous  h skipped  s sort  p phase",
		"Timeline  l logs  i info  space fold",
		"Explain   y copy report  l failing log",
		"Events    s warnings first  / filter",
		"Logs      t follow  space pause  c container  G newest  esc back",
		"          / search  n / N next / previous match  & only matching lines",
		"          w wrap long lines  L source labels  | pipe to a program",
		"          ctrl+t server timestamps (reopens the stream, keeps the lines)",
		"Actions   a open actions pane",
		"          u resume  z suspend  r retry  b resubmit  s stop",
		"          t terminate (type the name)  d delete (then only D deletes)",
		"          y confirms; enter and esc cancel",
		"          marked: one request per workflow, in order; results stay until esc",
	}
}

// styleLine draws one overlay line: the title and each section's name stand
// out, the keys and their meanings stay plain.
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

const (
	helpFitWidth  = 76
	helpFitHeight = 32
)

// View renders the overlay clipped to the given box. A closed overlay renders
// nothing, so callers can concatenate it unconditionally.
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

// wordmark signs the overlay with Mićko and the project name.
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
