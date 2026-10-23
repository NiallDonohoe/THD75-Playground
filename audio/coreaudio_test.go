//go:build darwin && cgo

package audio

import "testing"

// Listing devices needs no microphone permission and no radio.
func TestListDevices(t *testing.T) {
	devs, err := New().Devices()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		if d.ID == "" {
			t.Errorf("device %q has no UID", d.Name)
		}
		t.Logf("%-40q in=%d out=%d rate=%v transport=%s formats=%+v", d.Name, d.InputChannels, d.OutputChannels, d.NominalRate, d.Transport, d.InputFormats)
	}
}
