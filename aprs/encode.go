package aprs

import (
	"fmt"
	"math"
)

// maxPositionComment is the longest comment after a plain position, counting
// the course/speed and altitude extensions (APRS101 chapter 8).
const maxPositionComment = 43

// PositionInfo formats a plain position report without a timestamp: '=' when
// messaging is true, '!' otherwise. Course and speed, and the altitude, are
// included when set. Ambiguity is not applied.
func PositionInfo(p Position, comment string, messaging bool) (string, error) {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.Abs(p.Lat) > 90 || math.Abs(p.Lon) > 180 {
		return "", fmt.Errorf("position %v, %v is out of range", p.Lat, p.Lon)
	}
	if !validTable(p.Symbol.Table) || !validSymbolCode(p.Symbol.Code) {
		return "", fmt.Errorf("symbol %q%q is not valid", p.Symbol.Table, p.Symbol.Code)
	}
	for i := 0; i < len(comment); i++ {
		if c := comment[i]; c < ' ' || c > '~' || c == '|' || c == '~' {
			return "", fmt.Errorf("comment may not contain %q", c)
		}
	}
	b := []byte{'!'}
	if messaging {
		b[0] = '='
	}
	b = appendCoordinate(b, p.Lat, 2, 'N', 'S')
	b = append(b, p.Symbol.Table)
	b = appendCoordinate(b, p.Lon, 3, 'E', 'W')
	b = append(b, p.Symbol.Code)
	ext := len(b)
	if p.HasCourseSpeed {
		speed := math.Round(p.Speed)
		if p.Course < 0 || p.Course > 360 || speed < 0 || speed > 999 {
			return "", fmt.Errorf("course %d or speed %v knots is out of range", p.Course, p.Speed)
		}
		b = fmt.Appendf(b, "%03d/%03d", p.Course, int(speed))
	}
	if p.HasAltitude {
		ft := int(math.Round(p.Altitude / feet))
		switch {
		case ft >= 0 && ft <= 999999:
			b = fmt.Appendf(b, "/A=%06d", ft)
		case ft < 0 && ft >= -99999:
			b = fmt.Appendf(b, "/A=-%05d", -ft)
		default:
			return "", fmt.Errorf("altitude %v m is out of range", p.Altitude)
		}
	}
	b = append(b, comment...)
	if n := len(b) - ext; n > maxPositionComment {
		return "", fmt.Errorf("comment and extensions are %d characters, at most %d fit", n, maxPositionComment)
	}
	return string(b), nil
}

// appendCoordinate formats degrees as DDMM.hh (latitude, 2 degree digits) or
// DDDMM.hh (longitude, 3), rounding to the nearest hundredth of a minute.
func appendCoordinate(b []byte, v float64, degDigits int, pos, neg byte) []byte {
	hemi := pos
	if v < 0 {
		hemi = neg
	}
	h := int(math.Round(math.Abs(v) * 6000))
	return fmt.Appendf(b, "%0*d%02d.%02d%c", degDigits, h/6000, h%6000/100, h%100, hemi)
}
