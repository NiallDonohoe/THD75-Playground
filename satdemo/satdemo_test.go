package satdemo

import (
	"testing"
	"time"

	"d75scope/orbit"
)

func TestDemoCatalog(t *testing.T) {
	station := orbit.Observer{Lat: 53.35, Lon: -6.26}
	start := time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)
	t0 := time.Now()
	cat := Catalog(station, start)
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("building the demo took %v", d)
	}
	if len(cat) != 5 {
		t.Fatalf("%d satellites", len(cat))
	}
	for _, s := range cat {
		if _, err := orbit.New(s.Elements); err != nil {
			t.Errorf("%s: %v", s.Name, err)
		}
		if len(s.Transmitters) == 0 || s.Transmitters[0].ID == "" {
			t.Errorf("%s: transmitters %+v", s.Name, s.Transmitters)
		}
	}
	s, _ := orbit.New(cat[0].Elements)
	ps, _ := s.Passes(station, start, start.Add(30*time.Minute), 0)
	if len(ps) == 0 {
		t.Fatal("DEMOSAT-1 has no pass in the first half hour")
	}
	if w := ps[0].AOS.Sub(start); w < time.Minute || w > 3*time.Minute || ps[0].MaxEl < 30 {
		t.Errorf("first pass rises after %v and peaks at %.0f°", w, ps[0].MaxEl)
	}
	// Everywhere gets a first pass, not just Dublin.
	for _, o := range []orbit.Observer{{Lat: -33.9, Lon: 151.2}, {Lat: 41.7, Lon: -72.7}, {Lat: 0, Lon: 0}} {
		s, _ := orbit.New(Catalog(o, start)[0].Elements)
		if ps, _ := s.Passes(o, start, start.Add(5*time.Minute), 0); len(ps) == 0 {
			t.Errorf("no early pass at %+v", o)
		}
	}
}
