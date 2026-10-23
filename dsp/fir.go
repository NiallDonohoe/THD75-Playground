package dsp

import "math"

// KaiserBeta returns the Kaiser window β for a stopband attenuation in dB.
func KaiserBeta(attenDB float64) float64 {
	switch {
	case attenDB > 50:
		return 0.1102 * (attenDB - 8.7)
	case attenDB >= 21:
		return 0.5842*math.Pow(attenDB-21, 0.4) + 0.07886*(attenDB-21)
	}
	return 0
}

// KaiserTaps estimates the odd tap count for a Kaiser-window FIR with the
// given attenuation and transition width (as a fraction of the sample rate).
func KaiserTaps(attenDB, transition float64) int {
	n := int(math.Ceil((attenDB-7.95)/(14.36*transition))) + 1
	if n%2 == 0 {
		n++
	}
	return n
}

// KaiserWindow returns a Kaiser window of length n.
func KaiserWindow(n int, beta float64) []float64 {
	w := make([]float64, n)
	den := besselI0(beta)
	for i := range w {
		r := 2*float64(i)/float64(n-1) - 1
		w[i] = besselI0(beta*math.Sqrt(1-r*r)) / den
	}
	return w
}

func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		sum += term
		if term < 1e-12*sum {
			break
		}
	}
	return sum
}

// LowpassFIR designs a linear-phase low-pass filter: a windowed sinc with
// cutoff (−6 dB point) at cutoff × sample rate, unity gain at DC.
func LowpassFIR(taps int, cutoff, beta float64) []float64 {
	w := KaiserWindow(taps, beta)
	h := make([]float64, taps)
	mid := float64(taps-1) / 2
	sum := 0.0
	for i := range h {
		x := float64(i) - mid
		if x == 0 {
			h[i] = 2 * cutoff
		} else {
			h[i] = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		h[i] *= w[i]
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum
	}
	return h
}

// FrequencyResponse returns |H(f)| in dB at f × sample rate.
func FrequencyResponse(h []float64, f float64) float64 {
	var re, im float64
	for i, v := range h {
		ph := -2 * math.Pi * f * float64(i)
		re += v * math.Cos(ph)
		im += v * math.Sin(ph)
	}
	return AmplitudeDB(math.Hypot(re, im))
}

// ComplexFIR filters a complex stream with real taps, keeping state across
// blocks.
type ComplexFIR struct {
	taps []float64
	hist []complex128 // circular, doubled so a window is contiguous
	pos  int
}

// NewComplexFIR returns a streaming filter.
func NewComplexFIR(taps []float64) *ComplexFIR {
	return &ComplexFIR{taps: taps, hist: make([]complex128, 2*len(taps))}
}

// Process filters one sample.
func (f *ComplexFIR) Process(x complex128) complex128 {
	n := len(f.taps)
	f.pos--
	if f.pos < 0 {
		f.pos = n - 1
	}
	f.hist[f.pos] = x
	f.hist[f.pos+n] = x
	var re, im float64
	win := f.hist[f.pos : f.pos+n]
	for i, t := range f.taps {
		re += t * real(win[i])
		im += t * imag(win[i])
	}
	return complex(re, im)
}
