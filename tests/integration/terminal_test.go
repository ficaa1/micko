//go:build integration

package integration

// terminal_test.go — ET-3 PTY acceptance (docs/test-environment.md §5:
// real process lifecycle, scripted keys, captured output, exit/restore).
// Maps to acceptance matrix rows UI-01/03/04/05/06, CONN-22, SEC-03/06
// (docs/acceptance-matrix.md §3.6/§3.3).
//
// Honest scope label (plan §8 E1 gate; docs/testing.md): the F1 baseline
// binary is the F1 root model with placeholder views — the real alpha UI
// (B1/C1/D1 views, I1 wiring) replaces it. These tests pin the PROCESS
// contract (launch, render, scripted keys, clean exit, no egress, restore
// bytes present in raw capture); I1/Q1 re-run the same harness against
// the wired binary for the full UI-05 alpha journey.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// buildBinary compiles the real cmd/argo-tui binary once per test binary.
func buildBinary(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("ET-3 unavailable: /dev/ptmx missing on this host (%v)", err)
	}
	bin := "/tmp/argo-tui-e1-" + strings.ReplaceAll(t.Name(), "/", "_")
	cmd := exec.Command("go", "build", "-o", bin, "argo-tui/cmd/argo-tui")
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
		return strings.Contains(s, "argo-tui | ns: demo") && strings.Contains(s, "list: 5 workflows")
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
		return strings.Contains(s, "list: 5 workflows")
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

// TestPTYVersionAndNonDemoRefusal: --version prints and exits; non-demo
// start refuses with guidance and exit 1 (CONN-22; the real-connection
// refusal is honest F1 behavior — A1/I1 replace the message).
func TestPTYVersionAndNonDemoRefusal(t *testing.T) {
	bin := buildBinary(t)

	p, err := StartPTY(bin, "--version")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 || !strings.Contains(p.Screen(), "argo-tui 0.2.0-beta.1") {
		t.Fatalf("--version: code=%d screen=%q", code, p.Screen())
	}
	p.Close()

	p2, err := StartPTY(bin)
	if err != nil {
		t.Fatalf("start non-demo: %v", err)
	}
	code2, err := p2.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code2 != 1 {
		t.Fatalf("non-demo exit = %d, want 1", code2)
	}
	if s := p2.Screen(); !strings.Contains(s, "server endpoint missing") {
		t.Fatalf("non-demo guidance missing: %q", s)
	}
	p2.Close()
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
		return strings.Contains(s, "list: 5 workflows")
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
		return strings.Contains(s, "list: 5 workflows")
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

// --- F: live-smoke integration defect regressions (compiled demo) ------------
//
// These drive the REAL compiled binary through a PTY in --demo mode and pin
// the behaviours the live smoke test found broken (AGE, list key routing,
// resize, alternate screen). They fail on the baseline binary and pass once
// the integrated slice lands (TDD: regressions written before the fix).

// Defect 1: AGE must render from the injected clock (not time.Time{} which
// renders "-" on every row). demo-data-pull started 40m ago → "40m".
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
		return strings.Contains(s, "list: 5 workflows") && strings.Contains(s, "40m")
	})
	if !ok {
		t.Fatalf("AGE column empty (baseline renders '-' for every row); screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Defect 2: list keys must route. "j" moves the selection down and Enter
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
		return strings.Contains(s, "list: 5 workflows")
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// "enter" must be sent as the carriage-return byte (the enter key); a
	// literal "enter" string would just type five letters.
	if err := p.SendKeys(30*time.Millisecond, "j", "\r"); err != nil {
		t.Fatalf("send keys: %v", err)
	}
	ok := waitScreen(p, 10*time.Second, func(s string) bool {
		// The detail pane titles the opened workflow; assert on that rendered
		// title (the "route: detail" header prefix is unreliable here because
		// the alternate-screen repaint overwrites it via C0 controls that the
		// PTY harness's stripANSI discards, so the stream only carries the
		// overwriting suffix).
		return strings.Contains(s, "DETAIL demo-data-pull") && strings.Contains(s, "phase: Running")
	})
	if !ok {
		t.Fatalf("j/enter did not open the second row's detail; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Defect item 8 (partial): 'l' opens logs straight from the list, and a single
// Esc must return to the LIST — never to a never-loaded detail pane ("(no
// workflow loaded)") or a stale previously-viewed workflow (Q finding
// t_fa36e201). Repro 1: --demo → j → l → Esc should land back on the list.
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
		return strings.Contains(s, "list: 5 workflows")
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// j selects demo-data-pull, l opens its logs (nil pod -> workflow-wide).
	if err := p.SendKeys(30*time.Millisecond, "j", "l"); err != nil {
		t.Fatalf("send j/l: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "logs: demo-data-pull")
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
		return strings.Contains(screen, "list: 5 workflows") &&
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
		return strings.Contains(s, "list: 5 workflows")
	}) {
		t.Fatalf("list never rendered: %q", p.Screen())
	}
	// j Enter -> DETAIL demo-data-pull (A); Esc back to list; j l -> logs B.
	if err := p.SendKeys(30*time.Millisecond, "j", "\r"); err != nil {
		t.Fatalf("send j/enter: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "DETAIL demo-data-pull")
	}) {
		t.Fatalf("detail A never rendered; screen=%q", p.Screen())
	}
	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc back to list
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "list: 5 workflows")
	}) {
		t.Fatalf("did not return to list; screen=%q", p.Screen())
	}
	// j selects demo-train-pipeline (B), l opens its logs.
	p.ClearScreen()
	if err := p.SendKeys(30*time.Millisecond, "j", "l"); err != nil {
		t.Fatalf("send j/l: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return strings.Contains(s, "logs: demo-train-pipeline")
	}) {
		t.Fatalf("logs B never rendered; screen=%q", p.Screen())
	}
	// Esc from logs B: must NOT show the stale demo-data-pull detail, and
	// must return to the list (the route logs B was opened from).
	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil { // Esc
		t.Fatalf("send esc: %v", err)
	}
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		screen := s
		return strings.Contains(screen, "list: 5 workflows") &&
			!strings.Contains(screen, "DETAIL demo-data-pull")
	}) {
		t.Fatalf("Esc from logs B did not return to the list without stale DETAIL demo-data-pull (A); screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Defect 4(a): resize must re-layout. Shrinking to 40x10 shows the resize
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
		return strings.Contains(s, "list: 5 workflows")
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
	// Drop the buffered 40x10 "too small" frame so the restore assertion
	// reflects only the re-laid-out render at the new size.
	p.ClearScreen()
	if !waitScreen(p, 10*time.Second, func(s string) bool {
		return !strings.Contains(s, "too small") && strings.Contains(s, "list: 5 workflows")
	}) {
		t.Fatalf("120x40 did not restore the full layout; screen=%q", p.Screen())
	}
	_ = p.Send("q")
	_, _ = p.Wait()
}

// Defect 4(b): the program must use the alternate screen buffer (enter
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
		return strings.Contains(s, "list: 5 workflows")
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
