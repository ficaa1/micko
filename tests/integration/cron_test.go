//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestPTYCronDrillDown drives the real binary: `:cron` shows the cron
// workflows, enter on the hourly one lists the runs it started, and esc
// returns to the cron list.
//
// The renderer repaints only the cells that change, so each check looks for
// text that the new screen writes whole: a toolbar that differs from the
// previous one from its first cell, and cells of a column that held
// something else.
func TestPTYCronDrillDown(t *testing.T) {
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
	if err := p.SendKeys(50*time.Millisecond, ":", "c", "r", "o", "n", "\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "Sort: next run, suspended last") && strings.Contains(s, "demo-weekly-compaction")
	}) {
		t.Fatalf(":cron did not show the cron list; screen=%q", p.Screen())
	}

	// The hourly ETL is first: its next run is the soonest.
	p.ClearScreen()
	if err := p.Send("\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "Workflows ← cron demo-etl-hourly") && strings.Contains(s, "Phase: All")
	}) {
		t.Fatalf("enter did not list the cron workflow's runs; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("\x1b"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool { return strings.Contains(s, "Sort: next run, suspended last") }) {
		t.Fatalf("esc did not return to the cron list; screen=%q", p.Screen())
	}

	if err := p.SendKeys(50*time.Millisecond, "q"); err != nil {
		t.Fatal(err)
	}
	if code, err := p.WaitFor(5 * time.Second); err != nil || code != 0 {
		t.Fatalf("q exit = %d, %v; want 0", code, err)
	}
}
