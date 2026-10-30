package ft8

import "math"

// subtract removes a decoded signal from the audio, so weaker signals it
// covered can decode in the next pass. It rebuilds the signal's phase from
// its tones, refines the start to the sample, measures the signal's slowly
// varying complex amplitude against the rebuilt phase (a smoothed product,
// which follows fading and a small frequency error), and subtracts the
// rebuilt signal. It returns the refined start.
func subtract(x []float64, tones *[NumSymbols]int, freq float64, start int) int {
	phase := gfskPhase(tones, freq)
	n := len(phase)
	ref := make([]complex128, n)
	for k, phi := range phase {
		ref[k] = complex(math.Cos(phi), math.Sin(phi))
	}
	start = refineStart(x, ref, start)
	z := make([]complex128, n)
	mask := make([]complex128, n)
	for k := range ref {
		if i := start + k; i >= 0 && i < len(x) {
			z[k] = complex(x[i], 0) * complex(real(ref[k]), -imag(ref[k]))
			mask[k] = 1
		}
	}
	// Two passes of a box filter make a triangular low-pass about two
	// symbols wide. Smoothing the in-range mask the same way and dividing
	// by it keeps the estimate unbiased at the ends.
	const width = 1600
	z = boxSmooth(boxSmooth(z, width), width)
	mask = boxSmooth(boxSmooth(mask, width), width)
	for k := range z {
		i := start + k
		if i < 0 || i >= len(x) || real(mask[k]) < 1e-6 {
			continue
		}
		a := z[k] / mask[k]
		x[i] -= 2 * real(a*ref[k])
	}
	return start
}

// refineStart moves start, which the baseband knows to ±32 samples, to the
// sample where the rebuilt signal matches best. A misplaced reference
// leaves the symbol edges behind, which matters most for strong signals.
func refineStart(x []float64, ref []complex128, start int) int {
	best, bestM := start, matchPower(x, ref, start)
	for _, step := range []int{16, 4, 1} {
		centre := best
		for o := -3 * step; o <= 3*step; o += step {
			if o == 0 {
				continue
			}
			if m := matchPower(x, ref, centre+o); m > bestM {
				best, bestM = centre+o, m
			}
		}
	}
	return best
}

// matchPower is the energy of the audio correlated with the reference one
// symbol at a time.
func matchPower(x []float64, ref []complex128, start int) float64 {
	var total float64
	for s := 0; s < NumSymbols; s++ {
		var acc complex128
		for k := s * SymbolSamples; k < (s+1)*SymbolSamples; k++ {
			if i := start + k; i >= 0 && i < len(x) {
				acc += complex(x[i], 0) * complex(real(ref[k]), -imag(ref[k]))
			}
		}
		total += real(acc)*real(acc) + imag(acc)*imag(acc)
	}
	return total
}

// boxSmooth returns the running sum of v over a centred window of width w.
func boxSmooth(v []complex128, w int) []complex128 {
	sum := make([]complex128, len(v)+1)
	for i, c := range v {
		sum[i+1] = sum[i] + c
	}
	out := make([]complex128, len(v))
	for i := range v {
		lo, hi := i-w/2, i+w/2+1
		if lo < 0 {
			lo = 0
		}
		if hi > len(v) {
			hi = len(v)
		}
		out[i] = sum[hi] - sum[lo]
	}
	return out
}
