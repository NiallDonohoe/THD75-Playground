package orbit

import (
	"math"
	"time"
)

// Pass is one pass over a station, above a minimum elevation.
type Pass struct {
	AOS, TCA, LOS time.Time // rise, highest point, set
	AOSAz, LOSAz  float64   // degrees
	MaxEl         float64   // degrees, at TCA
	// InProgress means the pass had already begun at the start of the
	// search and AOS was found by looking back.
	InProgress bool
}

// Duration is how long the satellite is above the minimum elevation.
func (p Pass) Duration() time.Duration { return p.LOS.Sub(p.AOS) }

// passStep is the coarse search step. The shortest useful LEO passes last
// a few minutes; a pass that never rises above the minimum for longer
// than this may be missed, which costs nothing worth hearing.
const passStep = 30 * time.Second

// Passes finds the passes over o between from and to that rise above
// minEl degrees. Edges are found to within 0.5 s and TCA to about 1 s.
func (s *Satellite) Passes(o Observer, from, to time.Time, minEl float64) ([]Pass, error) {
	el := func(t time.Time) (float64, error) {
		l, err := s.Look(o, t)
		return l.El - minEl, err
	}
	var out []Pass
	t := from
	e, err := el(t)
	if err != nil {
		return nil, err
	}
	inProgress := false
	if e > 0 {
		// Already up: walk back to the rise (at most half an orbit).
		back := t
		for i := 0; i < 120 && e > 0; i++ {
			back = back.Add(-passStep)
			if e, err = el(back); err != nil {
				return nil, err
			}
		}
		t, inProgress = back, true
	}
	for t.Before(to) {
		next := t.Add(passStep)
		en, err := el(next)
		if err != nil {
			return out, err
		}
		if e <= 0 && en > 0 {
			aos := edge(el, t, next, true)
			p, after, err := s.finishPass(o, el, aos, minEl)
			if err != nil {
				return out, err
			}
			p.InProgress = inProgress && len(out) == 0
			out = append(out, p)
			next = after
			if en, err = el(next); err != nil {
				return out, err
			}
		}
		t, e = next, en
	}
	return out, nil
}

// finishPass follows a pass from its AOS to LOS and returns it, with the
// time to resume the search from.
func (s *Satellite) finishPass(o Observer, el func(time.Time) (float64, error), aos time.Time, minEl float64) (Pass, time.Time, error) {
	t := aos
	// A near-Earth satellite is never up for longer than its period
	// (under 225 minutes), so following one longer means something is
	// wrong with the elements.
	for i := 0; i < int(225*time.Minute/passStep); i++ {
		next := t.Add(passStep)
		en, err := el(next)
		if err != nil {
			return Pass{}, next, err
		}
		if en <= 0 {
			los := edge(el, t, next, false)
			tca := peak(el, aos, los)
			p := Pass{AOS: aos, LOS: los, TCA: tca}
			la, _ := s.Look(o, aos)
			ll, _ := s.Look(o, los)
			lt, _ := s.Look(o, tca)
			p.AOSAz, p.LOSAz, p.MaxEl = la.Az, ll.Az, lt.El
			return p, next, nil
		}
		t = next
	}
	return Pass{}, t, ErrDiverged
}

// edge bisects for the moment el crosses zero between a and b.
func edge(el func(time.Time) (float64, error), a, b time.Time, rising bool) time.Time {
	for b.Sub(a) > 500*time.Millisecond {
		mid := a.Add(b.Sub(a) / 2)
		e, err := el(mid)
		if err != nil {
			break
		}
		if (e > 0) == rising {
			b = mid
		} else {
			a = mid
		}
	}
	return a.Add(b.Sub(a) / 2)
}

// peak finds the highest point between a and b by golden-section search;
// elevation over one pass has a single maximum.
func peak(el func(time.Time) (float64, error), a, b time.Time) time.Time {
	g := (math.Sqrt(5) - 1) / 2
	at := func(f float64) time.Time { return a.Add(time.Duration(f * float64(b.Sub(a)))) }
	lo, hi := 0.0, 1.0
	x1, x2 := hi-g*(hi-lo), lo+g*(hi-lo)
	f1, _ := el(at(x1))
	f2, _ := el(at(x2))
	for (hi-lo)*b.Sub(a).Seconds() > 0.5 {
		if f1 < f2 {
			lo, x1, f1 = x1, x2, f2
			x2 = lo + g*(hi-lo)
			f2, _ = el(at(x2))
		} else {
			hi, x2, f2 = x2, x1, f1
			x1 = hi - g*(hi-lo)
			f1, _ = el(at(x1))
		}
	}
	return at((lo + hi) / 2)
}

// Footprint returns n points (latitude, longitude in degrees, longitudes
// in −180…180) around the area from which a satellite at g is above
// minEl degrees: for minEl 0, its radio horizon. A spherical Earth is
// accurate enough for drawing.
func Footprint(g Geo, minEl float64, n int) [][2]float64 {
	r := FootprintRadius(g.Alt, minEl) / wgs84A // angular radius, rad
	lat0, lon0 := g.Lat*deg, g.Lon*deg
	out := make([][2]float64, n)
	for i := range out {
		b := 2 * math.Pi * float64(i) / float64(n)
		lat := math.Asin(math.Sin(lat0)*math.Cos(r) + math.Cos(lat0)*math.Sin(r)*math.Cos(b))
		lon := lon0 + math.Atan2(math.Sin(b)*math.Sin(r)*math.Cos(lat0), math.Cos(r)-math.Sin(lat0)*math.Sin(lat))
		out[i] = [2]float64{lat / deg, normLon(lon / deg)}
	}
	return out
}

// FootprintRadius is the ground distance (km) from the sub-satellite point
// to the edge of the area where the satellite, at altKm, is above minEl
// degrees.
func FootprintRadius(altKm, minEl float64) float64 {
	e := minEl * deg
	return wgs84A * (math.Acos(wgs84A*math.Cos(e)/(wgs84A+altKm)) - e)
}

func normLon(l float64) float64 {
	l = math.Mod(l+180, 360)
	if l < 0 {
		l += 360
	}
	return l - 180
}

// GroundTrack returns the sub-satellite points from from to to, one every
// step.
func (s *Satellite) GroundTrack(from, to time.Time, step time.Duration) ([]Geo, error) {
	var out []Geo
	for t := from; !t.After(to); t = t.Add(step) {
		g, err := s.SubPoint(t)
		if err != nil {
			return out, err
		}
		out = append(out, g)
	}
	return out, nil
}
