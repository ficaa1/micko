//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestPTYArchived drives the real binary: `:aw` lists the archive, enter
// opens the newest run marked as archived, and esc returns to the archive
// list. Checks look for text the renderer writes whole.
func TestPTYArchived(t *testing.T) {
	bin := buildBinary(t)
	p, err := StartPTYSize(120, 30, bin, "--demo")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	}()
	if !waitScreen(p, 10*time.Second, func(s string) bool { return strings.Contains(s, demoListed) }) {
		t.Fatalf("demo list never rendered; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.SendKeys(50*time.Millisecond, ":", "a", "w", "\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "Sort: newest") && strings.Contains(s, "demo-backfill-2026-q1")
	}) {
		t.Fatalf(":aw did not list the archive; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "the workflow archive") && strings.Contains(s, "synthetic-uid-archived-")
	}) {
		t.Fatalf("enter did not open the archived run; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool { return strings.Contains(s, "Sort: newest") }) {
		t.Fatalf("esc did not return to the archive list; screen=%q", p.Screen())
	}

	if err := p.SendKeys(50*time.Millisecond, "q"); err != nil {
		t.Fatal(err)
	}
	if code, err := p.WaitFor(5 * time.Second); err != nil || code != 0 {
		t.Fatalf("q exit = %d, %v; want 0", code, err)
	}
}
