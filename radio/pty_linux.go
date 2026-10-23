package radio

import (
	"fmt"
	"syscall"
	"unsafe"
)

// openPTY opens a pseudo-terminal pair and returns the controlling side and
// the device path of the other side.
func openPTY() (int, string, error) {
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	var unlock int32
	if err := ioctl(fd, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		syscall.Close(fd)
		return -1, "", fmt.Errorf("unlock pty: %w", err)
	}
	var n uint32
	if err := ioctl(fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		syscall.Close(fd)
		return -1, "", fmt.Errorf("pty number: %w", err)
	}
	return fd, fmt.Sprintf("/dev/pts/%d", n), nil
}
