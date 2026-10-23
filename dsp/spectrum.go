package dsp

import (
	"math"
	"math/cmplx"
	"slices"
)

// Spectrum is an averaged power spectrum in dBFS: a full-scale sine reads
// 0 dB at its peak bin.
type Spectrum struct {
	BinHz  float64   `json:"bin_hz"`
	DB     []float64 `json:"-"`
	Frames int       `json:"frames"` // FFT frames averaged
}

// FreqOf returns the frequency of bin i (fractional bins allowed).
func (s Spectrum) FreqOf(i float64) float64 { return i * s.BinHz }

// BinOf returns the nearest bin to f, clamped to the spectrum.
func (s Spectrum) BinOf(f float64) int {
	i := int(math.Round(f / s.BinHz))
	return max(0, min(len(s.DB)-1, i))
}

// RealSpectrum averages the one-sided power spectrum of a real signal over
// frames of size n (a power of two) with 50% overlap and a 4-term
// Blackman–Harris window. It returns n/2+1 bins.
func RealSpectrum(x []float64, fs float64, n int) Spectrum {
	fft, err := NewFFT(n)
	if err != nil {
		panic(err)
	}
	w := BlackmanHarris4(n)
	sum := WindowSum(w)
	power := make([]float64, n/2+1)
	buf := make([]complex128, n)
	frames := 0
	for start := 0; start+n <= len(x); start += n / 2 {
		for i := range buf {
			buf[i] = complex(x[start+i]*w[i], 0)
		}
		fft.Transform(buf)
		for k := range power {
			a := cmplx.Abs(buf[k])
			power[k] += a * a
		}
		frames++
	}
	s := Spectrum{BinHz: fs / float64(n), DB: make([]float64, len(power)), Frames: frames}
	for k, p := range power {
		if frames > 0 {
			p /= float64(frames)
		}
		// One-sided amplitude scaling: a sine of amplitude A gives |X| = A·Σw/2.
		s.DB[k] = PowerDB(4 * p / (sum * sum))
	}
	return s
}

// PowerDB converts linear power to dB, floored at −200 dB.
func PowerDB(p float64) float64 {
	if p <= 1e-20 {
		return -200
	}
	return 10 * math.Log10(p)
}

// AmplitudeDB converts a linear amplitude to dB, floored at −200 dB.
func AmplitudeDB(a float64) float64 {
	return PowerDB(a * a)
}

// MedianFilter smooths v with a running median of odd width, which tracks
// the noise floor and ignores narrow peaks.
func MedianFilter(v []float64, width int) []float64 {
	if width < 1 {
		width = 1
	}
	half := width / 2
	out := make([]float64, len(v))
	win := make([]float64, 0, width)
	for i := range v {
		lo, hi := max(0, i-half), min(len(v), i+half+1)
		win = append(win[:0], v[lo:hi]...)
		slices.Sort(win)
		out[i] = win[len(win)/2]
	}
	return out
}
