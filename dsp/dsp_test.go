package dsp

import (
	"math"
	"math/cmplx"
	"path/filepath"
	"testing"
)

func TestFFTMatchesDFT(t *testing.T) {
	const n = 64
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Sin(float64(i)*0.3)+0.1*float64(i%5), math.Cos(float64(i)*1.7))
	}
	want := make([]complex128, n)
	for k := range want {
		for i, v := range x {
			want[k] += v * cmplx.Rect(1, -2*math.Pi*float64(k*i)/n)
		}
	}
	f, err := NewFFT(n)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]complex128(nil), x...)
	f.Transform(got)
	for k := range got {
		if cmplx.Abs(got[k]-want[k]) > 1e-9 {
			t.Fatalf("bin %d: %v, want %v", k, got[k], want[k])
		}
	}
	if _, err := NewFFT(100); err == nil {
		t.Error("non-power-of-two accepted")
	}
}

func TestWindow(t *testing.T) {
	w := BlackmanHarris4(4096)
	if e := ENBW(w); math.Abs(e-2.0044) > 0.001 {
		t.Errorf("ENBW = %v, want 2.0044", e)
	}
}

func TestOscillatorContinuousAcrossBlocks(t *testing.T) {
	const fs = 48000
	one := NewOscillator(1234.5, fs)
	whole := make([]complex128, 1000)
	one.Fill(whole)
	two := NewOscillator(1234.5, fs)
	a, b := make([]complex128, 333), make([]complex128, 667)
	two.Fill(a)
	two.Fill(b)
	for i, v := range append(a, b...) {
		if cmplx.Abs(v-whole[i]) > 1e-12 {
			t.Fatalf("sample %d differs across a block boundary", i)
		}
	}
	// A frequency change keeps the phase: no jump between samples.
	o := NewOscillator(1000, fs)
	prev := o.Next()
	o.SetFrequency(3000)
	next := o.Next()
	// The step between them is the old step (phase advanced before the change).
	if d := math.Abs(cmplx.Phase(next/prev) - 2*math.Pi*1000/fs); d > 1e-9 {
		t.Errorf("phase jump %v rad at a frequency change", d)
	}
	// Long runs stay accurate (phase wrap).
	long := NewOscillator(12000, fs)
	for i := 0; i < 10*fs; i++ {
		long.Next()
	}
	if v := long.Next(); cmplx.Abs(v-1) > 1e-6 {
		t.Errorf("after 10 s at fs/4, phase drifted: %v", v)
	}
}

func TestRealSpectrumLevelAndFrequency(t *testing.T) {
	const fs, n = 48000.0, 8192
	x := make([]float64, 48000)
	for i := range x {
		x[i] = 0.5 * math.Cos(2*math.Pi*1003.7*float64(i)/fs)
	}
	s := RealSpectrum(x, fs, n)
	peaks := FindPeaks(s, 100, 23000, 20, 3)
	if len(peaks) != 1 {
		t.Fatalf("peaks = %+v", peaks)
	}
	if math.Abs(peaks[0].FreqHz-1003.7) > 0.5 {
		t.Errorf("peak at %v Hz, want 1003.7", peaks[0].FreqHz)
	}
	if math.Abs(peaks[0].LevelDB-(-6.02)) > 0.3 {
		t.Errorf("peak level %v dB, want −6.02", peaks[0].LevelDB)
	}
}

func TestLowpassFIRMeetsAntiAliasSpec(t *testing.T) {
	// The spec's anti-alias filter at 48 kHz: pass ±7.5 kHz with ≤0.1 dB
	// ripple, reject ≥80 dB from 16.5 kHz.
	const fs = 48000.0
	trans := (16500 - 7500) / fs
	taps := KaiserTaps(85, trans)
	h := LowpassFIR(taps, 12000/fs, KaiserBeta(85))
	for f := 0.0; f <= 7500; f += 50 {
		if g := FrequencyResponse(h, f/fs); math.Abs(g) > 0.1 {
			t.Fatalf("passband %v Hz: %.3f dB", f, g)
		}
	}
	for f := 16500.0; f <= 24000; f += 50 {
		if g := FrequencyResponse(h, f/fs); g > -80 {
			t.Fatalf("stopband %v Hz: %.1f dB", f, g)
		}
	}
	t.Logf("%d taps", taps)
}

func TestSynthPlacesSignals(t *testing.T) {
	const fs = 48000.0
	cases := []struct {
		name   string
		invert bool
		want   float64
	}{{"normal", false, 13500}, {"inverted", true, 10500}}
	for _, c := range cases {
		s := NewSynth(SynthConfig{SampleRate: fs, Invert: c.invert, Signals: []Signal{{Kind: KindCarrier, FreqHz: 1500, LevelDBFS: -20}}})
		x := make([]float32, 48000)
		s.Generate(x)
		sp := RealSpectrum(Channel(x, 1, 0), fs, 8192)
		p := FindPeaks(sp, 4500, 19500, 20, 1)
		if len(p) != 1 || math.Abs(p[0].FreqHz-c.want) > 0.5 || math.Abs(p[0].LevelDB+20) > 0.3 {
			t.Errorf("%s: peaks %+v, want one at %v Hz, −20 dB", c.name, p, c.want)
		}
	}
}

func TestSynthMirrorClipAndOffset(t *testing.T) {
	const fs = 48000.0
	s := NewSynth(SynthConfig{SampleRate: fs, MirrorDB: -40, FreqOffsetHz: 100,
		Signals: []Signal{{Kind: KindCarrier, FreqHz: 2000, LevelDBFS: -10}}})
	x := make([]float32, 48000)
	s.Generate(x)
	sp := RealSpectrum(Channel(x, 1, 0), fs, 8192)
	main := sp.DB[sp.BinOf(14100)]
	mirror := sp.DB[sp.BinOf(9900)]
	if d := main - mirror; math.Abs(d-40) > 1 {
		t.Errorf("mirror %.1f dB below, want 40", d)
	}
	clip := NewSynth(SynthConfig{SampleRate: fs, ClipLevel: 0.5, Signals: []Signal{{Kind: KindCarrier, FreqHz: 0, LevelDBFS: 0}}})
	clip.Generate(x)
	if l := Levels(x, 1)[0]; math.Abs(l.PeakDBFS-AmplitudeDB(0.5)) > 1e-6 {
		t.Errorf("clipped peak %v dBFS", l.PeakDBFS)
	}
}

func TestSynthNoiseLevelAndBandwidth(t *testing.T) {
	const fs = 48000.0
	s := NewSynth(SynthConfig{SampleRate: fs, Seed: 1, Signals: []Signal{{Kind: KindNoise, LevelDBFS: -30, BandwidthHz: 15000}}})
	x := make([]float32, 4*48000)
	s.Generate(x)
	if l := Levels(x, 1)[0]; math.Abs(l.RMSDBFS+30) > 0.5 {
		t.Errorf("noise RMS %v dBFS, want −30", l.RMSDBFS)
	}
	sp := RealSpectrum(Channel(x, 1, 0), fs, 4096)
	in, out := sp.DB[sp.BinOf(12000)], sp.DB[sp.BinOf(22000)]
	if in-out < 60 {
		t.Errorf("noise only %.1f dB down outside the band", in-out)
	}
}

func TestSynthTwoToneAndAM(t *testing.T) {
	const fs = 48000.0
	s := NewSynth(SynthConfig{SampleRate: fs, Signals: []Signal{
		{Kind: KindTwoTone, FreqHz: -5000, LevelDBFS: -20},
		{Kind: KindAM, FreqHz: 3000, LevelDBFS: -20, ModHz: 1000, ModDepth: 0.5},
	}})
	x := make([]float32, 48000)
	s.Generate(x)
	sp := RealSpectrum(Channel(x, 1, 0), fs, 8192)
	got := map[int]bool{}
	for _, p := range FindPeaks(sp, 4500, 19500, 20, 10) {
		got[int(math.Round(p.FreqHz))] = true
	}
	for _, f := range []int{7700, 8900, 14000, 15000, 16000} {
		if !got[f] {
			t.Errorf("no peak at %d Hz; got %v", f, got)
		}
	}
}

func TestMorseKeying(t *testing.T) {
	const fs = 48000.0
	s := NewSynth(SynthConfig{SampleRate: fs, Signals: []Signal{{Kind: KindMorse, FreqHz: 0, LevelDBFS: -6, Text: "E", WPM: 20}}})
	x := make([]float32, int(fs)) // "E": 1 dit on, 7 off (word gap) = 8 dits = 0.48 s
	s.Generate(x)
	on := 0
	dit := int(1.2 / 20 * fs)
	for i := 0; i < 8*dit; i += 4 {
		if math.Abs(float64(x[i])) > 0.01 {
			on++
		}
	}
	frac := float64(on) / float64(8*dit/4)
	if frac < 0.09 || frac > 0.14 {
		t.Errorf("key-down fraction %.3f, want ≈ 1/8 less the tone's zero crossings", frac)
	}
}

func TestLevelsAndChannels(t *testing.T) {
	x := []float32{0.5, 0.5, -0.25, -0.25, 0, 0, 1, 1}
	l := Levels(x, 2)
	if math.Abs(l[0].PeakDBFS) > 1e-9 || l[0].ClippedCount != 1 || l[0].ZeroFraction != 0.25 {
		t.Errorf("levels = %+v", l[0])
	}
	if c := CompareChannels(x, 2, 0, 1); !c.Identical || math.Abs(c.Correlation-1) > 1e-9 {
		t.Errorf("identical channels: %+v", c)
	}
	// I and Q of a complex tone: different, uncorrelated.
	iq := make([]float32, 2*4800)
	for i := 0; i < 4800; i++ {
		iq[2*i] = float32(math.Cos(2 * math.Pi * 1000 * float64(i) / 48000))
		iq[2*i+1] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 48000))
	}
	if c := CompareChannels(iq, 2, 0, 1); c.Identical || math.Abs(c.Correlation) > 0.01 {
		t.Errorf("I/Q channels: %+v", c)
	}
	if l := Levels(nil, 1); l[0].ZeroFraction != 1 {
		t.Errorf("empty levels = %+v", l)
	}
}

func TestEstimateShift(t *testing.T) {
	const fs = 48000.0
	gen := func(shift float64) Spectrum {
		s := NewSynth(SynthConfig{SampleRate: fs, Seed: 7, Signals: []Signal{
			{Kind: KindNoise, LevelDBFS: -40},
			{Kind: KindCarrier, FreqHz: 1000 + shift, LevelDBFS: -20},
			{Kind: KindCarrier, FreqHz: -3210 + shift, LevelDBFS: -30},
			{Kind: KindTwoTone, FreqHz: 4000 + shift, LevelDBFS: -35},
		}})
		x := make([]float32, 2*48000)
		s.Generate(x)
		return RealSpectrum(Channel(x, 1, 0), fs, 8192)
	}
	base := gen(0)
	for _, want := range []float64{-1000, 1000} {
		got, score, energy := EstimateShift(base, gen(want), 4500, 19500, 2000)
		if math.Abs(got-want) > 2 || score < 0.8 || energy <= 0 {
			t.Errorf("shift %v: got %.2f Hz, score %.2f, energy %.0f", want, got, score, energy)
		}
	}
	// Noise alone: no excess to correlate.
	noise := func(seed uint64) Spectrum {
		s := NewSynth(SynthConfig{SampleRate: fs, Seed: seed, Signals: []Signal{{Kind: KindNoise, LevelDBFS: -40}}})
		x := make([]float32, 2*48000)
		s.Generate(x)
		return RealSpectrum(Channel(x, 1, 0), fs, 8192)
	}
	if _, _, energy := EstimateShift(noise(1), noise(2), 4500, 19500, 2000); energy > 50 {
		t.Errorf("noise-only excess energy %.0f; should be near zero", energy)
	}
}

func TestInterpolatePeak(t *testing.T) {
	// A parabola with its vertex at 10.3.
	db := make([]float64, 20)
	for i := range db {
		d := float64(i) - 10.3
		db[i] = -d * d
	}
	d, level := InterpolatePeak(db, 10)
	if math.Abs(d-0.3) > 1e-9 || math.Abs(level) > 1e-9 {
		t.Errorf("delta %v level %v", d, level)
	}
}

func TestWAVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	x := []float32{0, 0.5, -0.5, 0.25, -1, 0.999, 0.1, -0.1}
	for _, f := range []WAVFormat{
		{SampleRate: 48000, Channels: 2},
		{SampleRate: 44100, Channels: 1, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 2, BitsPerSample: 24},
	} {
		path := filepath.Join(dir, "t.wav")
		w, err := CreateWAV(path, f)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(x[:4])
		w.Write(x[4:])
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		got, gf, err := ReadWAVFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if gf.SampleRate != f.SampleRate || gf.Channels != f.Channels || len(got) != len(x) {
			t.Fatalf("format %+v len %d", gf, len(got))
		}
		tol := 1e-7
		if f.BitsPerSample == 16 {
			tol = 1.0 / 16000
		} else if f.BitsPerSample == 24 {
			tol = 1.0 / 4000000
		}
		for i := range x {
			if math.Abs(float64(got[i]-x[i])) > tol {
				t.Errorf("%+v sample %d: %v, want %v", f, i, got[i], x[i])
			}
		}
		r, _ := OpenWAV(path)
		if err := r.SeekFrame(2); err != nil {
			t.Fatal(err)
		}
		buf := make([]float32, 1)
		r.Read(buf)
		if math.Abs(float64(buf[0]-x[2*f.Channels])) > tol {
			t.Errorf("%+v seek: %v", f, buf[0])
		}
		r.Close()
	}
	if _, err := CreateWAV(filepath.Join(dir, "bad.wav"), WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 8}); err == nil {
		t.Error("8-bit accepted")
	}
}
