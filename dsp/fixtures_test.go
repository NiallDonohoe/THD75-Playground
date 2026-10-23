package dsp

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata fixtures")

type fixture struct {
	File        string      `json:"file"`
	Description string      `json:"description"`
	Config      SynthConfig `json:"config"`
	Seconds     float64     `json:"seconds"`
}

// fixtures are synthetic raw-IF recordings (48 kHz, mono, 16-bit) for later
// milestones' WAV-source and DSP tests, until real ones come from the probe.
var fixtures = []fixture{
	{File: "if-carrier-noise.wav", Description: "carrier 1.5 kHz above the IF centre at −20 dBFS in −60 dBFS noise", Seconds: 1,
		Config: SynthConfig{SampleRate: 48000, IFCentreHz: 12000, Seed: 1, Signals: []Signal{{Kind: KindNoise, LevelDBFS: -60}, {Kind: KindCarrier, FreqHz: 1500, LevelDBFS: -20}}}},
	{File: "if-inverted.wav", Description: "as if-carrier-noise but spectrally inverted (carrier 1.5 kHz below the centre)", Seconds: 1,
		Config: SynthConfig{SampleRate: 48000, IFCentreHz: 12000, Seed: 1, Invert: true, Signals: []Signal{{Kind: KindNoise, LevelDBFS: -60}, {Kind: KindCarrier, FreqHz: 1500, LevelDBFS: -20}}}},
	{File: "if-mix.wav", Description: "USB two-tone (suppressed carrier −3 kHz), AM with 1 kHz tone (+4 kHz), Morse 'CQ TEST' (+1 kHz), noise", Seconds: 1,
		Config: SynthConfig{SampleRate: 48000, IFCentreHz: 12000, Seed: 2, Signals: []Signal{
			{Kind: KindNoise, LevelDBFS: -60},
			{Kind: KindTwoTone, FreqHz: -3000, LevelDBFS: -30},
			{Kind: KindAM, FreqHz: 4000, LevelDBFS: -25, ModHz: 1000, ModDepth: 0.5},
			{Kind: KindMorse, FreqHz: 1000, LevelDBFS: -28, Text: "CQ TEST", WPM: 25},
		}}},
	{File: "if-mirror-clip.wav", Description: "carrier +2.5 kHz at −3 dBFS with a −40 dB mirror image, clipped at 0.6", Seconds: 1,
		Config: SynthConfig{SampleRate: 48000, IFCentreHz: 12000, Seed: 3, MirrorDB: -40, ClipLevel: 0.6, Signals: []Signal{{Kind: KindCarrier, FreqHz: 2500, LevelDBFS: -3}}}},
}

func generateFixture(f fixture) []float32 {
	x := make([]float32, int(f.Seconds*f.Config.SampleRate))
	NewSynth(f.Config).Generate(x)
	return x
}

func TestFixtures(t *testing.T) {
	dir := filepath.Join("..", "testdata")
	if *update {
		for _, f := range fixtures {
			w, err := CreateWAV(filepath.Join(dir, f.File), WAVFormat{SampleRate: int(f.Config.SampleRate), Channels: 1, BitsPerSample: 16})
			if err != nil {
				t.Fatal(err)
			}
			w.Write(generateFixture(f))
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
		}
		b, _ := json.MarshalIndent(fixtures, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "fixtures.json"), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fixtures {
		got, format, err := ReadWAVFile(filepath.Join(dir, f.File))
		if err != nil {
			t.Fatalf("%s: %v (run go test ./dsp -run Fixtures -update)", f.File, err)
		}
		want := generateFixture(f)
		if format.SampleRate != int(f.Config.SampleRate) || len(got) != len(want) {
			t.Fatalf("%s: format %+v, %d samples", f.File, format, len(got))
		}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 2.0/32768 {
				t.Fatalf("%s: sample %d is %v, generator gives %v", f.File, i, got[i], want[i])
			}
		}
	}
}
