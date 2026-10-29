// Command d75aprs is an APRS client for the TH-D75: a map of the stations
// heard on RF and on APRS-IS, with messages, in a page on localhost. See
// docs/APRS.md.
//
// So far only --demo runs: fake RF, APRS-IS and aprs.fi traffic. The live
// sources (the radio's KISS TNC and APRS-IS) arrive in milestone A2.
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
	"strings"
	"syscall"
	"time"

	"d75scope/aprs"
	"d75scope/aprsapp"
	"d75scope/aprsfi"
	"d75scope/aprsweb"
	"d75scope/demo"
	"d75scope/tiles"
	"d75scope/web"
)

const (
	version   = "0.1.0"
	homePage  = "https://github.com/NiallDonohoe/THD75-Playground"
	userAgent = "d75aprs/" + version + " (+" + homePage + ")"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "d75aprs:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		demoMode = flag.Bool("demo", false, "run on made-up RF, APRS-IS and aprs.fi traffic (no radio, no network apart from map tiles)")
		addr     = flag.String("addr", "127.0.0.1:8076", "address for the page (loopback only)")
		call     = flag.String("call", "", "your callsign with SSID, such as EI2ABC-7 (demo default N0CALL)")
		lat      = flag.Float64("lat", 53.3498, "your latitude, degrees north (the map's centre)")
		lon      = flag.Float64("lon", -6.2603, "your longitude, degrees east")
		noTiles  = flag.Bool("no-tiles", false, "draw no map tiles, so nothing is fetched from the tile server")
		tileURL  = flag.String("tile-url", tiles.OSM, "tile server URL template")
		verbose  = flag.Bool("v", false, "log every request and packet")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75aprs --demo [flags]\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nD75APRS_APRSFI_KEY in the environment turns on aprs.fi lookups with your own API key.\n")
	}
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if !*demoMode {
		return errors.New("only --demo runs so far: the radio (KISS) and APRS-IS sources arrive in milestone A2")
	}
	if *call == "" {
		*call = "N0CALL"
	}
	*call = strings.ToUpper(*call)
	if _, _, err := aprs.SplitCall(*call); err != nil {
		return fmt.Errorf("--call: %v", err)
	}
	if *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 {
		return errors.New("--lat and --lon must be a position on Earth")
	}

	store := aprsapp.NewStore(*call)
	scenario := demo.New(*call, *lat, *lon)
	store.SetStatus("rf", aprsapp.SourceStatus{State: "demo", Detail: "made-up RF traffic, as if from the radio's TNC"})
	store.SetStatus("is", aprsapp.SourceStatus{State: "demo", Detail: "made-up APRS-IS traffic"})

	srv := &aprsweb.Server{
		Store:  store,
		Assets: web.APRS(),
		Info:   aprsweb.Info{Version: version, MyCall: *call, Lat: *lat, Lon: *lon, Demo: true, Tiles: !*noTiles, APRSFI: true},
	}

	// aprs.fi: the real service with the owner's key, otherwise the demo's
	// fake answering through the same client.
	fi := &aprsfi.Client{Key: os.Getenv("D75APRS_APRSFI_KEY"), UserAgent: userAgent}
	if fi.Key != "" {
		store.SetStatus("aprsfi", aprsapp.SourceStatus{State: "on request", Detail: "aprs.fi with your API key"})
	} else {
		fi.Key = "demo"
		fi.HTTP = &http.Client{Transport: scenario.APRSFI(), Timeout: 5 * time.Second}
		store.SetStatus("aprsfi", aprsapp.SourceStatus{State: "demo", Detail: "made-up aprs.fi answers; set D75APRS_APRSFI_KEY for the real one"})
	}
	srv.Lookup = fi.Locate

	if !*noTiles {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		srv.Tiles = &tiles.Proxy{Upstream: *tileURL, Dir: filepath.Join(cache, "d75aprs", "tiles"), UserAgent: userAgent}
	}

	ln, err := aprsweb.Listen(*addr)
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

	stop := make(chan struct{})
	go scenario.Run(func(a aprsapp.Arrival) {
		log.Debug("packet", "source", a.Source, "packet", a.Packet.String())
		store.Ingest(a)
	}, stop)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		close(stop)
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		hs.Shutdown(sctx)
	}()

	fmt.Printf("d75aprs %s (demo) for %s: open http://%s/ in Chrome\n", version, *call, ln.Addr())
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
