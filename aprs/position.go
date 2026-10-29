package aprs

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Symbol is an APRS symbol. Table is '/' (primary), '\\' (alternate) or an
// overlay character (A–Z, 0–9) drawn on the alternate symbol; Code picks the
// symbol within the table.
type Symbol struct {
	Table byte
	Code  byte
}

// Position is a decoded position.
type Position struct {
	Lat, Lon float64 // degrees, north and east positive
	// Ambiguity is how many trailing digits the sender blanked (0–4). Lat and
	// Lon are then the centre of the box those digits leave open.
	Ambiguity  int
	Symbol     Symbol
	Compressed bool

	HasCourseSpeed bool
	Course         int     // degrees, 1–360; 0 when unknown
	Speed          float64 // knots

	HasAltitude bool
	Altitude    float64 // metres
}

const feet = 0.3048 // metres

// ambCentre is half the size of the box left open by each ambiguity level,
// in minutes: blanking hundredths leaves 0.1′, tenths 1′, units 10′, tens 60′.
var ambCentre = [5]float64{0, 0.05, 0.5, 5, 30}

// ambStep is the same box in hundredths of a minute, for truncation.
var ambStep = [5]int{1, 10, 100, 1000, 6000}

// degrees turns degrees and hundredths of a minute into decimal degrees,
// applying an ambiguity level.
func degrees(deg, hundredths, amb int) float64 {
	hundredths -= hundredths % ambStep[amb]
	return float64(deg) + (float64(hundredths)/100+ambCentre[amb])/60
}

// ambiguous reads the digits DDMMhh of a latitude, where the sender may have
// blanked up to four trailing digits with spaces. It returns the degrees,
// the minutes in hundredths and the number of blanked digits.
func ambiguous(d [6]byte) (deg, hundredths, amb int, ok bool) {
	for i := 5; i >= 2 && d[i] == ' '; i-- {
		amb++
	}
	var v [6]int
	for i, c := range d {
		switch {
		case isDigit(c):
			v[i] = int(c - '0')
		case c == ' ' && i >= 6-amb:
		default:
			return 0, 0, 0, false
		}
	}
	return v[0]*10 + v[1], (v[2]*10+v[3])*100 + v[4]*10 + v[5], amb, true
}

func validTable(t byte) bool {
	return t == '/' || t == '\\' || t >= 'A' && t <= 'Z' || isDigit(t)
}

func validSymbolCode(c byte) bool { return c >= '!' && c <= '~' }

// parseUncompressed reads "DDMM.hhN/DDDMM.hhW-" (19 bytes: latitude 0–7,
// symbol table 8, longitude 9–17, symbol code 18) and an optional
// course/speed extension, and returns what follows.
func parseUncompressed(s string) (Position, string, error) {
	if len(s) < 19 {
		return Position{}, "", fmt.Errorf("%w: position is shorter than 19 characters", ErrMalformed)
	}
	if s[4] != '.' || s[14] != '.' {
		return Position{}, "", fmt.Errorf("%w: position %q has no decimal points where expected", ErrMalformed, s[:19])
	}
	deg, hun, amb, ok := ambiguous([6]byte{s[0], s[1], s[2], s[3], s[5], s[6]})
	if !ok || hun >= 6000 || deg > 90 {
		return Position{}, "", fmt.Errorf("%w: latitude %q", ErrMalformed, s[:8])
	}
	lat := degrees(deg, hun, amb)
	switch s[7] {
	case 'N':
	case 'S':
		lat = -lat
	default:
		return Position{}, "", fmt.Errorf("%w: latitude %q has no N or S", ErrMalformed, s[:8])
	}

	// The longitude takes the latitude's ambiguity (APRS101 chapter 6);
	// blanked longitude digits count as zero.
	if !allDigits(s[9:12]) {
		return Position{}, "", fmt.Errorf("%w: longitude %q", ErrMalformed, s[9:18])
	}
	lonDeg, _ := strconv.Atoi(s[9:12])
	lonHun := 0
	for _, c := range []byte{s[12], s[13], s[15], s[16]} {
		switch {
		case isDigit(c):
			lonHun = lonHun*10 + int(c-'0')
		case c == ' ':
			lonHun *= 10
		default:
			return Position{}, "", fmt.Errorf("%w: longitude %q", ErrMalformed, s[9:18])
		}
	}
	if lonHun >= 6000 || lonDeg > 180 {
		return Position{}, "", fmt.Errorf("%w: longitude %q", ErrMalformed, s[9:18])
	}
	lon := degrees(lonDeg, lonHun, amb)
	switch s[17] {
	case 'E':
	case 'W':
		lon = -lon
	default:
		return Position{}, "", fmt.Errorf("%w: longitude %q has no E or W", ErrMalformed, s[9:18])
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Position{}, "", fmt.Errorf("%w: position %q is out of range", ErrMalformed, s[:19])
	}

	pos := Position{Lat: lat, Lon: lon, Ambiguity: amb, Symbol: Symbol{Table: s[8], Code: s[18]}}
	if !validTable(pos.Symbol.Table) || !validSymbolCode(pos.Symbol.Code) {
		return Position{}, "", fmt.Errorf("%w: symbol %q%q", ErrMalformed, pos.Symbol.Table, pos.Symbol.Code)
	}
	rest := s[19:]
	// "ccc/sss" after a weather symbol is wind, not course and speed; it stays
	// in the comment for the weather fields.
	if pos.Symbol.Code != '_' && len(rest) >= 7 && rest[3] == '/' && allDigits(rest[:3]) && allDigits(rest[4:7]) {
		course, _ := strconv.Atoi(rest[:3])
		speed, _ := strconv.Atoi(rest[4:7])
		if course <= 360 {
			pos.HasCourseSpeed = true
			pos.Course = course
			pos.Speed = float64(speed)
			rest = rest[7:]
		}
	}
	return pos, rest, nil
}

// parseCompressed reads a compressed position (13 bytes: symbol table,
// latitude and longitude in base 91, symbol code, course/speed or altitude,
// and the compression type byte) and returns what follows.
func parseCompressed(s string) (Position, string, error) {
	if len(s) < 13 {
		return Position{}, "", fmt.Errorf("%w: compressed position is shorter than 13 characters", ErrMalformed)
	}
	table := s[0]
	switch {
	case table == '/' || table == '\\' || table >= 'A' && table <= 'Z':
	case table >= 'a' && table <= 'j': // overlay digits 0–9
		table = '0' + table - 'a'
	default:
		return Position{}, "", fmt.Errorf("%w: compressed symbol table %q", ErrMalformed, table)
	}
	y, okY := base91(s[1:5])
	x, okX := base91(s[5:9])
	if !okY || !okX {
		return Position{}, "", fmt.Errorf("%w: compressed position %q is not base 91", ErrMalformed, s[1:9])
	}
	pos := Position{
		Lat:        90 - float64(y)/380926,
		Lon:        -180 + float64(x)/190463,
		Symbol:     Symbol{Table: table, Code: s[9]},
		Compressed: true,
	}
	if pos.Lat < -90 || pos.Lat > 90 || pos.Lon < -180 || pos.Lon > 180 {
		return Position{}, "", fmt.Errorf("%w: compressed position %q is out of range", ErrMalformed, s[1:9])
	}
	if !validSymbolCode(pos.Symbol.Code) {
		return Position{}, "", fmt.Errorf("%w: symbol code %q", ErrMalformed, pos.Symbol.Code)
	}
	c, sp, t := s[10], s[11], s[12]
	if c != ' ' {
		if !isBase91(c) || !isBase91(sp) || !isBase91(t) {
			return Position{}, "", fmt.Errorf("%w: compressed extension %q is not base 91", ErrMalformed, s[10:13])
		}
		switch {
		case (t-33)>>3&3 == 2: // the fix came from a GGA sentence: cs is altitude
			pos.HasAltitude = true
			pos.Altitude = math.Pow(1.002, float64(int(c-33)*91+int(sp-33))) * feet
		case c-33 <= 89:
			pos.HasCourseSpeed = true
			pos.Course = int(c-33) * 4
			pos.Speed = math.Pow(1.08, float64(sp-33)) - 1
		}
		// c == '{' is a pre-calculated radio range, which is not decoded.
	}
	return pos, s[13:], nil
}

// takeAltitude removes a "/A=nnnnnn" altitude (feet) from a comment and
// records it.
func (p *Position) takeAltitude(comment string) string {
	i := strings.Index(comment, "/A=")
	if i < 0 || len(comment) < i+9 {
		return comment
	}
	f := comment[i+3 : i+9]
	if !allDigits(f) && !(f[0] == '-' && allDigits(f[1:])) {
		return comment
	}
	n, _ := strconv.Atoi(f)
	p.HasAltitude = true
	p.Altitude = float64(n) * feet
	return comment[:i] + comment[i+9:]
}

func isBase91(c byte) bool { return c >= '!' && c <= '{' }

// base91 decodes digits '!' (0) to '{' (90), most significant first.
func base91(s string) (int, bool) {
	n := 0
	for i := 0; i < len(s); i++ {
		if !isBase91(s[i]) {
			return 0, false
		}
		n = n*91 + int(s[i]-'!')
	}
	return n, true
}
