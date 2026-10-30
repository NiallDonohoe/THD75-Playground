package ft8

import (
	"fmt"
	"math"
)

// The (174,91) LDPC code. Its two tables, in ldpc_tables.go, are generated
// from Kārlis Goba's ft8_lib (MIT licence, ft8/constants.c), which took them
// from WSJT-X. They are protocol constants: a decoder that differs by one
// bit cannot talk to anyone. The generator (for encoding) and the sparse
// parity checks (for decoding) are separate tables, and a test checks each
// against the other, so a transcription error in either would show.
//
// A codeword is the 91 message bits followed by the 83 parity bits.

// varChecks lists the three parity checks each codeword bit takes part in.
var varChecks [ldpcN][3]int

// checkEdge is one bit of a parity check: the bit, and which of that bit's
// three checks this is.
type checkEdge struct{ bit, slot int }

var checkEdges [ldpcM][]checkEdge

func init() {
	var used [ldpcN]int
	for m, row := range ldpcChecks {
		for _, b := range row {
			if b == 0 {
				continue
			}
			n := int(b) - 1
			if used[n] == 3 {
				panic("ft8: codeword bit in more than three checks")
			}
			varChecks[n][used[n]] = m
			checkEdges[m] = append(checkEdges[m], checkEdge{n, used[n]})
			used[n]++
		}
	}
	for n, u := range used {
		if u != 3 {
			panic(fmt.Sprintf("ft8: codeword bit %d is in %d checks, not three", n, u))
		}
	}
}

// ldpcEncode adds the 83 parity bits to 91 message bits.
func ldpcEncode(a *[ldpcK]uint8) (c [ldpcN]uint8) {
	copy(c[:], a[:])
	for i, row := range ldpcGenerator {
		var p uint8
		for j := 0; j < ldpcK; j++ {
			if row[j/8]>>(7-j%8)&1 != 0 {
				p ^= a[j]
			}
		}
		c[ldpcK+i] = p
	}
	return c
}

// parityErrors counts the parity checks a hard-decision codeword fails.
func parityErrors(c *[ldpcN]uint8) int {
	errs := 0
	for _, edges := range checkEdges {
		var x uint8
		for _, e := range edges {
			x ^= c[e.bit]
		}
		errs += int(x)
	}
	return errs
}

// bpDecode runs sum-product belief propagation. llr holds, for each code
// bit, log(P(0)/P(1)): positive means 0. It returns the hard decisions and
// the number of parity checks they still fail (0 means a codeword).
func bpDecode(llr *[ldpcN]float64, maxIter int) (c [ldpcN]uint8, errs int) {
	var toVar [ldpcN][3]float64 // check → bit messages
	var toCheck [7]float64
	errs = ldpcM
	for iter := 0; ; iter++ {
		for n := range c {
			t := llr[n] + toVar[n][0] + toVar[n][1] + toVar[n][2]
			if t < 0 {
				c[n] = 1
			} else {
				c[n] = 0
			}
		}
		errs = parityErrors(&c)
		if errs == 0 || iter == maxIter {
			return c, errs
		}
		for _, edges := range checkEdges {
			// Bit → check messages: everything the bit knows except what
			// this check told it, squashed to tanh(L/2).
			for i, e := range edges {
				v := llr[e.bit]
				for s := 0; s < 3; s++ {
					if s != e.slot {
						v += toVar[e.bit][s]
					}
				}
				toCheck[i] = math.Tanh(v / 2)
			}
			for i, e := range edges {
				p := 1.0
				for j := range edges {
					if j != i {
						p *= toCheck[j]
					}
				}
				// Clamp so atanh stays finite when the others are certain.
				p = math.Max(-0.9999999, math.Min(0.9999999, p))
				toVar[e.bit][e.slot] = 2 * math.Atanh(p)
			}
		}
	}
}
