package aprs

import (
	"fmt"
	"strings"
)

// Mic-E (APRS101 chapter 10) packs the latitude, a three-bit message and two
// longitude flags into the six characters of the destination address, and
// the longitude, speed, course and symbol into the first eight bytes after
// the data type. Kenwood radios, the TH-D75 among them, send it.

// micEStandard names the standard Mic-E messages by their bits A, B, C.
var micEStandard = [8]string{"Emergency", "Priority", "Special", "Committed", "Returning", "In Service", "En Route", "Off Duty"}

// micE decodes a Mic-E information field (data type ` or ') with its
// destination address.
func (d *Data) micE(dst, info string) error {
	dest, _, _ := strings.Cut(dst, "-")
	if len(dest) != 6 {
		return fmt.Errorf("%w: Mic-E destination %q is not six characters", ErrMalformed, dst)
	}
	if len(info) < 9 {
		return fmt.Errorf("%w: Mic-E information field is shorter than 9 bytes", ErrMalformed)
	}

	var digits [6]byte
	var bits [3]int // per message bit: 0 clear, 1 standard, 2 custom
	for i := 0; i < 6; i++ {
		c := dest[i]
		switch {
		case isDigit(c):
			digits[i] = c
		case c >= 'A' && c <= 'J' && i < 3:
			digits[i], bits[i] = c-'A'+'0', 2
		case c == 'K' && i < 3:
			digits[i], bits[i] = ' ', 2
		case c == 'L':
			digits[i] = ' '
		case c >= 'P' && c <= 'Y':
			digits[i] = c - 'P' + '0'
			if i < 3 {
				bits[i] = 1
			}
		case c == 'Z':
			digits[i] = ' '
			if i < 3 {
				bits[i] = 1
			}
		default:
			return fmt.Errorf("%w: Mic-E destination %q has %q at position %d", ErrMalformed, dst, c, i+1)
		}
	}
	deg, hun, amb, ok := ambiguous(digits)
	if !ok || hun >= 6000 || deg > 90 {
		return fmt.Errorf("%w: Mic-E latitude in %q", ErrMalformed, dst)
	}
	lat := degrees(deg, hun, amb)
	if dest[3] < 'P' { // 0–9 and L: south
		lat = -lat
	}

	b := info[1:9]
	lonDeg := micELonDegrees(b[0], dest[4] >= 'P')
	lonMin := int(b[1]) - 28
	if lonMin >= 60 {
		lonMin -= 60
	}
	lonHun := int(b[2]) - 28
	if lonDeg < 0 || lonDeg > 179 || lonMin < 0 || lonMin > 59 || lonHun < 0 || lonHun > 99 {
		return fmt.Errorf("%w: Mic-E longitude bytes %q", ErrMalformed, b[:3])
	}
	lon := degrees(lonDeg, lonMin*100+lonHun, amb)
	if dest[5] >= 'P' {
		lon = -lon
	}

	sp, dc, se := int(b[3])-28, int(b[4])-28, int(b[5])-28
	if sp < 0 || dc < 0 || se < 0 {
		return fmt.Errorf("%w: Mic-E speed and course bytes %q", ErrMalformed, b[3:6])
	}
	speed := sp*10 + dc/10
	if speed >= 800 {
		speed -= 800
	}
	course := dc%10*100 + se
	if course >= 400 {
		course -= 400
	}

	pos := Position{Lat: lat, Lon: lon, Ambiguity: amb, Symbol: Symbol{Table: b[7], Code: b[6]}}
	if !validTable(pos.Symbol.Table) || !validSymbolCode(pos.Symbol.Code) {
		return fmt.Errorf("%w: Mic-E symbol %q%q", ErrMalformed, pos.Symbol.Table, pos.Symbol.Code)
	}
	if course <= 360 {
		pos.HasCourseSpeed = true
		pos.Course = course
		pos.Speed = float64(speed)
	}

	d.Kind = KindPosition
	d.Position = &pos
	d.Comment = pos.takeMicEAltitude(info[9:])
	d.MicE = micEMessage(bits)
	return nil
}

// micELonDegrees decodes the longitude degrees byte. With the +100 offset
// flag, 0–9° are sent as 190–199 and 100–109° as 180–189.
func micELonDegrees(b byte, offset bool) int {
	d := int(b) - 28
	if offset {
		d += 100
	}
	switch {
	case d >= 180 && d <= 189:
		d -= 80
	case d >= 190 && d <= 199:
		d -= 190
	}
	return d
}

func micEMessage(bits [3]int) string {
	std, custom, v := false, false, 0
	for _, b := range bits {
		v <<= 1
		switch b {
		case 1:
			std, v = true, v|1
		case 2:
			custom, v = true, v|1
		}
	}
	switch {
	case std && custom:
		return "Unknown"
	case custom:
		return fmt.Sprintf("Custom-%d", 7-v)
	}
	return micEStandard[v]
}

// takeMicEAltitude removes a Mic-E altitude ("xxx}": metres above −10 km, in
// base 91) from the start of the comment, or after a one-byte radio type
// prefix such as Kenwood's '>' or ']'.
func (p *Position) takeMicEAltitude(c string) string {
	for _, i := range []int{0, 1} {
		if len(c) >= i+4 && c[i+3] == '}' {
			if n, ok := base91(c[i : i+3]); ok {
				p.HasAltitude = true
				p.Altitude = float64(n - 10000)
				return c[:i] + c[i+4:]
			}
		}
	}
	return c
}
