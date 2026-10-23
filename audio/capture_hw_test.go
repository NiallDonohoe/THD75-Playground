//go:build darwin && cgo && hardware

package audio

import (
	"os"
	"testing"

	"d75scope/dsp"
	"d75scope/ifsource"
)

// TestCaptureRadio captures one second from the radio (or from the device
// named by D75_AUDIO_DEVICE). Run with: go test -tags hardware ./audio
func TestCaptureRadio(t *testing.T) {
	p := New()
	devs, err := p.Devices()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if name := os.Getenv("D75_AUDIO_DEVICE"); name != "" {
		for _, d := range devs {
			if d.Name == name || d.ID == name {
				id = d.ID
			}
		}
	} else if d, _, err := ifsource.PickRadioDevice(devs); err == nil {
		id = d.ID
	}
	if id == "" {
		t.Skip("radio audio device not found")
	}
	src, err := p.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	f := src.Format()
	buf := make([]float32, int(f.SampleRate)*f.Channels)
	if _, err := src.Read(buf); err != nil {
		t.Fatal(err)
	}
	for c, l := range dsp.Levels(buf, f.Channels) {
		t.Logf("channel %d: %+v", c, l)
	}
}
