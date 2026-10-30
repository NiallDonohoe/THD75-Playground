package ft8

import "math"

// Tones returns the 79 channel symbols (0–7) that carry a payload: CRC,
// LDPC parity, Gray mapping and the three Costas arrays.
func (p *Payload) Tones() (tones [NumSymbols]int) {
	pl := [payloadBits]uint8(*p)
	a := addCRC(&pl)
	c := ldpcEncode(&a)
	for _, s := range costasStart {
		for k, t := range costas {
			tones[s+k] = t
		}
	}
	for i := 0; i < numData; i++ {
		v := int(c[3*i])<<2 | int(c[3*i+1])<<1 | int(c[3*i+2])
		tones[dataSymbol(i)] = grayMap[v]
	}
	return tones
}

// Encode packs a message and returns its tones.
func Encode(text string) ([NumSymbols]int, error) {
	p, err := Pack(text)
	if err != nil {
		return [NumSymbols]int{}, err
	}
	return p.Tones(), nil
}

// gfskBT is FT8's Gaussian filter bandwidth–time product.
const gfskBT = 2.0

var gfskPulse = func() []float64 {
	// The frequency pulse of one symbol, three symbols long:
	// ½[erf(c·BT·(t+½)) − erf(c·BT·(t−½))], t in symbols from the centre.
	c := math.Pi * math.Sqrt(2/math.Ln2)
	p := make([]float64, 3*SymbolSamples)
	for i := range p {
		t := float64(i)/SymbolSamples - 1.5
		p[i] = 0.5 * (math.Erf(c*gfskBT*(t+0.5)) - math.Erf(c*gfskBT*(t-0.5)))
	}
	return p
}()

// gfskPhase returns the phase, in radians, of each of the SignalSamples
// samples of a transmission whose tone 0 is at freq Hz, starting from 0.
func gfskPhase(tones *[NumSymbols]int, freq float64) []float64 {
	const n = SymbolSamples
	// Instantaneous frequency in tone units, with a dummy symbol at each
	// end (repeating the first and last tone) so the edges are smooth.
	// Index 0 here is one symbol before the transmission starts.
	f := make([]float64, (NumSymbols+2)*n)
	for j, t := range tones {
		for i, p := range gfskPulse {
			f[j*n+i] += float64(t) * p
		}
	}
	for i := 0; i < 2*n; i++ {
		f[i] += float64(tones[0]) * gfskPulse[n+i]
		f[NumSymbols*n+i] += float64(tones[NumSymbols-1]) * gfskPulse[i]
	}
	phase := make([]float64, SignalSamples)
	base := 2 * math.Pi * freq / SampleRate
	step := 2 * math.Pi * ToneSpacing / SampleRate
	phi := 0.0
	for k := range phase {
		phase[k] = phi
		phi += base + step*f[n+k]
		if phi > 2*math.Pi {
			phi -= 2 * math.Pi
		}
	}
	return phase
}

// Synthesize returns one transmission at 12 kHz: SignalSamples samples of
// GFSK with tone 0 at freq Hz and peak amplitude amp, ramped up and down
// over the first and last eighth of a symbol as WSJT-X does.
func Synthesize(tones [NumSymbols]int, freq, amp float64) []float32 {
	phase := gfskPhase(&tones, freq)
	out := make([]float32, len(phase))
	ramp := SymbolSamples / 8
	for k, phi := range phase {
		a := amp
		if k < ramp {
			a *= (1 - math.Cos(math.Pi*float64(k)/float64(ramp))) / 2
		} else if k >= len(phase)-ramp {
			a *= (1 - math.Cos(math.Pi*float64(len(phase)-1-k)/float64(ramp))) / 2
		}
		out[k] = float32(a * math.Sin(phi))
	}
	return out
}
