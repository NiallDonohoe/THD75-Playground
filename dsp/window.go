package dsp

import "math"

// BlackmanHarris4 returns the 4-term Blackman–Harris window (−92 dB
// sidelobes) of length n, periodic form for spectral analysis.
func BlackmanHarris4(n int) []float64 {
	const a0, a1, a2, a3 = 0.35875, 0.48829, 0.14128, 0.01168
	w := make([]float64, n)
	for i := range w {
		x := 2 * math.Pi * float64(i) / float64(n)
		w[i] = a0 - a1*math.Cos(x) + a2*math.Cos(2*x) - a3*math.Cos(3*x)
	}
	return w
}

// WindowSum returns Σw, used to scale a windowed FFT to amplitude.
func WindowSum(w []float64) float64 {
	s := 0.0
	for _, v := range w {
		s += v
	}
	return s
}

// ENBW returns a window's equivalent noise bandwidth in bins.
func ENBW(w []float64) float64 {
	s, s2 := 0.0, 0.0
	for _, v := range w {
		s += v
		s2 += v * v
	}
	return float64(len(w)) * s2 / (s * s)
}
