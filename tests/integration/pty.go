//go:build integration

package integration

// PTY helpers drive the compiled binary and capture terminal output.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// setWinsize sets the pty size (TIOCSWINSZ — note 0x5414; 0x5413 is the
// GET code, a swap that silently leaves the pty at 0x0).
func setWinsize(fd uintptr, cols, rows uint16) error {
	ws := struct{ Row, Col, X, Y uint16 }{Row: rows, Col: cols}
	return ioctl(fd, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

// getWinsize reads the pty size (TIOCGWINSZ).
func getWinsize(fd uintptr) (cols, rows uint16, err error) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(fd, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, err
	}
	return ws.Col, ws.Row, nil
}

// PTYProcess is one running micko under a pseudo-terminal.
type PTYProcess struct {
	Cmd     *exec.Cmd
	master  *os.File
	slave   *os.File
	mu      sync.Mutex
	buf     strings.Builder
	scrub   strings.Builder
	done    chan struct{}
	errRead error
}

// StartPTY spawns the binary attached to a fresh 80x24 PTY.
func StartPTY(binPath string, args ...string) (*PTYProcess, error) {
	return StartPTYSize(80, 24, binPath, args...)
}

// StartPTYSize spawns the binary with an explicit terminal size (resize
// and small-terminal acceptance cases).
func StartPTYSize(cols, rows int, binPath string, args ...string) (*PTYProcess, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	// Set the size on the SLAVE (the fd the child actually opens as its
	// controlling terminal); the master ioctl alone does not propagate
	// before the child's session setup on this kernel.
	if err := setWinsize(slave.Fd(), uint16(cols), uint16(rows)); err != nil {
		master.Close()
		slave.Close()
		return nil, fmt.Errorf("set winsize: %w", err)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	// Deterministic terminal environment: TERM with a color-capable value
	// (probe-verified rendering path), NO_COLOR empty for color assertions,
	// and the pty as controlling terminal of a fresh session.
	// Point every config candidate at an empty directory. DefaultConfigPath
	// tries $XDG_CONFIG_HOME, then $HOME/.config, then os.UserConfigDir,
	// so both variables have to move. A developer's real config would
	// otherwise make a no-argument start connect to their live Argo server
	// and wait for keys, instead of refusing as the test expects.
	noConfig := emptyConfigHome()
	env := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "HOME="),
			strings.HasPrefix(kv, "XDG_CONFIG_HOME="):
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color", "NO_COLOR=", "COLORTERM=",
		"HOME="+noConfig, "XDG_CONFIG_HOME="+noConfig)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		return nil, err
	}
	slave.Close() // parent keeps only the master end

	// Re-assert the size on the master after the child's session setup:
	// the kernel delivers SIGWINCH to the foreground process group, which
	// the Tea event loop consumes as the authoritative WindowSizeMsg.
	if err := setWinsize(master.Fd(), uint16(cols), uint16(rows)); err != nil {
		_ = cmd.Process.Kill()
		master.Close()
		return nil, fmt.Errorf("re-set winsize: %w", err)
	}
	if gotC, gotR, _ := getWinsize(master.Fd()); gotC != uint16(cols) || gotR != uint16(rows) {
		_ = cmd.Process.Kill()
		master.Close()
		return nil, fmt.Errorf("pty winsize not applied (got %dx%d, want %dx%d)", gotC, gotR, cols, rows)
	}

	p := &PTYProcess{Cmd: cmd, master: master, slave: nil, done: make(chan struct{})}
	go p.readLoop()
	return p, nil
}

// emptyConfigHome returns a directory that holds no micko config, created
// once per test binary.
func emptyConfigHome() string {
	configHomeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "micko-noconfig-")
		if err != nil {
			// Fall back to a path that cannot hold a config either.
			dir = filepath.Join(os.TempDir(), "micko-noconfig-missing")
		}
		configHome = dir
	})
	return configHome
}

var (
	configHomeOnce sync.Once
	configHome     string
)

// readLoop accumulates output until EOF (process exit).
func (p *PTYProcess) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.master.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			p.mu.Lock()
			p.buf.WriteString(chunk)
			p.scrub.WriteString(stripANSI(chunk))
			p.mu.Unlock()
		}
		if err != nil {
			p.mu.Lock()
			p.errRead = err
			p.mu.Unlock()
			break
		}
	}
	close(p.done)
}

// Send writes scripted key bytes to the PTY.
func (p *PTYProcess) Send(keys string) error {
	_, err := p.master.Write([]byte(keys))
	return err
}

// SendKeys writes a sequence of key strings with a small delay between
// them (deterministic scripting).
func (p *PTYProcess) SendKeys(delay time.Duration, keys ...string) error {
	for _, k := range keys {
		if err := p.Send(k); err != nil {
			return err
		}
		time.Sleep(delay)
	}
	return nil
}

// Resize changes the pty size and signals SIGWINCH to the child so the Tea
// loop re-queries the terminal size and delivers a fresh WindowSizeMsg.
func (p *PTYProcess) Resize(cols, rows int) error {
	if err := setWinsize(p.master.Fd(), uint16(cols), uint16(rows)); err != nil {
		return err
	}
	return p.Cmd.Process.Signal(syscall.SIGWINCH)
}

// Screen returns the ANSI-scrubbed captured output so far.
func (p *PTYProcess) Screen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scrub.String()
}

// ClearScreen discards accumulated output so far, so a later Screen()/Raw()
// reflects only output produced after the call (used to assert a post-resize
// re-layout rather than stale buffered frames).
func (p *PTYProcess) ClearScreen() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf.Reset()
	p.scrub.Reset()
}

// Raw returns the raw captured output (restore-sequence checks).
func (p *PTYProcess) Raw() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// Wait blocks until the process exits, returning its exit code. A signal
// kill returns code -1 with the wait error.
func (p *PTYProcess) Wait() (int, error) {
	<-p.done
	err := p.Cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.Sys().(syscall.WaitStatus).ExitStatus(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// WaitFor is Wait with a deadline. A process that never exits fails the test
// in seconds instead of holding the whole package until the go test timeout.
func (p *PTYProcess) WaitFor(d time.Duration) (int, error) {
	select {
	case <-p.done:
		return p.Wait()
	case <-time.After(d):
		p.Kill()
		return -1, fmt.Errorf("process did not exit within %s", d)
	}
}

// Exited reports whether the process has exited (non-blocking).
func (p *PTYProcess) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Close releases the master fd after exit.
func (p *PTYProcess) Close() {
	if p.master != nil {
		_ = p.master.Close()
	}
}

// Kill sends SIGKILL (last-resort cleanup in deferred guards).
func (p *PTYProcess) Kill() {
	if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Kill()
		<-p.done
	}
}

// stripANSI turns captured pty output into the text a viewer would see.
//
// It is not a full VT emulator, but it must model horizontal position,
// because a renderer does not write runs of spaces: it moves the cursor.
// A pure byte-stripper therefore glued neighbouring cells together — a
// padded table came back as "NAMEPHASE" — so every geometry assertion
// silently passed on text that was never on screen. It also dropped UTF-8
// continuation bytes as "C1 controls", which destroyed box-drawing
// characters and status glyphs.
//
// The production sanitizer under test is internal/ui/shared.Sanitize, which
// is unit-covered separately; this is only the read-back path.
func stripANSI(s string) string {
	var b strings.Builder
	col := 0 // cells written on the current line

	// moveTo pads with spaces up to target. A backwards move cannot be
	// represented in append-only text, so it is ignored: over-writing is
	// rare in this renderer and padding is the safer failure.
	moveTo := func(target int) {
		if target > col {
			b.WriteString(strings.Repeat(" ", target-col))
			col = target
		}
	}

	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1B && i+1 < len(s) && s[i+1] == '[':
			start := i + 2
			i += 2
			for i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7E) {
				i++
			}
			if i >= len(s) {
				break
			}
			params := s[start:i]
			switch s[i] {
			case 'C': // CUF: cursor forward N columns
				moveTo(col + csiParam(params, 0, 1))
			case 'G': // CHA: cursor to absolute column (1-based)
				moveTo(csiParam(params, 0, 1) - 1)
			case 'H', 'f': // CUP: row;col — only the column is representable
				moveTo(csiParam(params, 1, 1) - 1)
			}
			i++
		case c == 0x1B && i+1 < len(s) && s[i+1] == ']':
			// OSC: skip to BEL or ESC\.
			i += 2
			for i < len(s) {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		case c == 0x1B:
			i += 2 // two-byte ESC sequence
		case c == '\n':
			b.WriteByte('\n')
			col = 0
			i++
		case c == '\t':
			moveTo((col/8 + 1) * 8)
			i++
		case c == '\r':
			i++ // carriage return: column reset is not representable here
		case c < 0x20:
			i++ // other C0 control: drop
		case c < 0x80:
			b.WriteByte(c)
			col++
			i++
		default:
			// Multibyte UTF-8: decode the rune rather than dropping its
			// continuation bytes as C1 controls.
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n <= 1 {
				i++
				continue
			}
			if r >= 0x7F && r < 0xA0 {
				i += n // real C1 control
				continue
			}
			b.WriteString(s[i : i+n])
			col++
			i++
			i += n - 1
		}
	}
	return b.String()
}

// csiParam reads the nth ";"-separated numeric parameter of a CSI sequence,
// returning def when it is absent or unparsable (per ECMA-48, an omitted or
// zero parameter means the default).
func csiParam(params string, n, def int) int {
	fields := strings.Split(params, ";")
	if n >= len(fields) || fields[n] == "" {
		return def
	}
	v := 0
	for _, r := range fields[n] {
		if r < '0' || r > '9' {
			return def
		}
		v = v*10 + int(r-'0')
	}
	if v == 0 {
		return def
	}
	return v
}
