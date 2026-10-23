package radio

import (
	"syscall"
	"time"
	"unsafe"
)

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
	crtscts         = 0x00030000 // CCTS_OFLOW | CRTS_IFLOW
)

var baudRates = map[int]uint64{9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400, 57600: syscall.B57600, 115200: syscall.B115200}

func setSpeed(t *syscall.Termios, speed uint64) {
	t.Ispeed = speed
	t.Ospeed = speed
}

func flush(fd int) {
	what := syscall.TCIOFLUSH
	ioctl(fd, syscall.TIOCFLUSH, uintptr(unsafe.Pointer(&what)))
}

func selectRead(fd int, timeout time.Duration) (bool, error) {
	var set syscall.FdSet
	set.Bits[fd/32] |= 1 << (uint(fd) % 32)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.Select(fd+1, &set, nil, nil, &tv); err != nil {
		return false, err
	}
	return set.Bits[fd/32]&(1<<(uint(fd)%32)) != 0, nil
}
