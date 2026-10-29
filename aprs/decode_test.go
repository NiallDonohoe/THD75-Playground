package aprs

import (
	"errors"
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// decodeInfo decodes an information field sent to dst.
func decodeInfo(t *testing.T, dst, info string) Data {
	t.Helper()
	d, err := Decode(Packet{Src: "EI2ABC-9", Dst: dst, Info: info})
	if err != nil {
		t.Fatalf("Decode(%q): %v", info, err)
	}
	return d
}

type wantPos struct {
	lat, lon     float64
	amb          int
	sym          string // table and code
	course       int
	speed        float64
	hasCourse    bool
	alt          float64
	hasAlt       bool
	compressed   bool
	comment      string
	messaging    bool
	timestamp    string
	positionTol  float64
	speedTol     float64
	altitudeTol  float64
	micE         string
	expectedKind Kind
	expectedName string
	expectedKill bool
}

func checkPos(t *testing.T, info string, d Data, w wantPos) {
	t.Helper()
	kind := w.expectedKind
	if kind == KindUnknown {
		kind = KindPosition
	}
	if d.Kind != kind {
		t.Fatalf("%q: Kind = %v, want %v", info, d.Kind, kind)
	}
	p := d.Position
	if p == nil {
		t.Fatalf("%q: no position", info)
	}
	tol := w.positionTol
	if tol == 0 {
		tol = 1e-6
	}
	if !near(p.Lat, w.lat, tol) || !near(p.Lon, w.lon, tol) {
		t.Errorf("%q: position %.6f, %.6f, want %.6f, %.6f", info, p.Lat, p.Lon, w.lat, w.lon)
	}
	if p.Ambiguity != w.amb {
		t.Errorf("%q: Ambiguity = %d, want %d", info, p.Ambiguity, w.amb)
	}
	if got := string([]byte{p.Symbol.Table, p.Symbol.Code}); got != w.sym {
		t.Errorf("%q: symbol %q, want %q", info, got, w.sym)
	}
	if p.Compressed != w.compressed {
		t.Errorf("%q: Compressed = %v", info, p.Compressed)
	}
	if p.HasCourseSpeed != w.hasCourse || p.Course != w.course || !near(p.Speed, w.speed, max(w.speedTol, 1e-9)) {
		t.Errorf("%q: course/speed %v %d %.2f, want %v %d %.2f", info, p.HasCourseSpeed, p.Course, p.Speed, w.hasCourse, w.course, w.speed)
	}
	if p.HasAltitude != w.hasAlt || !near(p.Altitude, w.alt, max(w.altitudeTol, 1e-6)) {
		t.Errorf("%q: altitude %v %.3f, want %v %.3f", info, p.HasAltitude, p.Altitude, w.hasAlt, w.alt)
	}
	if d.Comment != w.comment {
		t.Errorf("%q: Comment = %q, want %q", info, d.Comment, w.comment)
	}
	if d.Messaging != w.messaging {
		t.Errorf("%q: Messaging = %v", info, d.Messaging)
	}
	if d.Timestamp != w.timestamp {
		t.Errorf("%q: Timestamp = %q, want %q", info, d.Timestamp, w.timestamp)
	}
	if d.MicE != w.micE {
		t.Errorf("%q: MicE = %q, want %q", info, d.MicE, w.micE)
	}
	if d.Name != w.expectedName || d.Killed != w.expectedKill {
		t.Errorf("%q: Name, Killed = %q, %v, want %q, %v", info, d.Name, d.Killed, w.expectedName, w.expectedKill)
	}
}

// Examples from APRS101 chapter 8 (plain positions) and 9 (compressed).
func TestPlainAndCompressedPositions(t *testing.T) {
	const lat, lon = 49 + 3.50/60, -(72 + 1.75/60)
	cases := []struct {
		info string
		want wantPos
	}{
		{"!4903.50N/07201.75W-Test 001234", wantPos{lat: lat, lon: lon, sym: "/-", comment: "Test 001234"}},
		{"=4903.50N/07201.75W-", wantPos{lat: lat, lon: lon, sym: "/-", messaging: true}},
		{"@092345z4903.50N/07201.75W>088/036", wantPos{lat: lat, lon: lon, sym: "/>", hasCourse: true, course: 88, speed: 36, messaging: true, timestamp: "092345z"}},
		{"/092345z4903.50S\\07201.75E>", wantPos{lat: -lat, lon: -lon, sym: "\\>", timestamp: "092345z"}},
		{"!4903.50N/07201.75W-Test /A=001234", wantPos{lat: lat, lon: lon, sym: "/-", comment: "Test ", hasAlt: true, alt: 1234 * 0.3048}},
		{"!4903.50N/07201.75W-/A=-00012", wantPos{lat: lat, lon: lon, sym: "/-", hasAlt: true, alt: -12 * 0.3048}},
		{"!4903.50N/07201.75W-000/000", wantPos{lat: lat, lon: lon, sym: "/-", hasCourse: true}},
		{"!4903.50NE07201.75W#", wantPos{lat: lat, lon: lon, sym: "E#"}},

		// Ambiguity: blanked digits leave a box; the position is its centre,
		// and the longitude takes the latitude's level.
		{"!4903.5 N/07201.7 W-", wantPos{lat: 49 + 3.55/60, lon: -(72 + 1.75/60), amb: 1, sym: "/-"}},
		{"!4903.  N/07201.  W-", wantPos{lat: 49 + 3.5/60, lon: -(72 + 1.5/60), amb: 2, sym: "/-"}},
		{"!490 .  N/0720 .  W-", wantPos{lat: 49 + 5.0/60, lon: -(72 + 5.0/60), amb: 3, sym: "/-"}},
		{"!49  .  N/072  .  W-", wantPos{lat: 49.5, lon: -72.5, amb: 4, sym: "/-"}},
		{"!4903.  N/07201.75W-", wantPos{lat: 49 + 3.5/60, lon: -(72 + 1.5/60), amb: 2, sym: "/-"}},

		// A weather station's "ccc/sss" is wind, left in the comment.
		{"!4903.50N/07201.75W_220/004g005t077", wantPos{lat: lat, lon: lon, sym: "/_", comment: "220/004g005t077"}},

		// Compressed: "5L!!" is latitude 49.5, "<*e7" longitude −72.75, "7P"
		// course 88 and speed 1.08^47 − 1 knots.
		{"=/5L!!<*e7>7P[", wantPos{lat: 49.5, lon: -72.75, sym: "/>", compressed: true, hasCourse: true, course: 88, speed: 36.2, speedTol: 0.05, messaging: true, positionTol: 1e-5}},
		{"!/5L!!<*e7> sT", wantPos{lat: 49.5, lon: -72.75, sym: "/>", compressed: true, positionTol: 1e-5}},
		// Overlay digit 'a' means 0 on the alternate table; "S]" with a GGA
		// type byte is an altitude of 1.002^(50·91+60) feet.
		{"!a5L!!<*e7#S]1comment", wantPos{lat: 49.5, lon: -72.75, sym: "0#", compressed: true, hasAlt: true, alt: math.Pow(1.002, 50*91+60) * 0.3048, altitudeTol: 1e-6, comment: "comment", positionTol: 1e-5}},

		// A TNC's own beacon text before '!'.
		{"X1J4 beacon !4903.50N/07201.75Wn", wantPos{lat: lat, lon: lon, sym: "/n"}},
	}
	for _, c := range cases {
		checkPos(t, c.info, decodeInfo(t, "APRS", c.info), c.want)
	}
}

// Mic-E vectors worked by hand from APRS101 chapter 10.
//
// US2PUQ: digits 5 3 2 0 5 1 → 53°20.51′; U and S are standard 1 bits and 2 a
// 0 bit, so A B C = 1 1 0: En Route. P (4th) is north, U (5th) adds 100 to
// the longitude degrees, Q (6th) is west.
// Information field: '|' = 124 − 28 + 100 = 196 → 196 − 190 = 6°;
// '+' = 43 − 28 = 15′; 'X' = 88 − 28 = 60 hundredths.
// 'o' 'Z' 'O' = 83, 62, 51: speed 83·10 + 62/10 = 836 − 800 = 36 knots;
// course (62 mod 10)·100 + 51 = 251°. Symbol '>' on table '/'.
// Altitude "\"4T}": (1·91² + 19·91 + 51) − 10000 = 61 m.
//
// 3351R0: digits 3 3 5 1 2 0 → 33°51.20′; no message bits: Emergency. '1'
// (4th) is south, R adds 100, '0' (6th) is east.
// 'O' = 79 − 28 + 100 = 151°; '(' = 12′; 'D' = 40 hundredths.
// 'l' ' ' 'v' = 80, 4, 90: speed 800 + 0 − 800 = 0; course 4·100 + 90 − 400 = 90°.
func TestMicE(t *testing.T) {
	cases := []struct {
		dst, info string
		want      wantPos
	}{
		{"US2PUQ", "`|+XoZO>/\"4T}Hello", wantPos{lat: 53 + 20.51/60, lon: -(6 + 15.60/60), sym: "/>", hasCourse: true, course: 251, speed: 36, hasAlt: true, alt: 61, comment: "Hello", micE: "En Route"}},
		{"US2PUQ-2", "`|+XoZO>/>\"4T}Hi", wantPos{lat: 53 + 20.51/60, lon: -(6 + 15.60/60), sym: "/>", hasCourse: true, course: 251, speed: 36, hasAlt: true, alt: 61, comment: "Hi", micE: "En Route"}},
		{"3351R0", "'O(Dl v-/", wantPos{lat: -(33 + 51.20/60), lon: 151 + 12.40/60, sym: "/-", hasCourse: true, course: 90, micE: "Emergency"}},
		// Z in the 6th place is west and a blanked hundredths digit.
		{"US2PUZ", "`|+XoZO>/", wantPos{lat: 53 + 20.55/60, lon: -(6 + 15.65/60), amb: 1, sym: "/>", hasCourse: true, course: 251, speed: 36, micE: "En Route"}},
		// A, B, C (custom 1 bits) all set: Custom-0.
		{"FDCPUQ", "`|+XoZO>/", wantPos{lat: 53 + 20.51/60, lon: -(6 + 15.60/60), sym: "/>", hasCourse: true, course: 251, speed: 36, micE: "Custom-0"}},
		// A standard and a custom bit together.
		{"UDCPUQ", "`|+XoZO>/", wantPos{lat: 53 + 20.51/60, lon: -(6 + 15.60/60), sym: "/>", hasCourse: true, course: 251, speed: 36, micE: "Unknown"}},
	}
	for _, c := range cases {
		checkPos(t, c.info, decodeInfo(t, c.dst, c.info), c.want)
	}
}

func TestMicELongitudeDegrees(t *testing.T) {
	for _, c := range []struct {
		b      byte
		offset bool
		deg    int
	}{
		{118, true, 0}, {127, true, 9},
		{38, false, 10}, {127, false, 99},
		{108, true, 100}, {117, true, 109},
		{38, true, 110}, {107, true, 179},
	} {
		if got := micELonDegrees(c.b, c.offset); got != c.deg {
			t.Errorf("micELonDegrees(%d, %v) = %d, want %d", c.b, c.offset, got, c.deg)
		}
	}
}

func TestObjectsAndItems(t *testing.T) {
	const lat, lon = 49 + 3.50/60, -(72 + 1.75/60)
	cases := []struct {
		info string
		want wantPos
	}{
		{";LEADER   *092345z4903.50N/07201.75W>088/036", wantPos{lat: lat, lon: lon, sym: "/>", hasCourse: true, course: 88, speed: 36, timestamp: "092345z", expectedKind: KindObject, expectedName: "LEADER"}},
		{";LEADER   _092345z4903.50N/07201.75W>", wantPos{lat: lat, lon: lon, sym: "/>", timestamp: "092345z", expectedKind: KindObject, expectedName: "LEADER", expectedKill: true}},
		{")AID #2!4903.50N/07201.75WA", wantPos{lat: lat, lon: lon, sym: "/A", expectedKind: KindItem, expectedName: "AID #2"}},
		{")G/WB4APR_4903.50N/07201.75WA", wantPos{lat: lat, lon: lon, sym: "/A", expectedKind: KindItem, expectedName: "G/WB4APR", expectedKill: true}},
	}
	for _, c := range cases {
		checkPos(t, c.info, decodeInfo(t, "APRS", c.info), c.want)
	}
}

func TestStatusAndThirdParty(t *testing.T) {
	d := decodeInfo(t, "APRS", ">Net Control Center")
	if d.Kind != KindStatus || d.Status != "Net Control Center" || d.Timestamp != "" {
		t.Errorf("status: %+v", d)
	}
	d = decodeInfo(t, "APRS", ">092345zNet Control Center")
	if d.Kind != KindStatus || d.Status != "Net Control Center" || d.Timestamp != "092345z" {
		t.Errorf("status with time: %+v", d)
	}

	d = decodeInfo(t, "APRS", "}EI2DEF-7>APK005,TCPIP,EI7IG*:>On the air")
	if d.Kind != KindThirdParty || d.Inner == nil {
		t.Fatalf("third party: %+v", d)
	}
	if d.Inner.Src != "EI2DEF-7" || d.Inner.Dst != "APK005" || len(d.Inner.Path) != 2 || !d.Inner.Path[1].Used {
		t.Errorf("inner packet: %+v", *d.Inner)
	}
	inner, err := Decode(*d.Inner)
	if err != nil || inner.Kind != KindStatus || inner.Status != "On the air" {
		t.Errorf("inner decode: %+v, %v", inner, err)
	}
}

func TestUnknownTypes(t *testing.T) {
	for _, info := range []string{
		"T#005,199,000,255,073,123,01101001",
		"_10090556c220s004g005t077r000p000P000h50b09900wRSW",
		"!!0000000A00B20000",
		"?APRS?",
		"plain text with no position",
		"no position after the bang !nothing here",
	} {
		d, err := Decode(Packet{Src: "A", Dst: "APRS", Info: info})
		if err != nil || d.Kind != KindUnknown || d.Type != info[0] {
			t.Errorf("Decode(%q) = %v %q, %v; want unknown with type %q", info, d.Kind, d.Type, err, info[0])
		}
	}
	if d, err := Decode(Packet{Src: "A", Dst: "APRS"}); err != nil || d.Kind != KindUnknown {
		t.Errorf("empty info: %+v, %v", d, err)
	}
}

func TestDecodeMalformed(t *testing.T) {
	for _, c := range []struct{ dst, info string }{
		{"APRS", "!4903.50N/0720"},
		{"APRS", "!4903.50X/07201.75W-"},
		{"APRS", "!9903.50N/07201.75W-"},
		{"APRS", "!4963.50N/07201.75W-"},
		{"APRS", "!4903.50N/18101.75W-"},
		{"APRS", "!4903.50N/07201.75Q-"},
		{"APRS", "!4903.50Nx07201.75W-"},
		{"APRS", "!49 3.50N/07201.75W-"},
		{"APRS", "/12345"},
		{"APRS", "@092345x4903.50N/07201.75W>"},
		{"APRS", "="},
		{"APRS", "!/5L!!<*e"},
		{"APRS", "!x5L!!<*e7>7P["},
		{"APRS", ";SHORT*092345z"},
		{"APRS", ";LEADER   *0923"},
		{"APRS", ")AB!4903.50N/07201.75WA"},
		{"APRS", ":WU2Z:hello"},
		{"APRS", "}not a packet"},
		{"US2PU", "`|+XoZO>/"},
		{"US2AUQ", "`|+XoZO>/"},
		{"US2PUQ", "`|+XoZ"},
		{"US2PUQ", "`|\x01XoZO>/"},
	} {
		if _, err := Decode(Packet{Src: "A", Dst: c.dst, Info: c.info}); !errors.Is(err, ErrMalformed) {
			t.Errorf("Decode(%q to %q) error = %v, want ErrMalformed", c.info, c.dst, err)
		}
	}
}

func TestKindString(t *testing.T) {
	if KindMessage.String() != "message" || Kind(99).String() != "Kind(99)" {
		t.Errorf("Kind.String: %q %q", KindMessage, Kind(99))
	}
}
