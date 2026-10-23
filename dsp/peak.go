package dsp

import (
	"math"
	"sort"
)

// InterpolatePeak fits a parabola through bins i−1, i, i+1 of a dB spectrum
// and returns the peak's offset from i in bins (−0.5..0.5) and its level.
func InterpolatePeak(db []float64, i int) (delta, level float64) {
	if i <= 0 || i >= len(db)-1 {
		return 0, db[i]
	}
	a, b, c := db[i-1], db[i], db[i+1]
	den := a - 2*b + c
	if den == 0 {
		return 0, b
	}
	delta = 0.5 * (a - c) / den
	return delta, b - 0.25*(a-c)*delta
}

// Peak is a spectral peak.
type Peak struct {
	FreqHz       float64 `json:"freq_hz"`
	LevelDB      float64 `json:"level_db"`
	ProminenceDB float64 `json:"prominence_db"` // above the local noise floor
}

// FindPeaks returns up to maxPeaks local maxima between loHz and hiHz that
// stand at least minProminence dB above the median-filtered floor, strongest
// first, with sub-bin frequencies.
func FindPeaks(s Spectrum, loHz, hiHz, minProminence float64, maxPeaks int) []Peak {
	floor := MedianFilter(s.DB, 61)
	lo, hi := max(1, s.BinOf(loHz)), min(len(s.DB)-2, s.BinOf(hiHz))
	var peaks []Peak
	for i := lo; i <= hi; i++ {
		v := s.DB[i]
		if v < s.DB[i-1] || v <= s.DB[i+1] {
			continue
		}
		prom := v - floor[i]
		if prom < minProminence {
			continue
		}
		d, level := InterpolatePeak(s.DB, i)
		peaks = append(peaks, Peak{FreqHz: s.FreqOf(float64(i) + d), LevelDB: level, ProminenceDB: prom})
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].LevelDB > peaks[b].LevelDB })
	// Drop window sidelobes and leakage: peaks within 4 bins of a stronger one.
	var kept []Peak
	for _, p := range peaks {
		near := false
		for _, k := range kept {
			if math.Abs(k.FreqHz-p.FreqHz) < 4*s.BinHz {
				near = true
				break
			}
		}
		if !near {
			kept = append(kept, p)
		}
		if len(kept) == maxPeaks {
			break
		}
	}
	return kept
}

// EstimateShift measures how far the signals in b have moved relative to a,
// in hertz (positive: b's signals sit higher). Each spectrum is whitened by
// subtracting its median floor, so the fixed IF filter shape does not pin
// the answer to zero; only peaks above the floor count. score is the
// normalised correlation at the best shift (0..1); energy is the summed
// excess (dB·bins) available to correlate, so callers can tell "no signals"
// from "signals that disagree".
func EstimateShift(a, b Spectrum, loHz, hiHz, maxShiftHz float64) (shiftHz, score, energy float64) {
	ea, eb := excess(a), excess(b)
	lo, hi := a.BinOf(loHz), a.BinOf(hiHz)
	maxK := int(math.Ceil(maxShiftHz / a.BinHz))
	corr := make([]float64, 2*maxK+1)
	var na float64
	for i := lo; i <= hi; i++ {
		na += ea[i] * ea[i]
		energy += ea[i]
	}
	best := 0
	for k := -maxK; k <= maxK; k++ {
		var c, nb float64
		for i := lo; i <= hi; i++ {
			j := i + k
			if j < 0 || j >= len(eb) {
				continue
			}
			c += ea[i] * eb[j]
			nb += eb[j] * eb[j]
		}
		if na > 0 && nb > 0 {
			corr[k+maxK] = c / math.Sqrt(na*nb)
		}
		if corr[k+maxK] > corr[best] {
			best = k + maxK
		}
	}
	d := 0.0
	if best > 0 && best < len(corr)-1 {
		y0, y1, y2 := corr[best-1], corr[best], corr[best+1]
		if den := y0 - 2*y1 + y2; den != 0 {
			d = 0.5 * (y0 - y2) / den
		}
	}
	return (float64(best-maxK) + d) * a.BinHz, corr[best], energy
}

// excess is the spectrum's level above its median floor, ignoring anything
// within 6 dB of it.
func excess(s Spectrum) []float64 {
	floor := MedianFilter(s.DB, 61)
	out := make([]float64, len(s.DB))
	for i, v := range s.DB {
		if e := v - floor[i] - 6; e > 0 {
			out[i] = e
		}
	}
	return out
}
