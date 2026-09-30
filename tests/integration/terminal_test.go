//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/buildinfo"
)

// demoRows is the demo list in the order the default sort produces: phase
// first (Suspended, then Failed and Error, then Running, then Pending, then
// Succeeded), and newest first inside a phase.
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

// uid is the detail summary line that names wf. The pane title shares its
// line with a summary the renderer repaints cell by cell, so a capture can
// hold it spliced; the uid line is written whole.
func uid(wf string) string { return "synthetic-uid-" + wf }

var (
	buildOnce sync.Once
	binDir    string
	binPath   string
	buildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// binary is the micko binary built from this tree, once per test run.
func binary(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("no pseudo-terminals on this host: %v", err)
	}
	buildOnce.Do(func() {
		if binDir, buildErr = os.MkdirTemp("", "micko-pty-"); buildErr != nil {
			return
		}
		binPath = filepath.Join(binDir, "micko")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/ficaa1/micko/cmd/micko")
		cmd.Dir = filepath.Join("..", "..")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{err, out}
		}
	})
	if buildErr != nil {
		t.Fatalf("build micko: %v", buildErr)
	}
	return binPath
}

type buildError struct {
	err error
	out []byte
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + string(e.out) }

// start runs micko with args in a cols×rows terminal that closes with t.
func start(t *testing.T, cols, rows int, args ...string) *PTYProcess {
	t.Helper()
	p, err := StartPTYSize(cols, rows, binary(t), args...)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if !p.Exited() {
			p.Kill()
		}
		p.Close()
	})
	return p
}

// await fails unless every want is on screen and no absent one is, within
// ten seconds.
func await(t *testing.T, p *PTYProcess, want, absent []string) {
	t.Helper()
	missing := func(s string) string {
		for _, w := range want {
			if !strings.Contains(s, w) {
				return "lacks " + strconv.Quote(w)
			}
		}
		for _, a := range absent {
			if strings.Contains(s, a) {
				return "shows " + strconv.Quote(a)
			}
		}
		return ""
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if missing(p.Screen()) == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if why := missing(p.Screen()); why != "" {
		t.Fatalf("the screen %s:\n%s", why, p.Screen())
	}
}

// keyBytes are the bytes a terminal sends for the named keys.
var keyBytes = map[string]string{
	"enter": "\r", "esc": "\x1b", "tab": "\t", "space": " ", "ctrl+c": "\x03",
}

// send presses each key 50ms apart, a named key as its bytes and anything
// else as typed text.
func send(t *testing.T, p *PTYProcess, keys ...string) {
	t.Helper()
	var seq []string
	for _, k := range keys {
		if b, ok := keyBytes[k]; ok {
			seq = append(seq, b)
			continue
		}
		for _, r := range k {
			seq = append(seq, string(r))
		}
	}
	if err := p.SendKeys(50*time.Millisecond, seq...); err != nil {
		t.Fatalf("send %q: %v", keys, err)
	}
}

// quit presses keys and fails unless micko then exits 0.
func quit(t *testing.T, p *PTYProcess, keys ...string) {
	t.Helper()
	send(t, p, keys...)
	if code, err := p.WaitFor(10 * time.Second); err != nil || code != 0 {
		t.Fatalf("exit = %d, %v; want 0", code, err)
	}
}

// Every way out of micko exits 0 and leaves the alternate screen it
// entered: q and ctrl+c from the demo, esc from the profile picker, and
// --version before any screen.
func TestPTYExits(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "config.yaml")
	body := "currentProfile: prod\nprofiles:\n" +
		"  dev:\n    server: https://dev.invalid\n    namespace: workflows\n    tokenEnv: MICKO_TEST\n" +
		"  prod:\n    server: https://prod.invalid\n    namespace: argo\n    tokenEnv: MICKO_TEST\n"
	if err := os.WriteFile(configured, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		args    []string
		ready   []string
		keys    []string
		altScrn bool
	}{
		{"q from the demo", []string{"--demo"}, []string{demoListed}, []string{"q"}, true},
		{"ctrl+c from the demo", []string{"--demo"}, []string{demoListed}, []string{"ctrl+c"}, true},
		{"esc from an empty picker", []string{"--config", filepath.Join(t.TempDir(), "none.yaml")},
			[]string{"no profiles configured", "write them to:", "currentProfile: dev", "tokenEnv: ARGO_TOKEN"}, []string{"esc"}, true},
		{"esc from the configured profiles", []string{"--config", configured},
			[]string{"choose a profile", "https://dev.invalid", "https://prod.invalid"}, []string{"esc"}, true},
		{"--version", []string{"--version"}, []string{"micko " + buildinfo.Version}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := start(t, 100, 30, c.args...)
			await(t, p, c.ready, nil)
			quit(t, p, c.keys...)
			raw := p.Raw()
			for _, seq := range []string{"\x1b[?1049h", "\x1b[?1049l"} {
				if strings.Contains(raw, seq) != c.altScrn {
					t.Errorf("raw output holds %q: %v, want %v", seq, !c.altScrn, c.altScrn)
				}
			}
		})
	}
}

// The first frame at every size keeps the key hints, draws the bordered
// shell with the context band where it fits, carries each phase as a glyph
// and a word, and asks for room where nothing fits; q quits from each.
func TestPTYFirstFrame(t *testing.T) {
	cases := []struct {
		cols, rows int
		want       []string
		absent     []string
	}{
		{100, 30, []string{"┌", "└", "Workflows", "READ ONLY", "ns: demo", "? help", "✗ Failed", "● Running", demoListed}, nil},
		{80, 24, []string{"40m", demoListed}, nil},
		{100, 14, []string{"? help", demoListed}, nil},
		{100, 11, []string{"? help"}, nil},
		{100, 9, []string{"? help"}, nil},
		{50, 16, []string{"help"}, []string{"┌"}},
		{40, 10, []string{"too small"}, nil},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.cols)+"x"+strconv.Itoa(c.rows), func(t *testing.T) {
			p := start(t, c.cols, c.rows, "--demo")
			await(t, p, c.want, c.absent)
			quit(t, p, "q")
		})
	}
}

// step is one thing a reader does in the demo and what the screen then shows.
type step struct {
	// resize changes the terminal size before the keys when it is set.
	resize [2]int
	keys   []string
	// clear drops the capture before anything else, after a pause for frames
	// already on their way, so the check sees only what the step drew. The
	// renderer repaints only the cells that change, so each check is for
	// text the new frame writes whole.
	clear  bool
	want   []string
	absent []string
	// after, when set, is text the want must follow: a frame drawn before
	// the step can still land after the clear.
	after string
}

// Journeys through the demo: each starts on the loaded list and ends with a
// clean exit.
func TestPTYDemoJourneys(t *testing.T) {
	cases := []struct {
		name       string
		cols, rows int
		steps      []step
		quit       []string
	}{
		{"j and enter open the second row", 80, 24, []step{
			{keys: []string{"j", "enter"}, want: []string{uid(demoRows[1]), "✗ Failed"}},
		}, nil},
		{"the detail keeps the frame", 100, 30, []step{
			{keys: []string{"enter"}, want: []string{uid(demoRows[0]), "tab section", "? help"}},
		}, nil},
		{"esc from logs opened on the list returns to it", 80, 24, []step{
			{keys: []string{"j", "l"}, want: []string{"Logs " + demoRows[1]}},
			{keys: []string{"esc"}, clear: true, want: []string{demoListed}, absent: []string{"(no workflow loaded)"}},
		}, nil},
		{"esc from logs never lands on an earlier detail", 80, 24, []step{
			{keys: []string{"j", "enter"}, want: []string{uid(demoRows[1])}},
			{keys: []string{"esc"}, clear: true, want: []string{demoListed}},
			{keys: []string{"j", "l"}, clear: true, want: []string{"Logs " + demoRows[2]}},
			{keys: []string{"esc"}, clear: true, want: []string{demoListed}, absent: []string{uid(demoRows[1])}},
		}, nil},
		{"? opens help and esc closes it", 100, 30, []step{
			{keys: []string{"?"}, want: []string{"KEYS"}},
			{keys: []string{"esc"}, clear: true, want: []string{demoListed}, absent: []string{"KEYS"}},
		}, nil},
		{"q closes help without quitting", 100, 30, []step{
			{keys: []string{"?"}, want: []string{"KEYS"}},
			{keys: []string{"q"}, clear: true, want: []string{demoListed}, absent: []string{"KEYS"}},
		}, nil},
		{"esc clears a filter that matches nothing", 100, 30, []step{
			{keys: []string{"/", "zzz", "enter"}, want: []string{"no workflows match"}},
			{keys: []string{"esc"}, clear: true, want: []string{"demo-hello-world"}, absent: []string{"no workflows match"}},
		}, nil},
		{"a typed query is parsed, a bad term shown, and w widens", 140, 30, []step{
			{keys: []string{"/phase=failed cron=demo-etl-hourly"}},
			{keys: []string{"enter"}, clear: true, want: []string{"Failed & cron=demo-etl-hourly [within 12 collected]"}},
			{keys: []string{"/ age<2x"}, clear: true, want: []string{`✗ bad duration "2x"`}},
			{keys: []string{"esc"}},
			{keys: []string{"w"}, clear: true, want: []string{"STARTED", "TEMPLATE"}},
		}, nil},
		{"two marks open the bulk pane", 120, 30, []step{
			{keys: []string{"space"}, clear: true, want: []string{"◆ 1 marked"}},
			{keys: []string{"j", "space"}},
			{keys: []string{"a"}, clear: true, want: []string{"Bulk action", "targets: 2 marked workflows", "demo mode: actions unavailable"}},
		}, []string{"ctrl+c"}},
		{"the palette types q, switches namespace and lists every one", 120, 30, []step{
			{keys: []string{":", "q"}, clear: true, want: []string{"leave micko"}},
			{keys: []string{"esc"}},
			{keys: []string{":", "ns ml", "tab", "enter"}, clear: true, want: []string{"namespace: demo-ml", "ml-batch-infer"}},
			{keys: []string{"0"}, clear: true, want: []string{"in 2 namespaces", "demo-ml    demo-ml-batch-infer"}},
		}, []string{":quit", "enter"}},
		{"cron workflows drill into their runs", 120, 30, []step{
			{keys: []string{":cron", "enter"}, clear: true, want: []string{"Sort: next run, suspended last", "demo-weekly-compaction"}},
			{keys: []string{"enter"}, clear: true, want: []string{"Workflows ← cron demo-etl-hourly", "Phase: All"}},
			{keys: []string{"esc"}, clear: true, want: []string{"Sort: next run, suspended last"}},
		}, nil},
		{"templates and cluster templates", 120, 30, []step{
			{keys: []string{":tmpl", "enter"}, clear: true, want: []string{"Sort: name", "nightly-report"}},
			{keys: []string{":cwftmpl", "enter"}, clear: true, want: []string{"(cluster-scoped)", "whalesay"}},
			{keys: []string{"0"}, clear: true, want: []string{"belong to no namespace"}},
		}, nil},
		{"an archived run opens and esc returns to the archive", 120, 30, []step{
			{keys: []string{":aw", "enter"}, clear: true, want: []string{"Sort: newest", "demo-backfill-2026-q1"}},
			{keys: []string{"enter"}, clear: true, want: []string{"the workflow archive", "synthetic-uid-archived-"}},
			{keys: []string{"esc"}, clear: true, want: []string{"Sort: newest"}},
		}, nil},
		{"resizing re-lays out the frame", 120, 40, []step{
			{resize: [2]int{100, 10}, clear: true, want: []string{"? help", demoListed}},
			{resize: [2]int{90, 16}, clear: true, want: []string{"└", "? help"}},
			{resize: [2]int{40, 10}, want: []string{"too small"}},
			{resize: [2]int{120, 40}, clear: true, after: "Resize to at least", want: []string{demoListed}},
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := start(t, c.cols, c.rows, "--demo")
			await(t, p, []string{demoListed}, nil)
			for i, s := range c.steps {
				if s.clear {
					time.Sleep(200 * time.Millisecond)
					p.ClearScreen()
				}
				if s.resize != [2]int{} {
					if err := p.Resize(s.resize[0], s.resize[1]); err != nil {
						t.Fatalf("step %d: resize: %v", i, err)
					}
				}
				send(t, p, s.keys...)
				if s.after != "" {
					awaitAfter(t, p, s.after, s.want)
					continue
				}
				if s.want == nil && s.absent == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				await(t, p, s.want, s.absent)
			}
			q := c.quit
			if q == nil {
				q = []string{"q"}
			}
			quit(t, p, q...)
		})
	}
}

// awaitAfter fails unless every want is on screen after the last after.
func awaitAfter(t *testing.T, p *PTYProcess, after string, want []string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := p.Screen()
		if i := strings.LastIndex(s, after); i >= 0 {
			s = s[i+len(after):]
		}
		ok := true
		for _, w := range want {
			ok = ok && strings.Contains(s, w)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the screen lacks %q after the last %q:\n%s", want, after, p.Screen())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The demo opens no TCP connection: none of its sockets is in the kernel's
// TCP tables.
func TestPTYDemoOpensNoConnection(t *testing.T) {
	p := start(t, 100, 30, "--demo")
	await(t, p, []string{demoListed}, nil)
	pid := strconv.Itoa(p.Cmd.Process.Pid)
	fds, err := os.ReadDir("/proc/" + pid + "/fd")
	if err != nil {
		t.Skipf("no /proc on this host: %v", err)
	}
	sockets := map[string]bool{}
	for _, fd := range fds {
		link, err := os.Readlink("/proc/" + pid + "/fd/" + fd.Name())
		if inode, ok := strings.CutPrefix(link, "socket:["); err == nil && ok {
			sockets[strings.TrimSuffix(inode, "]")] = true
		}
	}
	for _, table := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile("/proc/" + pid + "/net/" + table)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			if f := strings.Fields(line); len(f) > 9 && sockets[f[9]] {
				t.Errorf("the demo holds a TCP socket: %s", line)
			}
		}
	}
	quit(t, p, "q")
}
