//go:build js && wasm

// Command micko-web runs micko's demo in a browser terminal.
//
// The page sets globalThis.mickoTerminal to an object with write(bytes),
// cols, rows and optionally skin before it starts the program. micko draws
// through write and adds two functions to the same object: input(text) for
// keystrokes and resize(cols, rows) for size changes. The program exits when
// micko quits.
//
// Build it with make web: bubbletea has no js platform file, and the
// Makefile builds against a copy of it with _bubbletea/platform_js.go added.
package main

import (
	"fmt"
	"os"
	"sync"
	"syscall/js"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/ficaa1/micko/internal/app"
	"github.com/ficaa1/micko/internal/buildinfo"
	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func main() {
	term := js.Global().Get("mickoTerminal")
	if term.Type() != js.TypeObject {
		fmt.Fprintln(os.Stderr, "micko: globalThis.mickoTerminal is not set")
		os.Exit(1)
	}

	clock := testkit.NewFakeClock(time.Now().UTC())
	root := app.NewRoot(testkit.DemoReader(clock), clock, "demo", config.DefaultRefreshInterval, actions.Options{
		ReadOnly: true,
		Demo:     true,
		Server:   "synthetic demo",
		Profile:  "demo",
	})
	skin := config.DefaultSkin
	if s := term.Get("skin"); s.Type() == js.TypeString && shared.CheckSkin(s.String()) == nil {
		skin = s.String()
	}
	_, _ = root.ApplySkin(skin)
	root.SetNotify(config.DefaultNotify())
	root.SetVersion(buildinfo.Version + " @ " + buildinfo.Commit)

	in := newInput()
	p := tea.NewProgram(root,
		tea.WithInput(in),
		tea.WithOutput(output{term.Get("write")}),
		tea.WithEnvironment([]string{"TERM=xterm-256color", "COLORTERM=truecolor"}),
		tea.WithColorProfile(colorprofile.TrueColor),
		tea.WithWindowSize(term.Get("cols").Int(), term.Get("rows").Int()),
		tea.WithoutSignalHandler(),
	)

	// A Go function called from JavaScript must return without blocking.
	term.Set("input", js.FuncOf(func(_ js.Value, args []js.Value) any {
		in.push(args[0].String())
		return nil
	}))
	term.Set("resize", js.FuncOf(func(_ js.Value, args []js.Value) any {
		size := tea.WindowSizeMsg{Width: args[0].Int(), Height: args[1].Int()}
		go p.Send(size)
		return nil
	}))

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "micko: %v\n", err)
		os.Exit(1)
	}
}

// output hands each frame to the page's terminal.
type output struct{ write js.Value }

func (o output) Write(b []byte) (int, error) {
	buf := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(buf, b)
	o.write.Invoke(buf)
	return len(b), nil
}

// input queues keystrokes from the page without blocking and hands them to
// the program's read loop in order.
type input struct {
	mu    sync.Mutex
	ready *sync.Cond
	buf   []byte
}

func newInput() *input {
	in := &input{}
	in.ready = sync.NewCond(&in.mu)
	return in
}

func (in *input) push(s string) {
	in.mu.Lock()
	in.buf = append(in.buf, s...)
	in.mu.Unlock()
	in.ready.Signal()
}

func (in *input) Read(p []byte) (int, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	for len(in.buf) == 0 {
		in.ready.Wait()
	}
	n := copy(p, in.buf)
	in.buf = in.buf[n:]
	return n, nil
}
