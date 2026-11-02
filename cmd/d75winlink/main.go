// Command d75winlink is a Winlink email client for the TH-D75: compose,
// a mailbox, and sessions with Winlink, in a page on localhost. See
// docs/WINLINK.md.
//
// So far only --demo runs: sessions with a fake Winlink server in this
// process. The real CMS over Telnet arrives in milestone W2, and RF in W4.
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

	"d75scope/web"
	"d75scope/winlink"
	"d75scope/winlinkapp"
	"d75scope/winlinkdemo"
	"d75scope/winlinkweb"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "d75winlink:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		demoMode = flag.Bool("demo", false, "run against a fake Winlink server with made-up mail (nothing leaves this Mac)")
		addr     = flag.String("addr", "127.0.0.1:8078", "address for the page (loopback only)")
		call     = flag.String("call", "", "your Winlink account's callsign, such as EI2ABC (demo default N0CALL)")
		locator  = flag.String("locator", "", "your Maidenhead locator, sent in the session greeting (optional)")
		dir      = flag.String("dir", "", "where the mailbox lives (default: ~/Library/Application Support/d75winlink)")
		rate     = flag.Int("demo-rate", 600, "the demo link's speed in bytes a second (0 for full speed)")
		verbose  = flag.Bool("v", false, "log every request")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75winlink --demo [flags]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if !*demoMode {
		return errors.New("only --demo runs so far: the real Winlink CMS over Telnet arrives in milestone W2")
	}
	if *call == "" {
		*call = "N0CALL"
	}
	mycall, err := winlink.ParseAddress(*call)
	if err != nil || strings.HasPrefix(mycall, "SMTP:") {
		return fmt.Errorf("--call: %q is not a callsign", *call)
	}

	root := *dir
	if root == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		root = filepath.Join(cfg, "d75winlink")
	}
	// The demo's mail is kept apart from real mail.
	mbDir := filepath.Join(root, "demo", "mailbox", mycall)
	mb, err := winlinkapp.OpenMailbox(mbDir)
	if err != nil {
		return err
	}
	sessionLog, err := os.OpenFile(filepath.Join(mbDir, "sessions.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer sessionLog.Close()

	demo, err := winlinkdemo.Start(mycall)
	if err != nil {
		return err
	}
	defer demo.Close()
	demo.Rate = *rate

	app := winlinkapp.New(winlinkapp.Config{
		MyCall:     mycall,
		Password:   winlinkdemo.Password,
		Locator:    *locator,
		Version:    version,
		SessionLog: sessionLog,
		Routes: []winlinkapp.Route{{
			ID:     "telnet",
			Label:  "Telnet to the demo CMS",
			Dial:   demo.Dial,
			Target: winlink.CMSTarget,
		}},
	}, mb)

	srv := &winlinkweb.Server{
		App:    app,
		Assets: web.Winlink(),
		Info: winlinkweb.Info{
			Version:        version,
			MyCall:         mycall,
			Demo:           true,
			MaxAttachments: winlinkapp.MaxAttachments,
			MaxMessage:     winlinkapp.MaxMessage,
		},
	}
	ln, err := winlinkweb.Listen(*addr)
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		app.Stop()
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		hs.Shutdown(sctx)
	}()

	fmt.Printf("d75winlink %s (demo) for %s: open http://%s/ in Chrome\nmailbox: %s\n", version, mycall, ln.Addr(), mbDir)
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
