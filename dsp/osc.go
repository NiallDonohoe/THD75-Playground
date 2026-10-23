package dsp

import "math"

// Oscillator is a complex oscillator e^{jφ} with a phase accumulator, so
// frequency changes keep the phase continuous and blocks join seamlessly.
type Oscillator struct {
	phase float64 // radians, kept in [−π, π)
	step  float64 // radians per sample
	fs    float64
}

// NewOscillator returns an oscillator at freq hertz for sample rate fs.
func NewOscillator(freq, fs float64) *Oscillator {
	o := &Oscillator{fs: fs}
	o.SetFrequency(freq)
	return o
}

// SetFrequency changes the frequency without a phase jump.
func (o *Oscillator) SetFrequency(freq float64) {
	o.step = 2 * math.Pi * freq / o.fs
}

// Frequency returns the current frequency in hertz.
func (o *Oscillator) Frequency() float64 { return o.step * o.fs / (2 * math.Pi) }

// Phase returns the current phase in radians.
func (o *Oscillator) Phase() float64 { return o.phase }

// Next returns e^{jφ} and advances the phase by one sample.
func (o *Oscillator) Next() complex128 {
	s, c := math.Sincos(o.phase)
	o.phase += o.step
	if o.phase >= math.Pi || o.phase < -math.Pi {
		o.phase = math.Remainder(o.phase, 2*math.Pi)
	}
	return complex(c, s)
}

// Fill writes the next len(out) samples.
func (o *Oscillator) Fill(out []complex128) {
	for i := range out {
		out[i] = o.Next()
	}
}
