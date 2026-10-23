//go:build darwin || linux

package radio

import "fmt"

// ServePTY puts the fake behind a pseudo-terminal and returns the device
// path a client opens with OpenSerial, so the real serial code is tested end
// to end. Unplug closes the controlling side, which the client sees as the
// device going away.
func (f *FakeD75) ServePTY() (string, error) {
	fd, path, err := openPTY()
	if err != nil {
		return "", fmt.Errorf("fake radio pty: %w", err)
	}
	// Hand the descriptor to a select-based port so Close really closes it
	// even while a read is waiting.
	f.attach(newFDPort(fd, "/dev/ptmx"))
	return path, nil
}
