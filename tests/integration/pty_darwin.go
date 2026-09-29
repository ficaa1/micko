//go:build integration && darwin

package integration

// pty_darwin.go — macOS pty allocation.
//
// The Linux path (pty_linux.go) uses TIOCSPTLCK, TIOCGPTN and /dev/pts/<n>.
// None of those exist on darwin: the ioctl numbers are unassigned, so the
// kernel answers ENOTTY ("inappropriate ioctl for device") and every PTY test
// fails before the binary is ever started. macOS instead grants and unlocks
// the replica with TIOCPTYGRANT / TIOCPTYUNLK and names it with TIOCPTYGNAME,
// which returns a /dev/ttys<nnn> path in a fixed 128-byte buffer.
//
// This file is why the PTY suite runs on a developer Mac at all. Without it
// the whole PTY harness is silently inert on darwin — which is how a
// demo with unroutable keys reached a beta handoff with the tests "passing".

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	// _IO('t', 84) — grant access to the replica side.
	tiocPtyGrant = 0x20007454
	// _IO('t', 82) — unlock the replica side.
	tiocPtyUnlk = 0x20007452
	// _IOC(IOC_OUT, 't', 83, 128) — read the replica's device path.
	tiocPtyGname = 0x40807453
	// ptyGnameLen is the fixed buffer size TIOCPTYGNAME writes into.
	ptyGnameLen = 128
)

// openPTY allocates a pty pair (posix_openpt + grantpt + unlockpt + ptsname).
func openPTY() (master, slave *os.File, err error) {
	masterFD, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	master = os.NewFile(uintptr(masterFD), "/dev/ptmx")

	if err := ioctl(master.Fd(), tiocPtyGrant, nil); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("grantpt: %w", err)
	}
	if err := ioctl(master.Fd(), tiocPtyUnlk, nil); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("unlockpt: %w", err)
	}

	var buf [ptyGnameLen]byte
	if err := ioctl(master.Fd(), tiocPtyGname, unsafe.Pointer(&buf[0])); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("ptsname: %w", err)
	}
	n := bytes.IndexByte(buf[:], 0)
	if n < 0 {
		n = len(buf)
	}
	slavePath := string(buf[:n])

	slaveFD, err := syscall.Open(slavePath, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	slave = os.NewFile(uintptr(slaveFD), slavePath)
	return master, slave, nil
}
