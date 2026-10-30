package ft8

import (
	"bufio"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"d75scope/dsp"
)

// The generator and the parity checks are separate tables: every codeword
// the generator makes must pass every check.
func TestLDPCTablesAgree(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 200; trial++ {
		var a [ldpcK]uint8
		for i := range a {
			a[i] = uint8(rng.IntN(2))
		}
		if trial < ldpcK { // and each unit vector, so every column is covered
			a = [ldpcK]uint8{}
			a[trial] = 1
		}
		c := ldpcEncode(&a)
		if e := parityErrors(&c); e != 0 {
			t.Fatalf("trial %d: codeword fails %d parity checks", trial, e)
		}
	}
}

func TestBPCorrectsErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	p, _ := Pack("CQ K1ABC FN42")
	pl := [payloadBits]uint8(p)
	a := addCRC(&pl)
	c := ldpcEncode(&a)
	var llr [ldpcN]float64
	for i, b := range c {
		// BPSK-like soft values with six confident errors.
		v := 3.0 + rng.NormFloat64()
		if b == 1 {
			v = -v
		}
		if i%29 == 0 {
			v = -v
		}
		llr[i] = v
	}
	got, errs := bpDecode(&llr, 30)
	if errs != 0 || got != c {
		t.Fatalf("BP did not recover the codeword (%d checks failing)", errs)
	}
}

func TestCRC(t *testing.T) {
	p, _ := Pack("K1ABC W9XYZ -11")
	pl := [payloadBits]uint8(p)
	a := addCRC(&pl)
	if _, ok := checkCRC(&a); !ok {
		t.Fatal("fresh CRC does not check")
	}
	for i := range a {
		b := a
		b[i] ^= 1
		if _, ok := checkCRC(&b); ok {
			t.Fatalf("single bit error at %d not detected", i)
		}
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	calls := NewCallsigns()
	for _, msg := range []string{
		"CQ K1ABC FN42",
		"CQ DX K1ABC FN42",
		"CQ POTA W9XYZ EN37",
		"CQ 290 K1ABC FN42",
		"QRZ K1ABC FN42",
		"K1ABC W9XYZ EN37",
		"W9XYZ K1ABC -11",
		"K1ABC W9XYZ R-09",
		"W9XYZ K1ABC +05",
		"K1ABC W9XYZ R EN37",
		"W9XYZ K1ABC RRR",
		"K1ABC W9XYZ RR73",
		"W9XYZ K1ABC 73",
		"K1ABC W9XYZ",
		"EI2ABC G4XYZ IO63",
		"CQ 9A1AA JN75",
		"CQ 3DA0XYZ KG53",
		"CQ 3XY1AB IJ19",
		"K1ABC/R W9XYZ EN37",
		"G4ABC/P EI7XYZ IO63",
		"CQ PJ4/K1ABC",
		"<W9XYZ> PJ4/K1ABC RRR",
		"PJ4/K1ABC <W9XYZ> 73",
		"<PJ4/K1ABC> W9XYZ -11",
		"TNX BOB 73 GL",
		"HELLO WORLD",
		"0123456789+-.",
	} {
		p, err := Pack(msg)
		if err != nil {
			t.Errorf("%q: %v", msg, err)
			continue
		}
		// The hashed calls must already be known for <...> to resolve.
		calls.Add("W9XYZ")
		calls.Add("PJ4/K1ABC")
		got, err := unpack(&p, calls)
		if err != nil || got != msg {
			t.Errorf("%q came back as %q (%v)", msg, got, err)
		}
	}
}

func TestPackRejects(t *testing.T) {
	for _, msg := range []string{"", "THIS IS FAR TOO LONG FOR FREE TEXT", "K1ABC W9XYZ R RRR", "hello_world"} {
		if _, err := Pack(msg); err == nil {
			t.Errorf("%q packed", msg)
		}
	}
}

// Payload types we unpack but never pack, built by hand.
func TestUnpackOtherTypes(t *testing.T) {
	var p Payload
	k1abc, _ := pack28("K1ABC")
	w9xyz, _ := pack28("W9XYZ")
	p.put(0, 28, uint64(k1abc))
	p.put(28, 28, uint64(w9xyz))
	p.put(56, 10, uint64(callHash("KH1/KH7Z", 10)))
	p.put(66, 5, 11) // 2×11 − 30 = −8
	p.put(71, 3, 1)
	calls := NewCallsigns()
	calls.Add("KH1/KH7Z")
	if got, err := unpack(&p, calls); err != nil || got != "K1ABC RR73; W9XYZ <KH1/KH7Z> -08" {
		t.Errorf("DXpedition: %q, %v", got, err)
	}

	p = Payload{}
	p.put(7, 64, 0x123456789ABCDEF0)
	p.put(71, 3, 5)
	if got, _ := unpack(&p, nil); got != "123456789ABCDEF0" {
		t.Errorf("telemetry: %q", got)
	}

	p = Payload{}
	p.put(74, 3, 3)
	if _, err := unpack(&p, nil); err != ErrUnsupported {
		t.Errorf("RTTY Roundup: %v, want ErrUnsupported", err)
	}
}

func TestTonesHaveCostas(t *testing.T) {
	tones, err := Encode("CQ K1ABC FN42")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range costasStart {
		for k, c := range costas {
			if tones[s+k] != c {
				t.Fatalf("symbol %d is %d, want Costas %d", s+k, tones[s+k], c)
			}
		}
	}
}

// slot builds 15 s of audio: white noise plus signals.
type signal struct {
	text string
	freq float64
	dt   float64
	snr  float64 // dB in 2500 Hz
	// overlapped signals share their noise tones, so their SNR reads low
	overlapped bool
}

func slot(t *testing.T, seed uint64, sigs []signal) []float32 {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 99))
	x := make([]float32, SlotSamples)
	// Noise of unit power across 6 kHz: 2500 Hz holds 2500/6000 of it.
	for i := range x {
		x[i] = float32(rng.NormFloat64())
	}
	for _, s := range sigs {
		tones, err := Encode(s.text)
		if err != nil {
			t.Fatal(err)
		}
		power := 2500.0 / 6000 * math.Pow(10, s.snr/10)
		w := Synthesize(tones, s.freq, math.Sqrt(2*power))
		off := StartSamples + int(s.dt*SampleRate)
		for i, v := range w {
			if j := off + i; j >= 0 && j < len(x) {
				x[j] += v
			}
		}
	}
	return x
}

func TestDecodeSynthetic(t *testing.T) {
	sigs := []signal{
		{"CQ K1ABC FN42", 600, 0.1, -5, false},
		{"W9XYZ K1ABC -11", 1210.5, -0.3, -12, false},
		{"K1ABC W9XYZ R EN37", 1500, 0.8, -16, false},
		{"CQ DX EI2ABC IO63", 2200.3, 1.5, -18, false},
		{"TNX BOB 73 GL", 350, 0, 0, false},
		// Two signals on top of each other: the weaker one needs the
		// stronger subtracted first.
		{"G4XYZ EI7ABC RR73", 1800, 0.2, 0, true},
		{"CQ 9A1AA JN75", 1812.5, -0.1, -10, true},
	}
	got := (&Decoder{}).Decode(slot(t, 7, sigs))
	byText := map[string]Decode{}
	for _, d := range got {
		byText[d.Text] = d
	}
	for _, s := range sigs {
		d, ok := byText[s.text]
		if !ok {
			t.Errorf("missed %q", s.text)
			continue
		}
		snrOff := math.Abs(float64(d.SNR)-s.snr) > 3 && !s.overlapped
		if math.Abs(d.Freq-s.freq) > 1 || math.Abs(d.DT-s.dt) > 0.04 || snrOff {
			t.Errorf("%q: freq %.1f dt %.2f snr %d, want %.1f %.2f %.0f", s.text, d.Freq, d.DT, d.SNR, s.freq, s.dt, s.snr)
		}
	}
	if len(got) != len(sigs) {
		t.Errorf("%d decodes, want %d: %+v", len(got), len(sigs), got)
	}
}

func TestDecodeNoiseOnly(t *testing.T) {
	if got := (&Decoder{}).Decode(slot(t, 11, nil)); len(got) != 0 {
		t.Errorf("decoded from noise: %+v", got)
	}
}

// Recordings with WSJT-X's decodes alongside, from ft8_lib's test set.
// They are not in the repository; set FT8_REFERENCE_DIR to a folder of
// .wav files, each with a .txt of expected decodes in WSJT-X's ALL.TXT
// short form ("HHMMSS SNR DT FREQ ~ MESSAGE").
func TestDecodeReferenceRecordings(t *testing.T) {
	dir := os.Getenv("FT8_REFERENCE_DIR")
	if dir == "" {
		dir = filepath.Join("..", "testdata", "ft8")
	}
	wavs, _ := filepath.Glob(filepath.Join(dir, "*.wav"))
	if len(wavs) == 0 {
		t.Skip("no reference recordings; set FT8_REFERENCE_DIR")
	}
	var want, found int
	for _, path := range wavs {
		audio, format, err := dsp.ReadWAVFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if format.SampleRate != SampleRate || format.Channels != 1 {
			t.Fatalf("%s: %+v, want 12 kHz mono", path, format)
		}
		expected := readExpected(t, strings.TrimSuffix(path, ".wav")+".txt")
		got := map[string]bool{}
		for _, d := range (&Decoder{}).Decode(audio) {
			got[d.Text] = true
		}
		var missed []string
		for _, e := range expected {
			if got[e] {
				found++
			} else {
				missed = append(missed, e)
			}
		}
		want += len(expected)
		t.Logf("%s: %d of %d; missed %q", filepath.Base(path), len(expected)-len(missed), len(expected), missed)
	}
	t.Logf("total: %d of %d WSJT-X decodes (%.0f%%)", found, want, 100*float64(found)/float64(want))
	if found*10 < want*7 {
		t.Errorf("decoded %d of %d, want at least 70%%", found, want)
	}
}

func readExpected(t *testing.T, path string) []string {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if _, msg, ok := strings.Cut(sc.Text(), "~"); ok {
			// WSJT-X appends a country after a run of spaces.
			msg, _, _ = strings.Cut(strings.TrimSpace(msg), "  ")
			out = append(out, msg)
		}
	}
	return out
}
