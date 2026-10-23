package dsp

import (
	"math"
	"math/rand/v2"
	"strings"
)

// SignalKind selects a synthetic signal.
type SignalKind string

const (
	KindCarrier SignalKind = "carrier"  // unmodulated carrier (CW key down)
	KindMorse   SignalKind = "morse"    // keyed carrier sending Text
	KindTwoTone SignalKind = "two-tone" // USB two-tone test signal
	KindAM      SignalKind = "am"       // AM carrier with a modulating tone
	KindNoise   SignalKind = "noise"    // band-limited noise
)

// Signal is one synthetic signal in the IF. FreqHz is its position relative
// to the IF centre as the radio would put it before any spectral inversion:
// the carrier for a carrier, Morse or AM signal, the suppressed carrier for
// two-tone SSB, and the centre of the band for noise.
type Signal struct {
	Kind      SignalKind `json:"kind"`
	FreqHz    float64    `json:"freq_hz"`
	LevelDBFS float64    `json:"level_dbfs"` // peak amplitude (each tone for two-tone; RMS for noise)

	Text string  `json:"text,omitempty"` // Morse
	WPM  float64 `json:"wpm,omitempty"`  // Morse, default 20

	Tone1Hz float64 `json:"tone1_hz,omitempty"` // two-tone, default 700
	Tone2Hz float64 `json:"tone2_hz,omitempty"` // two-tone, default 1900

	ModHz    float64 `json:"mod_hz,omitempty"`    // AM, default 1000
	ModDepth float64 `json:"mod_depth,omitempty"` // AM, default 0.5

	BandwidthHz float64 `json:"bandwidth_hz,omitempty"` // noise, default 15000
}

// SynthConfig describes a synthetic real IF.
type SynthConfig struct {
	SampleRate float64
	IFCentreHz float64 // default 12000
	Signals    []Signal
	// Invert flips the spectrum about the IF centre (s = −1).
	Invert bool
	// FreqOffsetHz shifts every signal, as a radio's LO error would.
	FreqOffsetHz float64
	// MirrorDB, if non-zero, adds an image of everything at the mirror
	// position (−f about the IF centre) this many dB below it.
	MirrorDB float64
	// ClipLevel, if positive, clips the output to ±ClipLevel.
	ClipLevel float64
	Seed      uint64
}

// Synth generates a real IF sample stream. Phases are continuous across
// calls and across SetSignals, so it behaves like a live source.
type Synth struct {
	cfg    SynthConfig
	lo     *Oscillator
	states []*signalState
	rng    *rand.Rand
}

type signalState struct {
	sig      Signal
	osc      [2]*Oscillator
	mod      *Oscillator
	noise    *ComplexFIR
	noiseAmp float64
	morse    *morseKeyer
}

// NewSynth returns a generator.
func NewSynth(cfg SynthConfig) *Synth {
	if cfg.IFCentreHz == 0 {
		cfg.IFCentreHz = 12000
	}
	s := &Synth{cfg: cfg, lo: NewOscillator(cfg.IFCentreHz, cfg.SampleRate), rng: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))}
	s.SetSignals(cfg.Signals)
	return s
}

// SetSignals replaces the signal list, keeping each signal's phase when the
// signal at that index keeps its kind (so a retune is phase-continuous).
func (s *Synth) SetSignals(sigs []Signal) {
	states := make([]*signalState, len(sigs))
	for i, sig := range sigs {
		if i < len(s.states) && s.states[i].sig.Kind == sig.Kind {
			st := s.states[i]
			st.retune(sig, s.cfg)
			states[i] = st
			continue
		}
		states[i] = newSignalState(sig, s.cfg)
	}
	s.states = states
	s.cfg.Signals = sigs
}

// SetInvert changes the spectral inversion.
func (s *Synth) SetInvert(inv bool) { s.cfg.Invert = inv }

// SampleRate returns the output rate.
func (s *Synth) SampleRate() float64 { return s.cfg.SampleRate }

func withDefaults(sig Signal) Signal {
	if sig.WPM == 0 {
		sig.WPM = 20
	}
	if sig.Tone1Hz == 0 {
		sig.Tone1Hz = 700
	}
	if sig.Tone2Hz == 0 {
		sig.Tone2Hz = 1900
	}
	if sig.ModHz == 0 {
		sig.ModHz = 1000
	}
	if sig.ModDepth == 0 {
		sig.ModDepth = 0.5
	}
	if sig.BandwidthHz == 0 {
		sig.BandwidthHz = 15000
	}
	return sig
}

func newSignalState(sig Signal, cfg SynthConfig) *signalState {
	sig = withDefaults(sig)
	fs := cfg.SampleRate
	st := &signalState{sig: sig}
	st.osc[0] = NewOscillator(0, fs)
	st.osc[1] = NewOscillator(0, fs)
	st.mod = NewOscillator(sig.ModHz, fs)
	switch sig.Kind {
	case KindNoise:
		cut := math.Min(sig.BandwidthHz/2/fs, 0.45)
		taps := LowpassFIR(KaiserTaps(80, math.Max(0.01, cut*0.2)), cut, KaiserBeta(80))
		st.noise = NewComplexFIR(taps)
		sum2 := 0.0
		for _, t := range taps {
			sum2 += t * t
		}
		// Real-IF RMS r needs E|z|² = 2r²; white complex noise of variance
		// σ² comes out of the filter with σ²·Σh².
		r := math.Pow(10, sig.LevelDBFS/20)
		st.noiseAmp = math.Sqrt(2 * r * r / sum2)
	case KindMorse:
		st.morse = newMorseKeyer(sig.Text, sig.WPM, fs)
	}
	st.retune(sig, cfg)
	return st
}

func (st *signalState) retune(sig Signal, cfg SynthConfig) {
	sig = withDefaults(sig)
	if st.morse != nil && (sig.Text != st.sig.Text || sig.WPM != st.sig.WPM) {
		st.morse = newMorseKeyer(sig.Text, sig.WPM, cfg.SampleRate)
	}
	st.sig = sig
	f := sig.FreqHz + cfg.FreqOffsetHz
	switch sig.Kind {
	case KindTwoTone:
		st.osc[0].SetFrequency(f + sig.Tone1Hz)
		st.osc[1].SetFrequency(f + sig.Tone2Hz)
	default:
		st.osc[0].SetFrequency(f)
	}
	st.mod.SetFrequency(sig.ModHz)
}

func (st *signalState) next(rng *rand.Rand) complex128 {
	a := math.Pow(10, st.sig.LevelDBFS/20)
	switch st.sig.Kind {
	case KindCarrier:
		return complex(a, 0) * st.osc[0].Next()
	case KindMorse:
		return complex(a*st.morse.next(), 0) * st.osc[0].Next()
	case KindTwoTone:
		return complex(a, 0) * (st.osc[0].Next() + st.osc[1].Next())
	case KindAM:
		m := real(st.mod.Next())
		return complex(a*(1+st.sig.ModDepth*m)/(1+st.sig.ModDepth), 0) * st.osc[0].Next()
	case KindNoise:
		w := complex(rng.NormFloat64()*math.Sqrt2/2, rng.NormFloat64()*math.Sqrt2/2) * complex(st.noiseAmp, 0)
		return st.noise.Process(w) * st.osc[0].Next()
	}
	return 0
}

// Generate fills out with the next len(out) real IF samples.
func (s *Synth) Generate(out []float32) {
	g := math.Pow(10, s.cfg.MirrorDB/20)
	for i := range out {
		var z complex128
		for _, st := range s.states {
			z += st.next(s.rng)
		}
		if s.cfg.Invert {
			z = complexConj(z)
		}
		if s.cfg.MirrorDB != 0 {
			z += complex(g, 0) * complexConj(z)
		}
		lo := s.lo.Next()
		x := real(z)*real(lo) - imag(z)*imag(lo)
		if c := s.cfg.ClipLevel; c > 0 {
			x = math.Max(-c, math.Min(c, x))
		}
		out[i] = float32(x)
	}
}

func complexConj(z complex128) complex128 { return complex(real(z), -imag(z)) }

// morseKeyer turns text into a keying envelope with 5 ms raised-cosine edges.
type morseKeyer struct {
	elements []int // run lengths in dits, positive on, negative off
	ditLen   int   // samples per dit
	idx      int
	left     int
	env      float64
	rise     float64
}

var morseCode = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.", 'H': "....",
	'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.", 'O': "---", 'P': ".--.",
	'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..", '0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.", '/': "-..-.", '?': "..--..",
	'=': "-...-", '.': ".-.-.-", ',': "--..--",
}

func newMorseKeyer(text string, wpm, fs float64) *morseKeyer {
	if text == "" {
		text = "CQ TEST"
	}
	var el []int
	for _, word := range strings.Fields(strings.ToUpper(text)) {
		for _, r := range word {
			code, ok := morseCode[r]
			if !ok {
				continue
			}
			for _, sym := range code {
				if sym == '.' {
					el = append(el, 1, -1)
				} else {
					el = append(el, 3, -1)
				}
			}
			el[len(el)-1] = -3 // letter gap
		}
		el[len(el)-1] = -7 // word gap
	}
	k := &morseKeyer{elements: el, ditLen: int(1.2 / wpm * fs), rise: 1 / (0.005 * fs)}
	k.left = abs(el[0]) * k.ditLen
	return k
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (k *morseKeyer) next() float64 {
	if k.left == 0 {
		k.idx = (k.idx + 1) % len(k.elements)
		k.left = abs(k.elements[k.idx]) * k.ditLen
	}
	k.left--
	if k.elements[k.idx] > 0 {
		k.env = math.Min(1, k.env+k.rise)
	} else {
		k.env = math.Max(0, k.env-k.rise)
	}
	return 0.5 - 0.5*math.Cos(math.Pi*k.env)
}
