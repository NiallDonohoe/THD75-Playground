package radio

import (
	"syscall"
	"time"
)

const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
	crtscts         = 0x80000000
)

var baudRates = map[int]uint32{9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400, 57600: syscall.B57600, 115200: syscall.B115200}

func setSpeed(t *syscall.Termios, speed uint32) {
	t.Cflag &^= 0x100f // CBAUD
	t.Cflag |= speed
	t.Ispeed = speed
	t.Ospeed = speed
}

func flush(fd int) {
	ioctl(fd, 0x540B, syscall.TCIOFLUSH) // TCFLSH
}

func selectRead(fd int, timeout time.Duration) (bool, error) {
	var set syscall.FdSet
	set.Bits[fd/64] |= 1 << (uint(fd) % 64)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	n, err := syscall.Select(fd+1, &set, nil, nil, &tv)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
