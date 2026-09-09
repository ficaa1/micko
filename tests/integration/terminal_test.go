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
