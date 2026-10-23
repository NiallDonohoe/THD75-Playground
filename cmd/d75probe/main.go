// Command d75probe answers the unverified facts about the TH-D75 (see
// HARDWARE_QUESTIONS.md) and writes probe-report.json, WAV recordings and a
// plain-language summary.
//
// It is read-only by default. --allow-writes lets it change only the allowed
// settings (FQ, MD, IO, FT, FS, AI on band B), after saving a snapshot; it
// restores them before exiting. Nothing it sends can make the radio transmit.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"d75scope/audio"
	"d75scope/probe"
	"d75scope/radio"
)

type notes map[string]string

func (n notes) String() string { return fmt.Sprint(map[string]string(n)) }
func (n notes) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("want key=value, got %q", s)
	}
	n[k] = v
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		allowWrites     = flag.Bool("allow-writes", false, "snapshot the radio, change band B settings to record IF in each mode, then restore")
		outDir          = flag.String("out", "", "output directory (default probe-out-<time>)")
		seconds         = flag.Float64("seconds", 10, "length of each IF recording")
		port            = flag.String("port", "", "serial device (default: discover /dev/cu.usbmodem*)")
		baud            = flag.Int("baud", 115200, "baud rate (falls back to 9600)")
		audioDevice     = flag.String("audio-device", "", "audio device name or UID (default: discover the TH-D75)")
		refCarrier      = flag.Int64("ref-carrier", 0, "frequency in Hz of a steady carrier near the dial, to measure the per-mode offset")
		ifCentre        = flag.Float64("if-centre", 12000, "expected IF centre in Hz")
		restorePrevious = flag.Bool("restore-previous", false, "restore settings a previous session left changed, before probing")
		snapshotPath    = flag.String("snapshot", "", "snapshot file (default: under the user config directory)")
		nonInteractive  = flag.Bool("non-interactive", false, "do not ask questions at the terminal")
		mock            = flag.Bool("mock", false, "run against a fake radio on a pseudo-terminal and a synthetic IF")
		list            = flag.Bool("list", false, "list audio devices and serial ports, then exit")
		verbose         = flag.Bool("v", false, "log every step")
	)
	ownerNotes := notes{}
	flag.Var(ownerNotes, "note", "an owner observation as key=value (repeatable), e.g. menu_170_cw_pitch=700")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75probe [flags]\n\nRead-only by default. See docs/HARDWARE_CHECKLIST_M0.md.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	o := probe.Options{
		AllowWrites:     *allowWrites,
		RecordSeconds:   *seconds,
		Port:            *port,
		Baud:            *baud,
		AudioDevice:     *audioDevice,
		IFCentreHz:      *ifCentre,
		RefCarrierHz:    *refCarrier,
		StorePath:       *snapshotPath,
		RestorePrevious: *restorePrevious,
		Notes:           ownerNotes,
		Log:             log,
		Tool:            probe.Tool{Name: "d75probe", Version: version()},
		Audio:           audio.New(),
	}
	stamp := time.Now().Format("20060102-150405")
	if *mock {
		m := probe.NewMock(1, true)
		if err := m.UsePTY(); err != nil {
			fmt.Fprintln(os.Stderr, "mock:", err)
			return 1
		}
		m.Apply(&o)
		if o.RefCarrierHz == 0 {
			o.RefCarrierHz = probe.MockRefCarrierHz
		}
		if o.StorePath == "" {
			dir, err := os.MkdirTemp("", "d75probe-mock-")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			o.StorePath = filepath.Join(dir, "radio-snapshot.json")
		}
		stamp = "mock-" + stamp
		fmt.Fprintf(os.Stderr, "mock radio on %s\n", m.PortPath)
	}
	if *list {
		return listDevices(o)
	}
	if *outDir == "" {
		*outDir = "probe-out-" + stamp
	}
	o.OutDir = *outDir
	if !*nonInteractive && !*mock && isTerminal(os.Stdin) {
		o.Prompt = newTerminalPrompt()
	}

	// First Ctrl-C (or SIGTERM) stops the probe and restores the radio; a
	// second exits at once and keeps the dirty snapshot for next time.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "\nstopping: restoring the radio (Ctrl-C again to exit now and leave it for next time)")
		cancel()
		<-sigs
		fmt.Fprintln(os.Stderr, "exiting without restoring; the snapshot stays dirty and the next run will offer to restore it")
		os.Exit(130)
	}()

	if *allowWrites {
		fmt.Fprintln(os.Stderr, "--allow-writes: the probe will change band B's mode, frequency, fine tuning, USB output (Menu 102) and AI, and put them back at the end.")
	}
	rep, err := probe.Run(ctx, o)
	if rep == nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		return 1
	}
	reportPath := filepath.Join(o.OutDir, "probe-report.json")
	if werr := rep.WriteJSON(reportPath); werr != nil {
		fmt.Fprintln(os.Stderr, "write report:", werr)
		return 1
	}
	summary := probe.Summary(rep)
	os.WriteFile(filepath.Join(o.OutDir, "summary.txt"), []byte(summary), 0o644)
	fmt.Print(summary)
	fmt.Printf("\nReport: %s\n", reportPath)
	switch {
	case rep.Restore != nil && !rep.Restore.Clean:
		return 2
	case err != nil || len(rep.Errors) > 0:
		return 1
	}
	return 0
}

func listDevices(o probe.Options) int {
	devs, err := o.Audio.Devices()
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio:", err)
	}
	fmt.Println("Audio devices:")
	for _, d := range devs {
		def := ""
		if d.DefaultInput {
			def = " (default input)"
		}
		fmt.Printf("  %-32q in=%d out=%d %g Hz %s%s\n      id=%s\n", d.Name, d.InputChannels, d.OutputChannels, d.NominalRate, d.Transport, def, d.ID)
	}
	listPorts := o.ListPorts
	if listPorts == nil {
		listPorts = radio.ListPorts
	}
	ports, err := listPorts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "serial:", err)
	}
	fmt.Println("Serial ports:")
	if len(ports) == 0 {
		fmt.Println("  none (is the radio on, connected, and Menu 980 set to COM+AF/IF Output?)")
	}
	for _, p := range ports {
		if p.USB != nil {
			fmt.Printf("  %s  %s %q vendor 0x%04X product 0x%04X serial %q\n", p.Path, p.USB.Vendor, p.USB.Product, p.USB.VendorID, p.USB.ProductID, p.USB.Serial)
		} else {
			fmt.Printf("  %s\n", p.Path)
		}
	}
	return 0
}

func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "+dirty"
	}
	return rev
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// terminalPrompt reads answers from stdin, one line per question.
type terminalPrompt struct{ lines chan string }

func newTerminalPrompt() *terminalPrompt {
	p := &terminalPrompt{lines: make(chan string)}
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
	}()
	return p
}

func (p *terminalPrompt) Ask(q string, timeout time.Duration) (string, bool) {
	fmt.Fprintf(os.Stderr, "\n>>> %s\n> ", q)
	select {
	case line, ok := <-p.lines:
		return line, ok
	case <-time.After(timeout):
		fmt.Fprintln(os.Stderr, "(no answer)")
		return "", false
	}
}
