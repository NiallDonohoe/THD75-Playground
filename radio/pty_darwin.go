package radio

import (
	"bytes"
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
	if err := ioctl(fd, syscall.TIOCPTYGRANT, 0); err != nil {
		syscall.Close(fd)
		return -1, "", fmt.Errorf("grant pty: %w", err)
	}
	if err := ioctl(fd, syscall.TIOCPTYUNLK, 0); err != nil {
		syscall.Close(fd)
		return -1, "", fmt.Errorf("unlock pty: %w", err)
	}
	var name [128]byte
	if err := ioctl(fd, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		syscall.Close(fd)
		return -1, "", fmt.Errorf("pty name: %w", err)
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	return fd, path, nil
}
