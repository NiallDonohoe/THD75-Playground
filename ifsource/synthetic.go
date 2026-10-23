package ifsource

import (
	"errors"
	"sync"
	"time"

	"d75scope/dsp"
)

// TuneState is what the synthetic radio is tuned to.
type TuneState struct {
	DialHz int64
	Mode   string // "USB", "LSB", "CW", "AM", "FM", ...
	Output string // "AF", "IF" or "Detect"
}

// RFCarrier is a signal on the air at an absolute frequency.
type RFCarrier struct {
	FreqHz    float64        `json:"freq_hz"`
	LevelDBFS float64        `json:"level_dbfs"`
	Kind      dsp.SignalKind `json:"kind"` // default carrier
	Text      string         `json:"text,omitempty"`
}

// Scene is the synthetic radio's hidden truth: the signals on the air and
// how this radio maps RF to IF in each mode,
//
//	f_RF = f_dial + Δ_mode + s·f_bb   so   f_bb = s·(f_RF − f_dial − Δ_mode)
//
// The probe and the calibration wizard must recover Inverted and DeltaHz.
type Scene struct {
	Carriers   []RFCarrier        `json:"carriers"`
	NoiseDBFS  float64            `json:"noise_dbfs"`
	Inverted   map[string]bool    `json:"inverted"` // s = −1 in this mode
	DeltaHz    map[string]float64 `json:"delta_hz"`
	IFCentreHz float64            `json:"if_centre_hz"`
	// IFModes lists modes in which IF output works; empty means all.
	IFModes []string `json:"if_modes,omitempty"`
}

// SyntheticProvider offers a pretend TH-D75 audio input (and a decoy
// built-in microphone) whose IF follows State().
type SyntheticProvider struct {
	Rate     float64 // default 48000
	Channels int     // default 1; 2 gives identical channels
	Scene    Scene
	State    func() TuneState
	Realtime bool // pace Read at the sample rate; otherwise as fast as asked
}

// SyntheticRadioID is the ID of the pretend radio input.
const SyntheticRadioID = "synthetic:TH-D75"

func (p *SyntheticProvider) defaults() (float64, int) {
	rate, ch := p.Rate, p.Channels
	if rate == 0 {
		rate = 48000
	}
	if ch == 0 {
		ch = 1
	}
	return rate, ch
}

// Devices lists the pretend radio and a decoy microphone.
func (p *SyntheticProvider) Devices() ([]Device, error) {
	rate, ch := p.defaults()
	return []Device{
		{ID: "synthetic:mic", Name: "MacBook Air Microphone", Manufacturer: "Apple Inc.", Transport: "builtin",
			InputChannels: 1, NominalRate: 48000, DefaultInput: true,
			InputFormats: []StreamFormat{{SampleRate: 48000, Channels: 1, BitsPerChannel: 24, FormatID: "lpcm"}}},
		{ID: SyntheticRadioID, Name: "TH-D75", Manufacturer: "JVC KENWOOD", ModelUID: "TH-D75:2166:600F", Transport: "usb",
			InputChannels: ch, NominalRate: rate, AvailableRates: []RateRange{{rate, rate}},
			InputFormats: []StreamFormat{{SampleRate: rate, Channels: ch, BitsPerChannel: 16, FormatID: "lpcm"}}},
	}, nil
}

// Open starts the pretend radio's IF.
func (p *SyntheticProvider) Open(id string) (Source, error) {
	if id != SyntheticRadioID {
		return nil, errors.New("synthetic provider only opens " + SyntheticRadioID)
	}
	rate, ch := p.defaults()
	centre := p.Scene.IFCentreHz
	if centre == 0 {
		centre = 12000
	}
	s := &syntheticSource{
		p: p, rate: rate, ch: ch, centre: centre,
		synth: dsp.NewSynth(dsp.SynthConfig{SampleRate: rate, IFCentreHz: centre, Seed: 1}),
		start: time.Now(),
	}
	return s, nil
}

type syntheticSource struct {
	p      *SyntheticProvider
	rate   float64
	ch     int
	centre float64
	synth  *dsp.Synth
	mono   []float32
	mu     sync.Mutex
	closed bool
	start  time.Time
	frames int64
}

func (s *syntheticSource) Format() Format { return Format{SampleRate: s.rate, Channels: s.ch} }

func (s *syntheticSource) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *syntheticSource) Flush() {
	s.start = time.Now()
	s.frames = 0
}

// block is how often the scene is re-evaluated against the radio state.
const block = 480

func (s *syntheticSource) Read(buf []float32) (int, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, errors.New("source closed")
	}
	frames := len(buf) / s.ch
	if cap(s.mono) < frames {
		s.mono = make([]float32, frames)
	}
	mono := s.mono[:frames]
	for off := 0; off < frames; off += block {
		end := min(frames, off+block)
		s.retune()
		s.synth.Generate(mono[off:end])
	}
	for i, v := range mono {
		for c := 0; c < s.ch; c++ {
			buf[i*s.ch+c] = v
		}
	}
	s.frames += int64(frames)
	if s.p.Realtime {
		due := s.start.Add(time.Duration(float64(s.frames) / s.rate * float64(time.Second)))
		time.Sleep(time.Until(due))
	}
	return frames * s.ch, nil
}

// retune maps the scene onto the current tuning.
func (s *syntheticSource) retune() {
	st := TuneState{Output: "IF", Mode: "USB"}
	if s.p.State != nil {
		st = s.p.State()
	}
	sc := s.p.Scene
	var sigs []dsp.Signal
	switch {
	case st.Output == "Detect":
		// Something audio-like: a 1 kHz tone, as a detector might produce.
		sigs = []dsp.Signal{{Kind: dsp.KindCarrier, FreqHz: 1000 - s.centre, LevelDBFS: -20}}
	case st.Output != "IF" || !ifWorks(sc, st.Mode):
		sigs = []dsp.Signal{{Kind: dsp.KindCarrier, FreqHz: 0, LevelDBFS: -200}}
	default:
		// The synth's inversion conjugates the whole baseband, so modulation
		// sidebands flip too, as they would in an inverted IF.
		s.synth.SetInvert(sc.Inverted[st.Mode])
		if sc.NoiseDBFS != 0 {
			sigs = append(sigs, dsp.Signal{Kind: dsp.KindNoise, LevelDBFS: sc.NoiseDBFS})
		}
		for _, c := range sc.Carriers {
			fbb := c.FreqHz - float64(st.DialHz) - sc.DeltaHz[st.Mode]
			if fbb < -7500 || fbb > 7500 {
				fbb, c.LevelDBFS = 0, -200 // outside the radio's 15 kHz IF filter
			}
			kind := c.Kind
			if kind == "" {
				kind = dsp.KindCarrier
			}
			sigs = append(sigs, dsp.Signal{Kind: kind, FreqHz: fbb, LevelDBFS: c.LevelDBFS, Text: c.Text})
		}
	}
	s.synth.SetSignals(sigs)
}

func ifWorks(sc Scene, mode string) bool {
	if len(sc.IFModes) == 0 {
		return true
	}
	for _, m := range sc.IFModes {
		if m == mode {
			return true
		}
	}
	return false
}
