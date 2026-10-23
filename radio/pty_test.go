//go:build darwin || linux

package radio

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSerialEndToEnd runs the real serial code against the fake radio
// behind a pseudo-terminal.
func TestSerialEndToEnd(t *testing.T) {
	fake := NewFakeD75()
	path, err := fake.ServePTY()
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	port, err := OpenSerial(path, 115200)
	if err != nil {
		t.Fatal(err)
	}
	// TIOCEXCL was accepted (OpenSerial fails otherwise). macOS
	// pseudo-terminals accept it but do not enforce it, so a second open
	// is only refused on real devices; the probe checks that on the radio.
	if p2, err := OpenSerial(path, 115200); err == nil {
		p2.Close()
		t.Log("pseudo-terminal does not enforce TIOCEXCL (expected on macOS)")
	}
	c := NewCAT(port, SessionConfig{})
	defer c.Close()
	ctx := context.Background()

	id, err := c.Identify(ctx)
	if err != nil || id.Model != "TH-D75" {
		t.Fatalf("Identify over pty: %+v, %v", id, err)
	}
	if got, err := c.SetFrequency(ctx, 14_074_000); err != nil || got != 14_074_000 {
		t.Errorf("SetFrequency over pty: %d, %v", got, err)
	}
	// NMEA over the pty holds writes.
	fake.Inject([]byte("$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47\r\n"))
	if ev := waitEvent(t, c.Events()); ev.Kind != EventNMEA {
		t.Fatalf("event = %+v", ev)
	}
	if _, err := c.SetIFOutput(ctx, IOIF); !errors.Is(err, ErrPortBusy) {
		t.Errorf("write with NMEA on the port: %v", err)
	}
	// Unplug mid-session: the reader sees the device go away.
	fake.Unplug()
	if ev := waitEvent(t, c.Events()); ev.Kind != EventClosed {
		t.Fatalf("event after unplug = %+v", ev)
	}
	if _, err := c.Frequency(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("after unplug: %v", err)
	}
	assertOnlyAllowed(t, fake)
}

func TestSerialCloseUnblocksRead(t *testing.T) {
	fake := NewFakeD75()
	path, err := fake.ServePTY()
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	port, err := OpenSerial(path, 9600)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := port.Read(make([]byte, 16))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	port.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Read still blocked after Close")
	}
	p2, err := OpenSerial(path, 9600)
	if err != nil {
		t.Errorf("reopen after close: %v", err)
	} else {
		p2.Close()
	}
}
