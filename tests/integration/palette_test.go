//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestPTYPaletteRunsCommands drives the real binary: `:` opens the palette,
// typed text and tab complete a namespace, enter switches to it, `0` widens
// the list to every namespace, and `:quit` exits cleanly. A q typed into the
// palette is a letter, not a quit.
func TestPTYPaletteRunsCommands(t *testing.T) {
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

	// q inside the palette is typed; esc closes the palette, not the program.
	p.ClearScreen()
	if err := p.SendKeys(50*time.Millisecond, ":", "q"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool { return strings.Contains(s, "leave argo-tui") }) {
		t.Fatalf("the palette did not suggest quit for q; screen=%q", p.Screen())
	}
	if err := p.SendKeys(50*time.Millisecond, "\x1b"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if p.Exited() {
		t.Fatal("q typed into the palette quit the program")
	}

	// ns, a space, a fragment, tab, enter: the namespace switches. The
	// footer's flash and a row only demo-ml holds are written whole; the pane
	// title is repainted cell by cell and cannot be matched as one string.
	p.ClearScreen()
	if err := p.SendKeys(50*time.Millisecond, ":", "n", "s", " ", "m", "l", "\t", "\r"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "namespace: demo-ml") && strings.Contains(s, "ml-batch-infer")
	}) {
		t.Fatalf("ns demo-ml did not switch; screen=%q", p.Screen())
	}

	// 0 lists every namespace, each row led by its own namespace. The
	// renderer repaints only the cells that change, so the checks look for
	// text that is written whole: the pane count and a full row.
	p.ClearScreen()
	if err := p.Send("0"); err != nil {
		t.Fatal(err)
	}
	if !waitScreen(p, 5*time.Second, func(s string) bool {
		return strings.Contains(s, "in 2 namespaces") && strings.Contains(s, "demo-ml    demo-ml-batch-infer")
	}) {
		t.Fatalf("0 did not show all namespaces; screen=%q", p.Screen())
	}

	if err := p.SendKeys(50*time.Millisecond, ":", "q", "u", "i", "t", "\r"); err != nil {
		t.Fatal(err)
	}
	code, err := p.WaitFor(5 * time.Second)
	if err != nil || code != 0 {
		t.Fatalf(":quit exit = %d, %v; want 0", code, err)
	}
}
