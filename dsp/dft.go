package dsp

import (
	"fmt"
	"math"
	"math/cmplx"
)

// DFT is a forward complex DFT of any size. Powers of two go straight to
// the radix-2 FFT; other sizes use Bluestein's algorithm, which turns the
// DFT into a convolution done with a power-of-two FFT. FT8's natural sizes
// (1920 samples a symbol, 3840 with zero padding) are not powers of two.
type DFT struct {
	n     int
	fft   *FFT
	chirp []complex128 // e^{-jπk²/n}
	conv  []complex128 // FFT of the conjugate chirp, scaled for the inverse
	buf   []complex128
}

// NewDFT prepares a DFT of size n ≥ 1.
func NewDFT(n int) (*DFT, error) {
	if n < 1 {
		return nil, fmt.Errorf("DFT size %d", n)
	}
	if n&(n-1) == 0 && n >= 2 {
		f, err := NewFFT(n)
		if err != nil {
			return nil, err
		}
		return &DFT{n: n, fft: f}, nil
	}
	m := 2
	for m < 2*n-1 {
		m <<= 1
	}
	f, err := NewFFT(m)
	if err != nil {
		return nil, err
	}
	d := &DFT{n: n, fft: f, chirp: make([]complex128, n), conv: make([]complex128, m), buf: make([]complex128, m)}
	for k := range d.chirp {
		// k² mod 2n keeps the phase argument small, and so exact.
		k2 := (k * k) % (2 * n)
		d.chirp[k] = cmplx.Rect(1, -math.Pi*float64(k2)/float64(n))
	}
	d.conv[0] = cmplx.Conj(d.chirp[0])
	for k := 1; k < n; k++ {
		d.conv[k] = cmplx.Conj(d.chirp[k])
		d.conv[m-k] = d.conv[k]
	}
	f.Transform(d.conv)
	for i := range d.conv {
		d.conv[i] /= complex(float64(m), 0)
	}
	return d, nil
}

// Size returns n.
func (d *DFT) Size() int { return d.n }

// Transform computes X[k] = Σ x[n] e^{-j2πkn/N} in place.
func (d *DFT) Transform(x []complex128) {
	if len(x) != d.n {
		panic(fmt.Sprintf("DFT of size %d given %d samples", d.n, len(x)))
	}
	if d.chirp == nil {
		if d.fft != nil {
			d.fft.Transform(x)
		}
		return
	}
	b := d.buf
	for i := range b {
		b[i] = 0
	}
	for i, v := range x {
		b[i] = v * d.chirp[i]
	}
	d.fft.Transform(b)
	// Multiply by the chirp's spectrum, then invert with the conjugate
	// trick: ifft(y) = conj(fft(conj(y))) / m, the 1/m already in conv.
	for i := range b {
		b[i] = cmplx.Conj(b[i] * d.conv[i])
	}
	d.fft.Transform(b)
	for k := range x {
		x[k] = cmplx.Conj(b[k]) * d.chirp[k]
	}
}
