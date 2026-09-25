//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestPTYTemplates drives the real binary: `:tmpl` lists the namespace's
// templates, `:cwftmpl` the cluster's with the header saying so, and `0`
// there explains that the namespace keys do not apply instead of switching.
// Checks look for text the renderer writes whole.
func TestPTYTemplates(t *testing.T) {
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
	if err := p.SendKeys(50*time.Millisecond, ":", "t", "m", "p", "l", "\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "Sort: name") && strings.Contains(s, "nightly-report")
	}) {
		t.Fatalf(":tmpl did not list the templates; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.SendKeys(50*time.Millisecond, ":", "c", "w", "f", "t", "m", "p", "l", "\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "(cluster-scoped)") && strings.Contains(s, "whalesay")
	}) {
		t.Fatalf(":cwftmpl did not list the cluster templates; screen=%q", p.Screen())
	}

	p.ClearScreen()
	if err := p.Send("0"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool { return strings.Contains(s, "belong to no namespace") }) {
		t.Fatalf("0 on the cluster list did not explain itself; screen=%q", p.Screen())
	}

	if err := p.SendKeys(50*time.Millisecond, "q"); err != nil {
		t.Fatal(err)
	}
	if code, err := p.WaitFor(5 * time.Second); err != nil || code != 0 {
		t.Fatalf("q exit = %d, %v; want 0", code, err)
	}
}
