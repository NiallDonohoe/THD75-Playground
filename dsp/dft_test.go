package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestDFTAnySizeMatchesDirect(t *testing.T) {
	for _, n := range []int{1, 3, 15, 64, 100, 1920} {
		x := make([]complex128, n)
		for i := range x {
			x[i] = complex(math.Sin(float64(i)*0.37)+0.2*float64(i%7), math.Cos(float64(i)*1.3))
		}
		want := make([]complex128, n)
		for k := range want {
			for i, v := range x {
				want[k] += v * cmplx.Rect(1, -2*math.Pi*float64((k*i)%n)/float64(n))
			}
		}
		d, err := NewDFT(n)
		if err != nil {
			t.Fatal(err)
		}
		d.Transform(x)
		for k := range x {
			if cmplx.Abs(x[k]-want[k]) > 1e-9*float64(n) {
				t.Fatalf("n=%d bin %d: got %v, want %v", n, k, x[k], want[k])
			}
		}
	}
}
