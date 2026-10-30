package orbit

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// The ISS element set used as the example on Wikipedia's TLE page.
const (
	issName = "ISS (ZARYA)"
	issL1   = "1 25544U 98067A   08264.51782528 -.00002182  00000-0 -11606-4 0  2927"
	issL2   = "2 25544  51.6416 247.4627 0006703 130.5360 325.0288 15.72125391563537"
)

var dublin = Observer{Lat: 53.3498, Lon: -6.2603, AltM: 20}

func iss(t *testing.T) *Satellite {
	t.Helper()
	e, err := ParseTLE(issName, issL1, issL2)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(e)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSGP4MatchesVallado(t *testing.T) {
	for _, c := range valladoCases {
		e, err := ParseTLE("", c.line1, c.line2)
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Catalog, err)
		}
		for _, r := range c.rows {
			p, v, err := s.propagate(r[0])
			if err != nil {
				t.Fatalf("%s at %g min: %v", e.Catalog, r[0], err)
			}
			dp := Vec{r[1], r[2], r[3]}.Sub(p).Norm()
			dv := Vec{r[4], r[5], r[6]}.Sub(v).Norm()
			if dp > 1e-5 || dv > 1e-7 {
				t.Errorf("%s at %g min: off by %.2g km, %.2g km/s", e.Catalog, r[0], dp, dv)
			}
		}
	}
}

func TestDeepSpaceRefused(t *testing.T) {
	// Vallado's 04632, a 20-hour orbit.
	e, err := ParseTLE("",
		"1 04632U 70093B   04031.91070959 -.00000084  00000-0  10000-3 0  9955",
		"2 04632  11.4628 273.1101 1450506 207.6000 143.9350  1.20231981 44145")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(e); !errors.Is(err, ErrDeepSpace) {
		t.Errorf("got %v, want ErrDeepSpace", err)
	}
}

func TestParseTLE(t *testing.T) {
	e, err := ParseTLE("0 "+issName, issL1, issL2)
	if err != nil {
		t.Fatal(err)
	}
	want := Elements{
		Name: issName, Catalog: "25544",
		Epoch:      time.Date(2008, 9, 20, 12, 25, 40, 104192000, time.UTC),
		MeanMotion: 15.72125391, Ecc: 0.0006703, Incl: 51.6416, RAAN: 247.4627,
		ArgPerigee: 130.5360, MeanAnomaly: 325.0288, BStar: -0.11606e-4,
		NDot: -0.00002182, RevNumber: 56353,
	}
	if d := e.Epoch.Sub(want.Epoch); d < -time.Microsecond || d > time.Microsecond {
		t.Errorf("epoch %v, want %v", e.Epoch, want.Epoch)
	}
	e.Epoch = want.Epoch
	if math.Abs(e.BStar-want.BStar) > 1e-15 {
		t.Errorf("bstar %g, want %g", e.BStar, want.BStar)
	}
	e.BStar = want.BStar
	if e != want {
		t.Errorf("got %+v\nwant %+v", e, want)
	}
	nan := Elements{MeanMotion: math.NaN(), Incl: 51}
	if err := nan.check(); err == nil {
		t.Error("NaN mean motion accepted")
	}
	bad := issL1[:20] + "9" + issL1[21:]
	if _, err := ParseTLE("", bad, issL2); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("corrupted line: %v, want a checksum error", err)
	}
}

func TestParseTLEs(t *testing.T) {
	text := issName + "\r\n" + issL1 + "\r\n" + issL2 + "\r\n\r\n" +
		issL1 + "\n" + issL2 + "\n" +
		"BROKEN\n" + issL1[:68] + "0\n" + issL2 + "\n"
	els, errs := ParseTLEs(text)
	if len(els) != 2 || els[0].Name != issName || els[1].Name != "" {
		t.Errorf("got %d sets: %+v", len(els), els)
	}
	if len(errs) != 1 {
		t.Errorf("errors %v, want one checksum error", errs)
	}
}

// The same elements as OMM JSON (CelesTrak's FORMAT=json) must propagate
// identically.
func TestParseOMMJSON(t *testing.T) {
	js := `[{"OBJECT_NAME":"ISS (ZARYA)","OBJECT_ID":"1998-067A","EPOCH":"2008-09-20T12:25:40.104192",
	"MEAN_MOTION":15.72125391,"ECCENTRICITY":0.0006703,"INCLINATION":51.6416,"RA_OF_ASC_NODE":247.4627,
	"ARG_OF_PERICENTER":130.536,"MEAN_ANOMALY":325.0288,"EPHEMERIS_TYPE":0,"CLASSIFICATION_TYPE":"U",
	"NORAD_CAT_ID":25544,"ELEMENT_SET_NO":292,"REV_AT_EPOCH":56353,"BSTAR":-1.1606e-5,
	"MEAN_MOTION_DOT":-2.182e-5,"MEAN_MOTION_DDOT":0},
	{"OBJECT_NAME":"BAD","NORAD_CAT_ID":1,"EPOCH":"yesterday"}]`
	els, errs := ParseOMMJSON([]byte(js))
	if len(els) != 1 || len(errs) != 1 {
		t.Fatalf("got %d sets and errors %v", len(els), errs)
	}
	s, err := New(els[0])
	if err != nil {
		t.Fatal(err)
	}
	ref := iss(t)
	at := ref.Epoch.Add(90 * time.Minute)
	p1, _, _ := s.Propagate(at)
	p2, _, _ := ref.Propagate(at)
	if d := p1.Sub(p2).Norm(); d > 1e-3 {
		t.Errorf("OMM and TLE positions differ by %.3g km", d)
	}
	if els[0].Catalog != "25544" {
		t.Errorf("catalog %q", els[0].Catalog)
	}
}

// Vallado, Fundamentals of Astrodynamics, example 3-5.
func TestGMST(t *testing.T) {
	g := gmst(time.Date(1992, 8, 20, 12, 14, 0, 0, time.UTC)) / deg
	if math.Abs(g-152.578787886) > 1e-5 {
		t.Errorf("GMST %.9f°, want 152.578787886°", g)
	}
}

func TestGeodeticRoundTrip(t *testing.T) {
	if p := geoToECEF(Geo{0, 0, 0}); p.Sub(Vec{wgs84A, 0, 0}).Norm() > 1e-9 {
		t.Errorf("equator at Greenwich: %v", p)
	}
	for _, g := range []Geo{{53.35, -6.26, 0.02}, {-33.9, 151.2, 0}, {89.9, 45, 420}, {-60, -179.5, 800}} {
		back := ecefToGeo(geoToECEF(g))
		if math.Abs(back.Lat-g.Lat) > 1e-8 || math.Abs(back.Lon-g.Lon) > 1e-8 || math.Abs(back.Alt-g.Alt) > 1e-6 {
			t.Errorf("%+v came back as %+v", g, back)
		}
	}
}

func TestLookGeometry(t *testing.T) {
	o := dublin
	up := geoToECEF(Geo{o.Lat, o.Lon, o.AltM/1000 + 500})
	l := lookFrom(o, up, Vec{})
	if math.Abs(l.El-90) > 0.01 || math.Abs(l.Range-500) > 0.01 {
		t.Errorf("overhead: %+v", l)
	}
	// A point far to the north-east, at the same height, is low in the
	// north-east and moving away when given a velocity away from us.
	ne := geoToECEF(Geo{o.Lat + 5, o.Lon + 8, 0})
	away := ne.Sub(o.ecef())
	l = lookFrom(o, ne, away.Scale(1/away.Norm()))
	if l.Az < 30 || l.Az > 60 || l.El > 0 || math.Abs(l.RangeRate-1) > 1e-9 {
		t.Errorf("north-east: %+v", l)
	}
}

// Passes must agree with a brute-force scan, second by second.
func TestPassesMatchScan(t *testing.T) {
	s := iss(t)
	from := s.Epoch
	to := from.Add(24 * time.Hour)
	passes, err := s.Passes(dublin, from, to, 0)
	if err != nil {
		t.Fatal(err)
	}
	type span struct {
		aos, los time.Time
		max      float64
	}
	var scan []span
	var cur *span
	for tt := from; tt.Before(to); tt = tt.Add(time.Second) {
		l, _ := s.Look(dublin, tt)
		switch {
		case l.El > 0 && cur == nil:
			cur = &span{aos: tt, max: l.El}
		case l.El > 0:
			cur.max = math.Max(cur.max, l.El)
		case cur != nil:
			cur.los = tt
			scan = append(scan, *cur)
			cur = nil
		}
	}
	if len(passes) != len(scan) || len(scan) < 3 {
		t.Fatalf("%d passes, scan found %d", len(passes), len(scan))
	}
	for i, p := range passes {
		w := scan[i]
		if d := p.AOS.Sub(w.aos).Seconds(); math.Abs(d) > 1.5 {
			t.Errorf("pass %d AOS off by %.1f s", i, d)
		}
		if d := p.LOS.Sub(w.los).Seconds(); math.Abs(d) > 1.5 {
			t.Errorf("pass %d LOS off by %.1f s", i, d)
		}
		if math.Abs(p.MaxEl-w.max) > 0.05 {
			t.Errorf("pass %d max elevation %.3f°, scan %.3f°", i, p.MaxEl, w.max)
		}
		if !p.TCA.After(p.AOS) || !p.TCA.Before(p.LOS) {
			t.Errorf("pass %d TCA %v outside %v–%v", i, p.TCA, p.AOS, p.LOS)
		}
	}
}

func TestPassInProgress(t *testing.T) {
	s := iss(t)
	all, _ := s.Passes(dublin, s.Epoch, s.Epoch.Add(24*time.Hour), 0)
	mid := all[0].TCA
	ps, err := s.Passes(dublin, mid, mid.Add(time.Hour), 0)
	if err != nil || len(ps) == 0 || !ps[0].InProgress {
		t.Fatalf("from mid-pass: %+v, %v", ps, err)
	}
	if d := ps[0].AOS.Sub(all[0].AOS).Seconds(); math.Abs(d) > 1 {
		t.Errorf("AOS found looking back is off by %.1f s", d)
	}
}

func TestDoppler(t *testing.T) {
	// 7 km/s receding at 435 MHz: about −10.16 kHz.
	if got := Doppler(435e6, 7) - 435e6; math.Abs(got+10157.1) > 1 {
		t.Errorf("shift %.1f Hz", got)
	}
	// Over a real pass the shift goes from high to low, through zero at TCA.
	s := iss(t)
	p, _ := s.Passes(dublin, s.Epoch, s.Epoch.Add(24*time.Hour), 10)
	la, _ := s.Look(dublin, p[0].AOS)
	lt, _ := s.Look(dublin, p[0].TCA)
	ll, _ := s.Look(dublin, p[0].LOS)
	if la.RangeRate > -1 || ll.RangeRate < 1 || math.Abs(lt.RangeRate) > 0.05 {
		t.Errorf("range rates at AOS, TCA, LOS: %.2f %.3f %.2f km/s", la.RangeRate, lt.RangeRate, ll.RangeRate)
	}
}

func TestFootprint(t *testing.T) {
	s := iss(t)
	at := s.Epoch.Add(40 * time.Minute)
	g, err := s.SubPoint(at)
	if err != nil {
		t.Fatal(err)
	}
	r0 := FootprintRadius(g.Alt, 0)
	if g.Alt < 300 || g.Alt > 450 || r0 < 1900 || r0 > 2400 {
		t.Fatalf("height %.0f km, radio horizon %.0f km", g.Alt, r0)
	}
	if FootprintRadius(g.Alt, 10) >= r0 {
		t.Error("a 10° footprint is not smaller than the horizon")
	}
	// From a point on the 10° edge, the satellite is at about 10°
	// (a spherical-Earth drawing, so within half a degree).
	for _, pt := range Footprint(g, 10, 12) {
		l, _ := s.Look(Observer{Lat: pt[0], Lon: pt[1]}, at)
		if math.Abs(l.El-10) > 0.5 {
			t.Errorf("at %.2f,%.2f the satellite is at %.2f°, want 10°", pt[0], pt[1], l.El)
		}
		if pt[1] < -180 || pt[1] >= 180 {
			t.Errorf("longitude %g not normalised", pt[1])
		}
	}
}

func TestGroundTrack(t *testing.T) {
	s := iss(t)
	track, err := s.GroundTrack(s.Epoch, s.Epoch.Add(92*time.Minute), time.Minute)
	if err != nil || len(track) != 93 {
		t.Fatalf("%d points, %v", len(track), err)
	}
	maxLat := 0.0
	for _, g := range track {
		maxLat = math.Max(maxLat, math.Abs(g.Lat))
	}
	// The ground track reaches the inclination, give or take.
	if math.Abs(maxLat-51.6) > 0.5 {
		t.Errorf("highest latitude %.2f°, want about 51.6°", maxLat)
	}
}
