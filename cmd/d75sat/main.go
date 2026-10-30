// Command d75sat helps listen to amateur satellites on the TH-D75: pass
// predictions, a map with ground tracks and coverage, and Doppler tuning
// of band B. See docs/SAT.md.
//
// So far it predicts passes from an element file ("passes"). The page and
// the network data arrive in milestone S1, Doppler tuning in S2.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"d75scope/orbit"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "d75sat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "passes" {
		fmt.Fprintf(os.Stderr, "usage: d75sat passes [flags] ELEMENTS-FILE\n\nRun \"d75sat passes -h\" for the flags.\n")
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
			return nil
		}
		return errors.New("no command")
	}
	fs := flag.NewFlagSet("passes", flag.ExitOnError)
	lat := fs.Float64("lat", 0, "station latitude, degrees north")
	lon := fs.Float64("lon", 0, "station longitude, degrees east")
	alt := fs.Float64("alt", 0, "station height, metres")
	hours := fs.Float64("hours", 24, "how far ahead to look")
	minEl := fs.Float64("min-el", 0, "lowest peak elevation to list, degrees")
	name := fs.String("sat", "", "only satellites whose name or catalogue number contains this")
	freq := fs.Float64("freq", 0, "downlink in MHz: also show the Doppler-shifted frequency at AOS, TCA and LOS")
	from := fs.String("from", "", "start time, RFC 3339 (default now)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75sat passes [flags] ELEMENTS-FILE\n\n"+
			"ELEMENTS-FILE holds TLEs (two- or three-line) or CelesTrak OMM JSON.\n"+
			"Times are UTC; azimuths are degrees from true north.\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args[1:])
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("give one elements file")
	}
	if *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 || (*lat == 0 && *lon == 0) {
		return errors.New("--lat and --lon must give your station's position")
	}
	start := time.Now().UTC()
	if *from != "" {
		t, err := time.Parse(time.RFC3339, *from)
		if err != nil {
			return fmt.Errorf("--from: %v", err)
		}
		start = t.UTC()
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var els []orbit.Elements
	var errs []error
	if t := strings.TrimSpace(string(data)); strings.HasPrefix(t, "[") {
		els, errs = orbit.ParseOMMJSON(data)
	} else {
		els, errs = orbit.ParseTLEs(string(data))
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "d75sat: skipped:", e)
	}

	obs := orbit.Observer{Lat: *lat, Lon: *lon, AltM: *alt}
	type row struct {
		sat *orbit.Satellite
		p   orbit.Pass
	}
	var rows []row
	for _, e := range els {
		if *name != "" && !strings.Contains(strings.ToUpper(e.Name), strings.ToUpper(*name)) && e.Catalog != *name {
			continue
		}
		s, err := orbit.New(e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "d75sat: skipped %s: %v\n", e.Name, err)
			continue
		}
		if age := start.Sub(e.Epoch); age > 14*24*time.Hour || age < -14*24*time.Hour {
			fmt.Fprintf(os.Stderr, "d75sat: %s: elements are %.0f days from the start time; predictions will be poor\n", e.Name, age.Hours()/24)
		}
		ps, err := s.Passes(obs, start, start.Add(time.Duration(*hours*float64(time.Hour))), 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "d75sat: %s: %v\n", e.Name, err)
		}
		for _, p := range ps {
			if p.MaxEl >= *minEl {
				rows = append(rows, row{s, p})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].p.AOS.Before(rows[j].p.AOS) })
	fmt.Printf("%-20s %-19s %8s %6s %5s %6s\n", "SATELLITE", "AOS (UTC)", "LENGTH", "AOS AZ", "MAX", "LOS AZ")
	for _, r := range rows {
		p := r.p
		aos := p.AOS.Format("2006-01-02 15:04:05")
		if p.InProgress {
			aos = "now (" + p.AOS.Format("15:04:05") + ")"
		}
		fmt.Printf("%-20s %-19s %8s %5.0f° %4.0f° %5.0f°", trim(r.sat.Name, 20), aos,
			p.Duration().Round(time.Second), p.AOSAz, p.MaxEl, p.LOSAz)
		if *freq > 0 {
			fmt.Print("  ")
			for _, t := range []time.Time{p.AOS, p.TCA, p.LOS} {
				l, _ := r.sat.Look(obs, t)
				fmt.Printf(" %.4f", orbit.Doppler(*freq*1e6, l.RangeRate)/1e6)
			}
			fmt.Print(" MHz")
		}
		fmt.Println()
	}
	return nil
}

func trim(s string, n int) string {
	if s == "" {
		return "?"
	}
	if len(s) > n {
		return s[:n]
	}
	return s
}
