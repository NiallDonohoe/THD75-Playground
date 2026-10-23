package dsp

import (
	"fmt"
	"math"
	"math/bits"
	"math/cmplx"
)

// FFT is an in-place radix-2 complex FFT of a fixed power-of-two size.
type FFT struct {
	n       int
	twiddle []complex128
	rev     []int
}

// NewFFT prepares an FFT of size n, which must be a power of two.
func NewFFT(n int) (*FFT, error) {
	if n < 2 || n&(n-1) != 0 {
		return nil, fmt.Errorf("FFT size %d is not a power of two", n)
	}
	f := &FFT{n: n, twiddle: make([]complex128, n/2), rev: make([]int, n)}
	for i := range f.twiddle {
		f.twiddle[i] = cmplx.Rect(1, -2*math.Pi*float64(i)/float64(n))
	}
	shift := 64 - bits.Len(uint(n-1))
	for i := range f.rev {
		f.rev[i] = int(bits.Reverse64(uint64(i)) >> shift)
	}
	return f, nil
}

// Size returns n.
func (f *FFT) Size() int { return f.n }

// Transform computes the forward DFT of x in place: X[k] = Σ x[n] e^{-j2πkn/N}.
func (f *FFT) Transform(x []complex128) {
	if len(x) != f.n {
		panic(fmt.Sprintf("FFT of size %d given %d samples", f.n, len(x)))
	}
	for i, j := range f.rev {
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				t := f.twiddle[k*step] * x[start+k+half]
				x[start+k+half] = x[start+k] - t
				x[start+k] += t
			}
		}
	}
}
