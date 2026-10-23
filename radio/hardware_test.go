//go:build hardware

package radio

import (
	"context"
	"testing"
)

// TestHardwareIdentify reads ID and FV from the real radio (read-only).
// Run with: go test -tags hardware -run Hardware -v ./radio
func TestHardwareIdentify(t *testing.T) {
	ports, err := ListPorts()
	if err != nil {
		t.Fatal(err)
	}
	p, m, err := PickRadioPort(ports)
	if err != nil {
		t.Skip(err)
	}
	t.Logf("port %s (%v)", p.Path, m)
	sp, err := OpenSerial(p.Path, 115200)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCAT(sp, SessionConfig{})
	defer c.Close()
	id, err := c.Identify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("identity %+v", id)
}
