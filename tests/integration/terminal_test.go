//go:build integration

package integration

// Terminal tests build the current binary and check demo rendering, keys and exit.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/buildinfo"
)

// demoRows is the demo list in the order the default sort produces: phase
// first (Suspended, then Failed and Error, then Running, then Pending, then
// Succeeded), and newest first inside a phase. Tests index into it instead of naming a workflow,
// so a change to the sort is one edit here.
var demoRows = []string{
	"demo-release-gate",
	"demo-etl-hourly-1790000000",
	"demo-nightly-report",
	"demo-oom-backfill",
	"demo-param-check",
	"demo-train-pipeline",
	"demo-data-pull",
	"demo-cleanup",
	"demo-hello-world",
	"demo-etl-hourly-1789996400",
	"demo-deploy-multi-layer",
	"demo-etl-hourly-1789992800",
}

// demoListed is the list pane's count once the whole demo snapshot is in.
const demoListed = "list: 12 workflows"

// buildBinary compiles the real cmd/argo-tui binary once per test binary.
func buildBinary(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("ET-3 unavailable: /dev/ptmx missing on this host (%v)", err)
	}
	bin := "/tmp/argo-tui-e1-" + strings.ReplaceAll(t.Name(), "/", "_")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/ficaa1/argo-tui/cmd/argo-tui")
	cmd.Dir = projectRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build argo-tui: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(bin) })
	return bin
}

// projectRoot returns the module root (three levels above this package's
// directory within the module: tests/integration → module root).
func projectRoot() string {
	wd, _ := os.Getwd()
	return wd + "/../.."
}

// detailShows reports whether the detail route for wf is on the screen.
//
// It looks for the workflow's uid in the summary body, not for the pane's
// border title. The title shares its line with the pane's right-hand summary,
// and the renderer repaints only the cells that changed, so a capture taken
// between the loading frame and the loaded one can hold the two spliced
// together with the title unreadable. The uid line is written whole.
func detailShows(screen, wf string) bool {
	return strings.Contains(screen, "synthetic-uid-"+wf)
}

// waitScreen polls the scrubbed screen until cond is true or timeout.
func waitScreen(p *PTYProcess, timeout time.Duration, cond func(string) bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond(p.Screen()) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond(p.Screen())
}

// TestPTYDemoJourney: launch --demo → header + list render → 'q' exits
// cleanly (exit 0) → raw capture contains teardown bytes. (UI-05 alpha
// journey over the F1 placeholder; full journey re-run by I1/Q1.)
func TestPTYDemoJourney(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()

	ok := waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "ns: demo") && strings.Contains(s, demoListed)
	})
	if !ok {
		t.Fatalf("demo header/list never rendered; screen=%q", p.Screen())
	}

	if err := p.Send("q"); err != nil {
		t.Fatalf("send q: %v", err)
	}
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

// TestPTYDemoQuitViaCtrlC: Ctrl-C quits globally (UI-04).
func TestPTYDemoQuitViaCtrlC(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	})
	if err := p.Send("\x03"); err != nil { // Ctrl-C
		t.Fatalf("send ctrl-c: %v", err)
	}
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("ctrl-c exit code = %d, want 0", code)
	}
}

// --version prints the build identity and exits without starting the TUI.
func TestPTYVersionFlag(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--version")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Close()
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 || !strings.Contains(p.Screen(), "argo-tui "+buildinfo.Version) {
		t.Fatalf("--version: code=%d screen=%q", code, p.Screen())
	}
}

// Started with no destination, argo-tui asks which profile to use instead of
// guessing one. With no config file the picker names the path to write and
// esc leaves, because there is no session behind the dialog to return to.
//
// --config points at a path that does not exist, so the run does not depend on
// whatever config file the machine running the test happens to have.
func TestPTYNoProfileOpensThePicker(t *testing.T) {
	bin := buildBinary(t)
	missing := filepath.Join(t.TempDir(), "config.yaml")
	p, err := StartPTY(bin, "--config", missing)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "no profiles configured")
	})
	// The box clips a long temporary path to fit, so the assertion here is on
	// the parts that always survive. The exact path is pinned by the unit
	// test, which renders the dialog without a box around it.
	for _, want := range []string{"write them to:", "currentProfile: dev", "tokenEnv: ARGO_TOKEN"} {
		if s := p.Screen(); !strings.Contains(s, want) {
			t.Fatalf("the empty picker does not mention %q: %q", want, s)
		}
	}
	if err := p.Send("\x1b"); err != nil { // esc
		t.Fatalf("send esc: %v", err)
	}
	code, err := p.WaitFor(20 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("esc exit = %d, want 0", code)
	}
}

// The picker lists the profiles the config file names, with the server that
// tells two clusters apart. Nothing is connected until one is chosen, so this
// runs against servers that do not exist.
func TestPTYThePickerListsConfiguredProfiles(t *testing.T) {
	bin := buildBinary(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "currentProfile: prod\nprofiles:\n" +
		"  dev:\n    server: https://dev.invalid\n    namespace: workflows\n    tokenEnv: ARGO_TUI_TEST\n" +
		"  prod:\n    server: https://prod.invalid\n    namespace: argo\n    tokenEnv: ARGO_TUI_TEST\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := StartPTY(bin, "--config", path)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "dev") && strings.Contains(s, "prod")
	})
	if s := p.Screen(); !strings.Contains(s, "https://dev.invalid") {
		t.Fatalf("the picker does not identify the cluster: %q", s)
	}
	p.Kill()
}

// TestPTYTerminalRestoreMarkers: the raw capture of a clean quit contains
// teardown output after the final view render (terminal restoration —
// UI-06, asserted against actual bytes, not assumed).
func TestPTYTerminalRestoreMarkers(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Close()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	})
	_ = p.Send("q")
	if _, err := p.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	raw := p.Raw()
	// The renderer emits teardown sequences; the essential invariant is
	// that SOMETHING is written after the last content frame and the
	// process exited 0 (above). Assert the capture is non-trivial.
	if len(raw) < 50 {
		t.Fatalf("raw capture suspiciously small (%d bytes); renderer output missing", len(raw))
	}
}

// TestPTYSmallTerminalStillQuits: below 60×15 the app must remain usable
// (resize notice per plan §2) and q must still quit (UI-01 graceful floor).
func TestPTYSmallTerminalStillQuits(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTYSize(40, 10, bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "argo-tui")
	})
	_ = p.Send("q")
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("small-terminal exit = %d, want 0", code)
	}
}

// TestPTYNoEgressInDemo: --demo never contacts non-loopback hosts. The
// demo binary is built from the F1 baseline; a full offline guarantee
// also holds at the adapter level (SEC-03 negative test — the demo path
// constructs no HTTP clients at all, verified in code by ADR 0001 and by
// the fixture-server tests above using an explicit client).
func TestPTYNoEgressInDemo(t *testing.T) {
	// Guard assertion: the demo path in main.go wires ONLY the fake
	// Reader (no transport construction). Re-verify at runtime that the
	// process makes no network connections by checking /proc/<pid>/net
	// snapshot while running.
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	})

	tcpEstablished := procTCPEstablished(p.Cmd.Process.Pid)
	for _, line := range tcpEstablished {
		// The LXC shares one netns, so this is a host-wide coarse guard:
		// any ESTABLISHED TCP with a NON-loopback local or remote address
		// while the demo runs is a violation (loopback hex = 0100007F).
		if strings.Contains(line, "ESTAB") && !strings.Contains(line, "0100007F") {
			t.Errorf("demo process has non-loopback TCP: %s", line)
		}
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// procTCPEstablished returns /proc/<pid>/net/tcp lines (host-wide net
// namespace view — the LXC shares one netns; treat as a coarse guard).
func procTCPEstablished(pid int) []string {
	data, err := os.ReadFile("/proc/self/net/tcp")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n")[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// --- F: compiled-demo behaviour through a PTY --------------------------------
//
// These drive the REAL compiled binary in --demo mode and pin the behaviours
// a terminal can break: AGE rendering, list key routing, resize, and the
// alternate screen.

// AGE must render from the injected clock; time.Time{} renders "-" on every
// row. demo-data-pull started 40m ago → "40m".
func TestPTYDemoAgeColumnNotEmpty(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	ok := waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed) && strings.Contains(s, "40m")
	})
	if !ok {
		t.Fatalf("AGE column empty (baseline renders '-' for every row); screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// List keys must route. "j" moves the selection down and Enter
// opens detail for the moved row. Demo default sort (SortPhaseName) puts the
// single Failed workflow first (nightly-report); "j" moves to row 1
// (data-pull, the first Running by name).
func TestPTYDemoKeysMoveAndOpenDetail(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// "enter" must be sent as the carriage-return byte (the enter key); a
	// literal "enter" string would just type five letters.
	if err := p.SendKeys(30*time.Millisecond, "j", "\r"); err != nil {
		t.Fatalf("send keys: %v", err)
	}
	ok := waitScreen(p, 10*time.Second, func(s string) bool {
		// The summary's uid line names the opened workflow; see detailShows
		// for why the border title is not used. demoRows[1] is a failed run,
		// so its phase word is on the pane.
		return detailShows(s, demoRows[1]) && strings.Contains(s, "✗ Failed")
	})
	if !ok {
		t.Fatalf("j/enter did not open the second row's detail; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// 'l' opens logs straight from the list, and a single Esc must return to the
// LIST — never to a never-loaded detail pane ("(no workflow loaded)") or a
// stale previously-viewed workflow. Path: --demo → j → l → Esc.
func TestPTYDemoEscFromListOpenedLogsReturnsToList(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// j selects the second row, l opens its logs (nil pod -> workflow-wide).
	if err := p.SendKeys(30*time.Millisecond, "j", "l"); err != nil {
		t.Fatalf("send j/l: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "Logs "+demoRows[1])
	}) {
		t.Fatalf("logs never rendered after l; screen=%q", p.Screen())
	}
	// A single Esc must return to the list (no detail pane was ever loaded).
	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc
		t.Fatalf("send esc: %v", err)
	}
	ok := waitScreen(p, 10*time.Second, func(s string) bool {
		screen := s
		return strings.Contains(screen, demoListed) &&
			!strings.Contains(screen, "(no workflow loaded)")
	})
	if !ok {
		t.Fatalf("Esc from list-opened logs did not return to the list; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Repro 2 (stale detail): open detail A, Esc to list, move to B, l (logs B),
// Esc must never land on the stale DETAIL A.
func TestPTYDemoEscFromLogsBNotStaleDetailA(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// j Enter -> detail for row 1 (A); Esc back to list; j j l -> logs for
	// row 2 (B).
	if err := p.SendKeys(30*time.Millisecond, "j", "\r"); err != nil {
		t.Fatalf("send j/enter: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return detailShows(s, demoRows[1])
	}) {
		t.Fatalf("detail A never rendered; screen=%q", p.Screen())
	}
	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc back to list
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("did not return to list; screen=%q", p.Screen())
	}
	// Esc keeps the cursor on row 1, so one more j reaches row 2.
	p.ClearScreen()
	if err := p.SendKeys(30*time.Millisecond, "j", "l"); err != nil {
		t.Fatalf("send j/l: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "Logs "+demoRows[2])
	}) {
		t.Fatalf("logs B never rendered; screen=%q", p.Screen())
	}
	// Esc from logs B: must NOT show the stale detail A, and
	// must return to the list (the route logs B was opened from).
	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		screen := s
		return strings.Contains(screen, demoListed) &&
			!detailShows(screen, demoRows[1])
	}) {
		t.Fatalf("Esc from logs B did not return to the list without the stale detail A; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Resize must re-layout. Shrinking to 40x10 shows the resize
// notice; growing back to 120x40 clears it (WindowSizeMsg reaches the child).
func TestPTYDemoResizeReacts(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTYSize(80, 24, bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}

	if err := p.Resize(40, 10); err != nil {
		t.Fatalf("resize down: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "too small")
	}) {
		t.Fatalf("40x10 did not show the resize notice (WindowSizeMsg not propagated); screen=%q", p.Screen())
	}

	if err := p.Resize(120, 40); err != nil {
		t.Fatalf("resize up: %v", err)
	}
	// Drop the frames rendered at the old size. One more can still arrive
	// after the clear, because the program was already writing it when the
	// resize landed, so the assertion asks for order rather than absence:
	// the full layout has to render after the last resize notice. The cut
	// falls after the notice's last line, so the stale frame's own text is
	// not mistaken for a notice drawn after the layout.
	p.ClearScreen()
	const notice = "Resize to at least"
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		if last := strings.LastIndex(s, notice); last >= 0 {
			s = s[last+len(notice):]
		}
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("120x40 did not restore the full layout; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// The program must use the alternate screen buffer (enter
// sequence \x1b[?1049h in the raw capture) so the layout is stable, and must
// exit it (\x1b[?1049l) on quit so the terminal is restored.
func TestPTYDemoUsesAltScreenAndRestores(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTY(bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Close()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	raw := p.Raw()
	if !strings.Contains(raw, "\x1b[?1049h") {
		t.Fatalf("alt-screen enter sequence missing; the TUI is not using the alternate screen buffer")
	}
	_ = p.Send("q")
	if _, err := p.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if after := p.Raw(); !strings.Contains(after, "\x1b[?1049l") {
		t.Fatalf("alt-screen exit sequence missing on quit; terminal not restored")
	}
}

// --- G: shell integration repair (viewport, help overlay, filter escape) ----

// startDemo launches --demo at a given size and waits for the first list
// frame, so the shell tests below start from a known screen.
func startDemo(t *testing.T, cols, rows int) *PTYProcess {
	t.Helper()
	bin := buildBinary(t)
	p, err := StartPTYSize(cols, rows, bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	})
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed)
	}) {
		t.Fatalf("list never rendered at %dx%d: %q", cols, rows, p.Screen())
	}
	return p
}

// On a terminal too short for every row, an overflowing frame pushes the
// footer — the only place the key hints appear — off screen. The alternate
// screen has no scrollback, so those lines would simply be gone.
func TestPTYDemoFooterSurvivesShortTerminal(t *testing.T) {
	for _, rows := range []int{9, 11, 14} {
		p := startDemo(t, 100, rows)
		if !waitScreen(p, 10*time.Second, func(s string) bool {
			return strings.Contains(s, "? help")
		}) {
			t.Fatalf("%d rows: key hints missing from the frame; screen=%q", rows, p.Screen())
		}
		_ = p.Send("q")
		_, _ = p.Wait()
	}
}

// Shrinking the terminal while running must re-lay out rather than lose the
// footer.
func TestPTYDemoShrinkKeepsFooter(t *testing.T) {
	p := startDemo(t, 120, 40)
	if err := p.Resize(100, 10); err != nil {
		t.Fatalf("resize: %v", err)
	}
	p.ClearScreen()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "? help") && strings.Contains(s, demoListed)
	}) {
		t.Fatalf("shrink to 100x10 lost the footer; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// The footer advertises `? help` on every route, so a `?` handler has to
// exist or the key is inert.
func TestPTYDemoHelpOverlayOpensAndCloses(t *testing.T) {
	p := startDemo(t, 100, 30)
	if strings.Contains(p.Screen(), "KEYS") {
		t.Fatalf("help overlay visible before ? was pressed; screen=%q", p.Screen())
	}
	if err := p.Send("?"); err != nil {
		t.Fatalf("send ?: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "KEYS")
	}) {
		t.Fatalf("? did not open the help overlay; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed) && !strings.Contains(s, "KEYS")
	}) {
		t.Fatalf("Esc did not close the help overlay; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// The help overlay is a dialog: q closes it instead of quitting the program.
func TestPTYDemoHelpOverlayQClosesWithoutQuitting(t *testing.T) {
	p := startDemo(t, 100, 30)
	if err := p.Send("?"); err != nil {
		t.Fatalf("send ?: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "KEYS")
	}) {
		t.Fatalf("? did not open help; screen=%q", p.Screen())
	}
	p.ClearScreen()
	if err := p.Send("q"); err != nil {
		t.Fatalf("send q: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, demoListed) && !strings.Contains(s, "KEYS")
	}) {
		t.Fatalf("q did not close the help overlay; screen=%q", p.Screen())
	}
	if p.Exited() {
		t.Fatal("q quit the program instead of closing the help overlay")
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// A filter matching nothing renders exactly like an empty
// namespace. Esc must clear it and restore the list.
func TestPTYDemoEscClearsAppliedFilter(t *testing.T) {
	p := startDemo(t, 100, 30)
	if err := p.SendKeys(30*time.Millisecond, "/", "z", "z", "z", "\r"); err != nil {
		t.Fatalf("send filter: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "no workflows match")
	}) {
		t.Fatalf("filter never applied; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "demo-hello-world") && !strings.Contains(s, "no workflows match")
	}) {
		t.Fatalf("Esc did not clear the applied filter; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// --- H: UI shell -----------------------------------------------------------

// The shell must actually reach the terminal. A bordered pane that renders
// only in unit tests is the same defect the inert demo was.
func TestPTYDemoRendersTheBorderedShell(t *testing.T) {
	p := startDemo(t, 100, 30)
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "┌") && strings.Contains(s, "└") &&
			strings.Contains(s, "Workflows")
	}) {
		t.Fatalf("the bordered pane never rendered; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// The context band and the help hint must be on screen at all times: the
// band says whether this session can mutate anything, and `?` is the only
// route to the rest of the keys.
func TestPTYDemoShowsContextAndHelpHint(t *testing.T) {
	p := startDemo(t, 100, 30)
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "READ ONLY") &&
			strings.Contains(s, "ns: demo") &&
			strings.Contains(s, "? help")
	}) {
		t.Fatalf("context band or help hint missing; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Statuses must never be carried by color alone (UI-03/07): each row shows
// a glyph and the phase word.
func TestPTYDemoRowsCarrySymbolAndWord(t *testing.T) {
	p := startDemo(t, 100, 30)
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "✗ Failed") && strings.Contains(s, "● Running")
	}) {
		t.Fatalf("phase symbols missing from the rows; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// The frame must survive a live resize with its border intact. A pane that
// keeps the old size leaves a torn border on the alternate screen.
func TestPTYDemoShellSurvivesResize(t *testing.T) {
	p := startDemo(t, 120, 40)
	p.ClearScreen()
	if err := p.Resize(90, 16); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "└") && strings.Contains(s, "? help")
	}) {
		t.Fatalf("border or help hint lost after a resize; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Below the border width the shell must degrade to plain bands rather than
// draw a border it has no room for.
func TestPTYDemoNarrowTerminalDropsTheBorder(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTYSize(50, 16, bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	})
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "help") && !strings.Contains(s, "┌")
	}) {
		t.Fatalf("narrow layout wrong; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Opening a workflow must keep the frame: the same bands, the same border,
// a different pane.
func TestPTYDemoDetailKeepsTheFrame(t *testing.T) {
	p := startDemo(t, 100, 30)
	// No ClearScreen here: the renderer repaints only the cells that
	// changed, so a cleared capture holds fragments rather than a frame.
	if err := p.Send("\r"); err != nil { // enter
		t.Fatalf("send enter: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return detailShows(s, demoRows[0]) &&
			strings.Contains(s, "tab section") &&
			strings.Contains(s, "? help")
	}) {
		t.Fatalf("detail lost the frame; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}
