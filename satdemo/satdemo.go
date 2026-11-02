// Package satdemo makes up a catalogue for d75sat --demo: five satellites
// with realistic orbits and downlinks, but invented names and numbers
// (DEMOSAT, catalogue 99901 on), and elements fresh at the demo's start.
// The first is placed so that a high pass over the station begins about
// two minutes in, so there is something to watch straight away. Nothing
// here touches the network.
package satdemo

import (
	"fmt"
	"math"
	"time"

	"d75scope/orbit"
	"d75scope/satdata"
)

const mu = 398600.8 // km³/s², WGS-72, as SGP4

// revsPerDay is the mean motion of a circular orbit at altKm.
func revsPerDay(altKm float64) float64 {
	a := 6378.135 + altKm
	return math.Sqrt(mu/(a*a*a)) * 86400 / (2 * math.Pi)
}

type plan struct {
	name     string
	altKm    float64
	incl     float64
	raan, ma float64
	tx       []satdata.Transmitter
}

func fm(desc string, hz float64, mode string) satdata.Transmitter {
	return satdata.Transmitter{Description: desc, Downlink: hz, Mode: mode, RadioMode: "FM", Kind: "Transmitter"}
}

var plans = []plan{
	{name: "DEMOSAT-1", altKm: 420, incl: 51.6, tx: []satdata.Transmitter{
		fm("FM voice repeater", 145.800e6, "FM"), fm("APRS digipeater", 145.825e6, "AFSK")}},
	{name: "DEMOSAT-2", altKm: 550, incl: 97.6, raan: 120, ma: 200, tx: []satdata.Transmitter{
		fm("FM voice repeater", 436.795e6, "FM"), fm("Telemetry, 9k6", 435.350e6, "GMSK")}},
	{name: "DEMOSAT-3", altKm: 1180, incl: 82.5, raan: 250, ma: 40, tx: []satdata.Transmitter{
		{Description: "CW beacon", Downlink: 435.605e6, Mode: "CW", RadioMode: "CW", Kind: "Transmitter"},
		{Description: "Linear transponder, inverting", Downlink: 435.610e6, DownlinkTop: 435.670e6,
			Uplink: 145.935e6, UplinkTop: 145.995e6, Mode: "SSB", RadioMode: "USB", Invert: true, Kind: "Transponder"}}},
	{name: "DEMOSAT-4", altKm: 1450, incl: 101.5, raan: 330, ma: 300, tx: []satdata.Transmitter{
		{Description: "Linear transponder", Downlink: 145.925e6, DownlinkTop: 145.975e6, Mode: "USB", RadioMode: "USB", Kind: "Transponder"},
		{Description: "CW beacon", Downlink: 145.9775e6, Mode: "CW", RadioMode: "CW", Kind: "Transmitter"}}},
	{name: "DEMOSAT-5", altKm: 500, incl: 97.4, raan: 40, ma: 90, tx: []satdata.Transmitter{
		fm("Telemetry, 1k2", 437.150e6, "AFSK")}},
}

func elements(p plan, i int, epoch time.Time) orbit.Elements {
	return orbit.Elements{
		Name: p.name, Catalog: fmt.Sprint(99901 + i), Epoch: epoch,
		MeanMotion: revsPerDay(p.altKm), Ecc: 0.0005, Incl: p.incl,
		RAAN: p.raan, ArgPerigee: 90, MeanAnomaly: p.ma, BStar: 2e-5,
	}
}

// Catalog returns the demo catalogue for a station, with elements whose
// epoch is start.
func Catalog(station orbit.Observer, start time.Time) []satdata.Satellite {
	var out []satdata.Satellite
	for i, p := range plans {
		if i == 0 {
			p.raan, p.ma = firstPass(p, station, start)
		}
		e := elements(p, i, start)
		txs := make([]satdata.Transmitter, len(p.tx))
		for j, t := range p.tx {
			t.ID = fmt.Sprintf("demo-%d-%d", i+1, j+1)
			txs[j] = t
		}
		out = append(out, satdata.Satellite{ID: e.Catalog, Name: e.Name, Elements: e,
			Epoch: e.Epoch.Format("2006-01-02T15:04:05Z"), Transmitters: txs})
	}
	return out
}

// firstPass searches the node and the position in the orbit for a pass
// that rises 1–3 minutes after start and peaks above 45°.
func firstPass(p plan, station orbit.Observer, start time.Time) (raan, ma float64) {
	best, bestEl := [2]float64{}, -1.0
	for r := 0.0; r < 360; r += 5 {
		for m := 0.0; m < 360; m += 3 {
			p.raan, p.ma = r, m
			s, err := orbit.New(elements(p, 0, start))
			if err != nil {
				continue
			}
			ps, _ := s.Passes(station, start, start.Add(20*time.Minute), 0)
			if len(ps) == 0 || ps[0].InProgress {
				continue
			}
			wait := ps[0].AOS.Sub(start)
			if wait < time.Minute || wait > 3*time.Minute {
				continue
			}
			if ps[0].MaxEl > 45 {
				return r, m
			}
			if ps[0].MaxEl > bestEl {
				best, bestEl = [2]float64{r, m}, ps[0].MaxEl
			}
		}
	}
	return best[0], best[1]
}
