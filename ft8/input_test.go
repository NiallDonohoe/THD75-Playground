package ft8

import (
	"math"
	"testing"
)

func TestTo12kKeepsBandAndRejectsAliases(t *testing.T) {
	const rate = 48000
	tone := func(hz float64) []float32 {
		x := make([]float32, 2*rate) // stereo, one second
		for i := 0; i < rate; i++ {
			x[2*i] = float32(math.Sin(2 * math.Pi * hz * float64(i) / rate))
			x[2*i+1] = 0.9 // the other channel is ignored
		}
		return x
	}
	rms := func(x []float32) float64 {
		var s float64
		for _, v := range x[1000 : len(x)-1000] {
			s += float64(v) * float64(v)
		}
		return math.Sqrt(s / float64(len(x)-2000))
	}
	for _, c := range []struct {
		hz       float64
		min, max float64 // RMS, of 0.707 for a full-scale sine
	}{
		{1500, 0.70, 0.71},
		{3000, 0.70, 0.71},
		{9000, 0, 0.0003}, // would alias to 3 kHz
	} {
		out, err := To12k(tone(c.hz), rate, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != SampleRate {
			t.Fatalf("%d samples, want %d", len(out), SampleRate)
		}
		if r := rms(out); r < c.min || r > c.max {
			t.Errorf("%.0f Hz: RMS %.4f, want %.4f–%.4f", c.hz, r, c.min, c.max)
		}
	}
	if _, err := To12k(nil, 44100, 1); err == nil {
		t.Error("44.1 kHz accepted")
	}
}
