// Command d75sat helps listen to amateur satellites on the TH-D75: pass
// predictions, a map with ground tracks and coverage, and Doppler tuning
// of band B. See docs/SAT.md.
//
// "d75sat" serves the page on localhost, with orbits from CelesTrak and
// transmitters from SatNOGS (or --demo, with made-up satellites and a fast
// clock). "d75sat passes" lists passes from an element file. Doppler
// tuning of the radio arrives in milestone S2.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"d75scope/orbit"
	"d75scope/satapp"
	"d75scope/satdata"
	"d75scope/satdemo"
	"d75scope/satweb"
	"d75scope/tiles"
	"d75scope/web"
)

const (
	version   = "0.1.0"
	homePage  = "https://github.com/NiallDonohoe/THD75-Playground"
	userAgent = "d75sat/" + version + " (+" + homePage + ")"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "d75sat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "passes" {
		return passes(args[1:])
	}
	return serve(args)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("d75sat", flag.ExitOnError)
	demoMode := fs.Bool("demo", false, "made-up satellites and a fast clock; no network apart from map tiles")
	speed := fs.Float64("speed", 10, "with --demo: how many times faster than real time the clock runs")
	addr := fs.String("addr", "127.0.0.1:8078", "address for the page (loopback only)")
	lat := fs.Float64("lat", 0, "your latitude, degrees north (otherwise set it on the page)")
	lon := fs.Float64("lon", 0, "your longitude, degrees east")
	offline := fs.Bool("offline", false, "do not contact CelesTrak or SatNOGS; use the cached data")
	noTiles := fs.Bool("no-tiles", false, "draw no map tiles, so nothing is fetched from the tile server")
	tileURL := fs.String("tile-url", tiles.OSM, "tile server URL template")
	verbose := fs.Bool("v", false, "log every request")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75sat [flags]              the page, on http://127.0.0.1:8078/\n"+
			"       d75sat passes [flags] FILE  passes from an element file\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unknown command %q", fs.Arg(0))
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(cfgDir, "d75sat", "config.json")
	var cfg satapp.Config
	var clock satapp.Clock = satapp.RealClock{}
	if *demoMode {
		if *speed < 1 || *speed > 100 {
			return errors.New("--speed must be 1 to 100")
		}
		cfgPath = "" // the demo remembers nothing
		clock = satapp.NewFastClock(time.Now(), *speed)
	} else if cfg, err = satapp.LoadConfig(cfgPath); err != nil {
		return fmt.Errorf("%s: %v", cfgPath, err)
	}
	app := satapp.New(clock, cfg, cfgPath)
	if *lat != 0 || *lon != 0 {
		if err := app.SetLocation(satapp.Location{Lat: *lat, Lon: *lon, Source: "flags"}); err != nil {
			return fmt.Errorf("--lat/--lon: %v", err)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *demoMode {
		station := orbit.Observer{Lat: 53.3498, Lon: -6.2603}
		if l := app.Config().Location; l != nil {
			station = orbit.Observer{Lat: l.Lat, Lon: l.Lon, AltM: l.AltM}
		} else {
			app.SetLocation(satapp.Location{Lat: station.Lat, Lon: station.Lon, Source: "demo"})
		}
		app.SetCatalog(satdemo.Catalog(station, clock.Now()), nil)
	} else {
		f := &satdata.Fetcher{Dir: filepath.Join(cacheDir, "d75sat"), UserAgent: userAgent, Offline: *offline}
		go refresh(ctx, f, app, log)
	}

	srv := &satweb.Server{App: app, Assets: web.Sat(),
		Info: satweb.Info{Version: version, Demo: *demoMode, Tiles: !*noTiles, Network: !*demoMode && !*offline}}
	if *demoMode {
		srv.Info.Speed = *speed
	}
	if !*noTiles {
		srv.Tiles = &tiles.Proxy{Upstream: *tileURL, Dir: filepath.Join(cacheDir, "d75sat", "tiles"), UserAgent: userAgent}
	}
	ln, err := satweb.Listen(*addr)
	if err != nil {
		return err
	}
	var handler http.Handler = srv.Handler()
	if *verbose {
		h := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Debug("request", "method", r.Method, "path", r.URL.Path)
			h.ServeHTTP(w, r)
		})
	}
	hs := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go app.Run(ctx)
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		hs.Shutdown(sctx)
	}()
	mode := ""
	if *demoMode {
		mode = fmt.Sprintf(" (demo, clock ×%g)", *speed)
	}
	fmt.Printf("d75sat %s%s: open http://%s/ in Chrome\n", version, mode, ln.Addr())
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// refresh loads the catalogue now and then every ten minutes; the fetcher
// itself decides when each source is due (2 h for orbits, a day for
// transmitters).
func refresh(ctx context.Context, f *satdata.Fetcher, app *satapp.App, log *slog.Logger) {
	var last map[string]satdata.Status
	for {
		cctx, c := context.WithTimeout(ctx, 2*time.Minute)
		sats, status, err := f.Catalog(cctx)
		c()
		switch {
		case err != nil:
			log.Warn("catalogue", "err", err)
			app.SetStatus(status)
		case last == nil || !status["celestrak"].Fetched.Equal(last["celestrak"].Fetched) || !status["satnogs"].Fetched.Equal(last["satnogs"].Fetched):
			log.Info("catalogue", "satellites", len(sats))
			app.SetCatalog(sats, status)
			last = status
		default:
			app.SetStatus(status)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}

func passes(args []string) error {
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
	fs.Parse(args)
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
