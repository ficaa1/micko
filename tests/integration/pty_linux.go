//go:build integration && linux

package integration

// pty_linux.go — Linux pty allocation. The ioctl numbers and the /dev/pts
// slave naming are Linux-specific; darwin uses a different set entirely
// (see pty_darwin.go).

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

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
