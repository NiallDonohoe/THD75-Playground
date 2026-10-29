package demo

import (
	"math"
)

// Encoders for the formats the demo stations send but d75aprs never does
// (the aprs package only encodes what the program transmits). They follow
// APRS101 chapters 9 and 10; the tests decode their output with package aprs.

// micE encodes a Mic-E position. msg is the standard message number as
// package aprs names them (0 Emergency … 7 Off Duty); prefix is a radio type
// byte such as Kenwood's '>' or "" for none. It returns the destination
// address and the information field.
func micE(lat, lon float64, speedKn, course int, table, code byte, msg int, prefix string, altM int, hasAlt bool) (dst, info string) {
	h := int(math.Round(math.Abs(lat) * 6000))
	deg, mh := h/6000, h%6000
	digits := [6]int{deg / 10, deg % 10, mh / 1000, mh / 100 % 10, mh / 10 % 10, mh % 10}

	ld := int(math.Round(math.Abs(lon)*6000)) / 6000
	lmh := int(math.Round(math.Abs(lon)*6000)) % 6000
	flags := [6]bool{
		msg>>2&1 == 1, msg>>1&1 == 1, msg&1 == 1,
		lat >= 0,             // north
		ld <= 9 || ld >= 100, // longitude offset +100
		lon < 0,              // west
	}
	d := make([]byte, 6)
	for i := range d {
		if flags[i] {
			d[i] = 'P' + byte(digits[i])
		} else {
			d[i] = '0' + byte(digits[i])
		}
	}

	var b0 int
	switch {
	case ld <= 9:
		b0 = ld + 118
	case ld <= 99:
		b0 = ld + 28
	case ld <= 109:
		b0 = ld + 8
	default:
		b0 = ld - 72
	}
	lm, lh := lmh/100, lmh%100
	b1 := lm + 28
	if lm < 10 {
		b1 = lm + 88
	}
	// Speed with the +800 offset and course with the +400 offset keep these
	// bytes printable; the decoder takes both off again.
	sp := speedKn/10 + 80
	dc := speedKn%10*10 + course/100 + 4
	se := course % 100
	out := []byte{'`', byte(b0), byte(b1), byte(lh + 28), byte(sp + 28), byte(dc + 28), byte(se + 28), code, table}
	out = append(out, prefix...)
	if hasAlt {
		out = append(out, base91(altM+10000, 3)...)
		out = append(out, '}')
	}
	return string(d), string(out)
}

// compressed encodes a compressed position report with course and speed,
// starting with the '!' data type.
func compressed(lat, lon float64, table, code byte, course int, speedKn float64) string {
	y := int(math.Round(380926 * (90 - lat)))
	x := int(math.Round(190463 * (180 + lon)))
	b := []byte{'!', table}
	b = append(b, base91(y, 4)...)
	b = append(b, base91(x, 4)...)
	s := int(math.Round(math.Log(speedKn+1) / math.Log(1.08)))
	b = append(b, code, byte(course%360/4+33), byte(s+33), '[')
	return string(b)
}

func base91(n, width int) []byte {
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = byte(n%91 + 33)
		n /= 91
	}
	return b
}
