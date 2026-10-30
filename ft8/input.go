package ft8

import (
	"fmt"

	"d75scope/dsp"
)

// To12k turns interleaved audio at any whole multiple of 12 kHz (such as
// the 48 kHz a USB sound device delivers) into the 12 kHz mono the decoder
// takes. It keeps the first channel and low-pass filters before
// decimating, flat to 3.5 kHz and at least 70 dB down from 6 kHz.
func To12k(samples []float32, rate, channels int) ([]float32, error) {
	if channels < 1 {
		return nil, fmt.Errorf("ft8: %d channels", channels)
	}
	if rate%SampleRate != 0 || rate == 0 {
		return nil, fmt.Errorf("ft8: sample rate %d Hz is not a multiple of 12 kHz", rate)
	}
	mono := make([]float32, len(samples)/channels)
	for i := range mono {
		mono[i] = samples[i*channels]
	}
	k := rate / SampleRate
	if k == 1 {
		return mono, nil
	}
	const atten = 70
	fr := float64(rate)
	taps := dsp.LowpassFIR(dsp.KaiserTaps(atten, (6000-3500)/fr), 4750/fr, dsp.KaiserBeta(atten))
	half := len(taps) / 2
	out := make([]float32, len(mono)/k)
	for i := range out {
		c := i * k
		var acc float64
		for j, h := range taps {
			if n := c + j - half; n >= 0 && n < len(mono) {
				acc += h * float64(mono[n])
			}
		}
		out[i] = float32(acc)
	}
	return out, nil
}
