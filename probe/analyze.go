package probe

import (
	"math"
	"slices"

	"d75scope/dsp"
	"d75scope/ifsource"
)

// Analysis is what the probe measures in one recording.
type Analysis struct {
	Levels   []dsp.ChannelLevel     `json:"levels"`
	Channels *dsp.ChannelComparison `json:"channels,omitempty"` // channel 0 vs 1, if stereo
	// Silent is true if every sample is exactly zero: usually a refused
	// microphone permission, or no output selected.
	Silent bool `json:"silent"`
	// SpectrumBinHz is the resolution of the analysis spectrum.
	SpectrumBinHz float64 `json:"spectrum_bin_hz"`
	// NoiseFloorDB is the median spectrum level inside the passband (or
	// across the whole spectrum if no passband was found), dBFS per bin.
	NoiseFloorDB float64   `json:"noise_floor_db"`
	Passband     *Passband `json:"passband,omitempty"`
	// LowBandFraction is the fraction of power below 4 kHz: near 1 for
	// audio (AF or Detect), small for an IF centred well above it.
	LowBandFraction float64    `json:"low_band_fraction"`
	Peaks           []dsp.Peak `json:"peaks"`

	spectrum dsp.Spectrum
}

// Passband is the region where the IF's noise stands above the rest of
// the spectrum, found by walking out from the expected centre.
type Passband struct {
	ReferenceDB float64 `json:"reference_db"` // smoothed level near the centre
	Low6Hz      float64 `json:"low_6db_hz"`
	High6Hz     float64 `json:"high_6db_hz"`
	Low20Hz     float64 `json:"low_20db_hz"`
	High20Hz    float64 `json:"high_20db_hz"`
	CentreHz    float64 `json:"centre_hz"` // midpoint of the −6 dB edges
	WidthHz     float64 `json:"width_hz"`  // −6 dB width
	// Shaped is false when the spectrum is flat to the band edges, so no
	// filter shape (and so no centre) could be measured.
	Shaped bool `json:"shaped"`
}

const analysisFFT = 8192

// Analyse measures a recording. expectedCentre is where the IF centre is
// expected to be (12 kHz by Kenwood's documentation).
func Analyse(x []float32, f ifsource.Format, expectedCentre float64) Analysis {
	a := Analysis{Levels: dsp.Levels(x, f.Channels)}
	if f.Channels >= 2 {
		c := dsp.CompareChannels(x, f.Channels, 0, 1)
		a.Channels = &c
	}
	a.Silent = true
	for _, l := range a.Levels {
		if l.ZeroFraction < 1 {
			a.Silent = false
		}
	}
	mono := dsp.Channel(x, f.Channels, 0)
	if len(mono) < analysisFFT || a.Silent {
		return a
	}
	sp := dsp.RealSpectrum(mono, f.SampleRate, analysisFFT)
	a.spectrum = sp
	a.SpectrumBinHz = sp.BinHz

	var low, total float64
	for i, db := range sp.DB {
		p := math.Pow(10, db/10)
		total += p
		if sp.FreqOf(float64(i)) < 4000 {
			low += p
		}
	}
	if total > 0 {
		a.LowBandFraction = low / total
	}

	a.Passband = findPassband(sp, expectedCentre)
	lo, hi := 100.0, f.SampleRate/2-100
	if a.Passband != nil && a.Passband.Shaped {
		lo, hi = a.Passband.Low6Hz, a.Passband.High6Hz
	}
	a.NoiseFloorDB = medianRange(sp, lo, hi)
	a.Peaks = dsp.FindPeaks(sp, 100, f.SampleRate/2-100, 15, 12)
	return a
}

func findPassband(sp dsp.Spectrum, centre float64) *Passband {
	smooth := dsp.MedianFilter(sp.DB, 2*int(150/sp.BinHz)+1)
	c := sp.BinOf(centre)
	span := int(2000 / sp.BinHz)
	ref := median(smooth[max(0, c-span):min(len(smooth), c+span+1)])
	edge := func(drop float64, dir int) float64 {
		i := c
		for i+dir >= 1 && i+dir < len(smooth)-1 && smooth[i+dir] >= ref-drop {
			i += dir
		}
		return sp.FreqOf(float64(i))
	}
	p := &Passband{ReferenceDB: ref, Low6Hz: edge(6, -1), High6Hz: edge(6, 1), Low20Hz: edge(20, -1), High20Hz: edge(20, 1)}
	p.CentreHz = (p.Low6Hz + p.High6Hz) / 2
	p.WidthHz = p.High6Hz - p.Low6Hz
	nyq := sp.FreqOf(float64(len(sp.DB) - 1))
	p.Shaped = p.Low20Hz > 2*sp.BinHz && p.High20Hz < nyq-2*sp.BinHz
	return p
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func medianRange(sp dsp.Spectrum, lo, hi float64) float64 {
	return median(sp.DB[sp.BinOf(lo) : sp.BinOf(hi)+1])
}

// Inversion is the result of comparing a recording with one made after
// tuning the dial up by DialStepHz.
type Inversion struct {
	Mode            string  `json:"mode"`
	Base            string  `json:"base"`
	Up              string  `json:"up"`
	DialStepHz      int64   `json:"dial_step_hz"` // read-back difference, not the request
	MeasuredShiftHz float64 `json:"measured_shift_hz"`
	Score           float64 `json:"score"`  // normalised correlation 0..1
	Energy          float64 `json:"energy"` // signal excess available (dB·bins)
	// S is +1 if signals moved down in the IF (not inverted), −1 if they
	// moved up (inverted), 0 if the measurement is not trustworthy.
	S       int    `json:"s"`
	Verdict string `json:"verdict"`
}

// CompareTuning measures how signals moved between base and up.
func CompareTuning(mode, baseName, upName string, base, up Analysis, dialStep int64, expectedCentre float64) Inversion {
	inv := Inversion{Mode: mode, Base: baseName, Up: upName, DialStepHz: dialStep}
	if base.spectrum.DB == nil || up.spectrum.DB == nil {
		inv.Verdict = "no usable audio in one of the recordings"
		return inv
	}
	if dialStep == 0 {
		inv.Verdict = "the dial did not move (FQ read-back unchanged)"
		return inv
	}
	lo, hi := expectedCentre-7500, expectedCentre+7500
	if pb := base.Passband; pb != nil && pb.Shaped && pb.WidthHz > 5000 {
		lo, hi = pb.Low6Hz, pb.High6Hz
	}
	step := math.Abs(float64(dialStep))
	shift, score, energy := dsp.EstimateShift(base.spectrum, up.spectrum, lo, hi, 2*step)
	inv.MeasuredShiftHz, inv.Score, inv.Energy = shift, score, energy
	switch {
	case energy < 30:
		inv.Verdict = "no signals strong enough to follow; tune near a steady carrier and run again"
	case score < 0.5:
		inv.Verdict = "signals did not line up between the two recordings (fading or keyed signals?)"
	case math.Abs(math.Abs(shift)-step) > 0.05*step+3*base.SpectrumBinHz:
		inv.Verdict = "signals moved by an unexpected amount; check the dial read-back"
	default:
		// Dial up by step: a fixed RF signal moves by −s·step in baseband.
		inv.S = -int(math.Copysign(1, shift*float64(dialStep)))
		if inv.S == 1 {
			inv.Verdict = "not inverted: signals moved down in the IF when the dial moved up"
		} else {
			inv.Verdict = "inverted: signals moved up in the IF when the dial moved up"
		}
	}
	return inv
}

// Offset solves the calibration formula f_RF = f_dial + Δ + s·f_bb for Δ
// using a carrier of known frequency.
type Offset struct {
	Mode         string  `json:"mode"`
	Recording    string  `json:"recording"`
	RefCarrierHz int64   `json:"ref_carrier_hz"`
	DialHz       int64   `json:"dial_hz"`
	PeakIFHz     float64 `json:"peak_if_hz"`
	FbbHz        float64 `json:"fbb_hz"` // peak relative to the IF centre used
	IFCentreHz   float64 `json:"if_centre_hz"`
	S            int     `json:"s"`
	DeltaHz      float64 `json:"delta_hz"`
	Note         string  `json:"note,omitempty"`
}

// SolveOffset finds the strongest peak and solves for Δ.
func SolveOffset(mode, name string, a Analysis, dial, ref int64, s int, centre float64) *Offset {
	if len(a.Peaks) == 0 || s == 0 {
		return nil
	}
	// The strongest peak inside the passband.
	lo, hi := centre-7500, centre+7500
	var best *dsp.Peak
	for i := range a.Peaks {
		p := &a.Peaks[i]
		if p.FreqHz >= lo && p.FreqHz <= hi && (best == nil || p.LevelDB > best.LevelDB) {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	o := &Offset{Mode: mode, Recording: name, RefCarrierHz: ref, DialHz: dial, PeakIFHz: best.FreqHz, IFCentreHz: centre, S: s}
	o.FbbHz = best.FreqHz - centre
	o.DeltaHz = float64(ref-dial) - float64(s)*o.FbbHz
	if best.ProminenceDB < 20 {
		o.Note = "weak peak; the result may be another signal"
	}
	return o
}
