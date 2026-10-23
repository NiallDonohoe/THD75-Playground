package ifsource

import (
	"math"
	"testing"

	"d75scope/dsp"
)

func TestSyntheticFollowsTuning(t *testing.T) {
	state := TuneState{DialHz: 7_074_000, Mode: "USB", Output: "IF"}
	p := &SyntheticProvider{
		Channels: 2,
		Scene: Scene{
			Carriers: []RFCarrier{{FreqHz: 7_075_500, LevelDBFS: -20}},
			Inverted: map[string]bool{"LSB": true},
			DeltaHz:  map[string]float64{"USB": 0, "LSB": 0},
		},
		State: func() TuneState { return state },
	}
	devs, _ := p.Devices()
	d, why, err := PickRadioDevice(devs)
	if err != nil || d.ID != SyntheticRadioID {
		t.Fatalf("pick = %+v %q %v", d, why, err)
	}
	src, err := p.Open(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	peakAt := func() float64 {
		buf := make([]float32, 2*48000)
		src.Read(buf)
		if c := dsp.CompareChannels(buf, 2, 0, 1); !c.Identical {
			t.Fatal("channels differ")
		}
		sp := dsp.RealSpectrum(dsp.Channel(buf, 2, 0), 48000, 8192)
		pk := dsp.FindPeaks(sp, 4500, 19500, 20, 1)
		if len(pk) == 0 {
			return 0
		}
		return pk[0].FreqHz
	}
	if f := peakAt(); math.Abs(f-13500) > 1 {
		t.Errorf("USB: carrier at %v Hz in the IF, want 13500", f)
	}
	state.DialHz += 1000
	if f := peakAt(); math.Abs(f-12500) > 1 {
		t.Errorf("USB +1 kHz: %v Hz, want 12500", f)
	}
	state.Mode = "LSB"
	if f := peakAt(); math.Abs(f-11500) > 1 {
		t.Errorf("LSB (inverted): %v Hz, want 11500", f)
	}
	state.Output = "AF"
	buf := make([]float32, 2*4800)
	src.Read(buf)
	if l := dsp.Levels(buf, 2)[0]; l.PeakDBFS > -150 {
		t.Errorf("AF output not silent: %v dBFS", l.PeakDBFS)
	}
}

func TestPickRadioDeviceNeverPicksDefault(t *testing.T) {
	devs := []Device{{ID: "mic", Name: "MacBook Air Microphone", InputChannels: 1, DefaultInput: true}}
	if _, _, err := PickRadioDevice(devs); err != ErrNoRadioDevice {
		t.Errorf("err = %v", err)
	}
	devs = append(devs, Device{ID: "k", Name: "USB Audio", Manufacturer: "JVC KENWOOD", InputChannels: 1})
	if d, _, err := PickRadioDevice(devs); err != nil || d.ID != "k" {
		t.Errorf("pick = %+v %v", d, err)
	}
	// An output-only device with the right name is not an input.
	if _, _, err := PickRadioDevice([]Device{{ID: "o", Name: "TH-D75", OutputChannels: 2}}); err == nil {
		t.Error("picked an output-only device")
	}
}
