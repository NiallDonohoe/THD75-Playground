//go:build darwin || linux

package radio

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// SerialPort is a raw 8N1 serial port opened exclusively (TIOCEXCL), with no
// flow control. It is a small termios wrapper over the standard syscall
// package, so the radio package needs no third-party code and no cgo.
type SerialPort struct {
	fd     int
	path   string
	closed atomic.Bool
	readMu sync.Mutex // held by Read; Close waits for it before closing fd
	wrMu   sync.Mutex
}

// ErrPortInUse means another program has the port open exclusively.
var ErrPortInUse = errors.New("serial port is in use by another program")

// OpenSerial opens path at the given baud rate (nominal on a CDC ACM port).
func OpenSerial(path string, baud int) (*SerialPort, error) {
	speed, ok := baudRates[baud]
	if !ok {
		return nil, fmt.Errorf("unsupported baud rate %d", baud)
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return nil, fmt.Errorf("open %s: %w", path, ErrPortInUse)
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	fail := func(what string, err error) (*SerialPort, error) {
		syscall.Close(fd)
		return nil, fmt.Errorf("%s %s: %w", what, path, err)
	}
	if err := ioctl(fd, syscall.TIOCEXCL, 0); err != nil {
		return fail("lock", err)
	}
	var t syscall.Termios
	if err := ioctl(fd, ioctlGetTermios, uintptr(unsafe.Pointer(&t))); err != nil {
		return fail("get attributes of", err)
	}
	makeRaw(&t)
	setSpeed(&t, speed)
	if err := ioctl(fd, ioctlSetTermios, uintptr(unsafe.Pointer(&t))); err != nil {
		return fail("configure", err)
	}
	// Back to blocking; reads are bounded by select below.
	if err := syscall.SetNonblock(fd, false); err != nil {
		return fail("set blocking mode on", err)
	}
	flush(fd)
	return &SerialPort{fd: fd, path: path}, nil
}

// newFDPort wraps an already-open descriptor (a pseudo-terminal's
// controlling side) with the same select-based reader.
func newFDPort(fd int, path string) *SerialPort {
	return &SerialPort{fd: fd, path: path}
}

// makeRaw is cfmakeraw: no echo, no line editing, no translation, 8N1,
// no flow control, receiver on, modem lines ignored.
func makeRaw(t *syscall.Termios) {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | crtscts
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
}

// Path returns the device path.
func (p *SerialPort) Path() string { return p.path }

// Read blocks until data arrives, the port closes, or the device goes away.
func (p *SerialPort) Read(b []byte) (int, error) {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	for {
		if p.closed.Load() {
			return 0, io.EOF
		}
		ready, err := selectRead(p.fd, 100*time.Millisecond)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return 0, fmt.Errorf("read %s: %w", p.path, err)
		}
		if !ready {
			continue
		}
		n, err := syscall.Read(p.fd, b)
		switch {
		case errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN):
			continue
		case err != nil:
			// ENXIO or EIO: the USB device has gone.
			return 0, fmt.Errorf("read %s: %w", p.path, err)
		case n == 0:
			// Readable but empty: hang-up.
			return 0, io.EOF
		}
		return n, nil
	}
}

// Write writes all of b.
func (p *SerialPort) Write(b []byte) (int, error) {
	p.wrMu.Lock()
	defer p.wrMu.Unlock()
	if p.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	total := 0
	for total < len(b) {
		n, err := syscall.Write(p.fd, b[total:])
		if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
			continue
		}
		if err != nil {
			return total, fmt.Errorf("write %s: %w", p.path, err)
		}
		total += n
	}
	return total, nil
}

// Close releases the exclusive lock and closes the port. It waits for an
// in-progress Read to notice (at most about 100 ms) so the descriptor is
// never closed under it.
func (p *SerialPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	p.readMu.Lock()
	p.wrMu.Lock()
	defer p.readMu.Unlock()
	defer p.wrMu.Unlock()
	ioctl(p.fd, syscall.TIOCNXCL, 0)
	return syscall.Close(p.fd)
}

func ioctl(fd int, req uint, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), arg)
	if errno != 0 {
		return errno
	}
	return nil
}
