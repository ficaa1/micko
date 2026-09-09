//go:build integration

package integration

// pty.go — ET-3 process/PTY lifecycle harness (docs/test-environment.md
// §5): spawn the REAL argo-tui binary under a pseudo-terminal, script
// input bytes, capture output, and assert exit/terminal-state behavior.
//
// Dependency note: no third-party PTY library is available (E1 may not
// touch go.mod); this harness uses the raw syscall API (posix_openpt /
// grantpt(unlockpt equivalent via TIOCSPTLCK ioctl) / ptsname via
// TIOCGPTN) — the same mechanism github.com/creack/pty wraps. Linux-only,
// which matches the documented host (ET-3 runs on the go1.25 linux/amd64
// LXC; docs/test-environment.md §1).
//
// tea v2 raw mode: on this host the library leaves the TERMIO state in
// its default (canonical echo) configuration for this flow, so output is
// asserted against an ANSI-scrubbed copy (text-first) while the RAW
// capture is used for restore-sequence checks. Anything the OS cannot
// report here is recorded honestly, not claimed.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	ioctlReadTermios  = 0x5401 // TCGETS
	ioctlWriteTermios = 0x5402 // TCSETS
)

// termios mirrors the kernel struct for amd64 linux (asm-generic/termbits.h).
type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Line   uint8
	Cc     [32]uint8
	Ispeed uint32
	Ospeed uint32
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// openPTY allocates a pty pair (equivalent of posix_openpt+grantpt+unlockpt+ptsname).
func openPTY() (master, slave *os.File, err error) {
	masterFD, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	master = os.NewFile(uintptr(masterFD), "/dev/ptmx")

	// Unlock the slave (grantpt+unlockpt): clear the TIOCSPTLCK flag
	// (_IOW('T', 0x31, int) = 0x40045431).
	var unlock int32 = 0
	const tiocsptlck = 0x40045431
	if err := ioctl(master.Fd(), tiocsptlck, unsafe.Pointer(&unlock)); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("unlockpt: %w", err)
	}

	// Slave name: /dev/pts/<TIOCGPTN> (_IOR('T', 0x30, int) = 0x80045430).
	var n int32
	const tiocgptn = 0x80045430
	if err := ioctl(master.Fd(), tiocgptn, unsafe.Pointer(&n)); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("ptsname: %w", err)
	}
	slavePath := fmt.Sprintf("/dev/pts/%d", n)

	slaveFD, err := syscall.Open(slavePath, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	slave = os.NewFile(uintptr(slaveFD), slavePath)
	return master, slave, nil
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

// PTYProcess is one running argo-tui under a pseudo-terminal.
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
	env := os.Environ()
	env = append(env, "TERM=xterm-256color", "NO_COLOR=", "COLORTERM=")
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

// stripANSI removes CSI/OSC escape sequences and C0/C1 controls (except
// newline) for text-first assertions. Deliberately simple; the production
// sanitizer under test is internal/ui/shared.Sanitize (unit-covered in F1).
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == 0x1B && i+1 < len(s) && s[i+1] == '[':
			// CSI: skip to final byte 0x40–0x7E.
			i += 2
			for i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7E) {
				i++
			}
		case c == 0x1B && i+1 < len(s) && s[i+1] == ']':
			// OSC: skip to BEL or ESC\.
			i += 2
			for i < len(s) {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		case c == 0x1B:
			// Two-byte ESC sequence: skip one more byte.
			i++
		case c < 0x20 && c != '\n' && c != '\t':
			// C0 control: drop.
		case c >= 0x7F && c < 0xA0:
			// C1 control: drop.
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
