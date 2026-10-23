// Package probe answers the unverified facts about the TH-D75 (see
// HARDWARE_QUESTIONS.md). It is read-only by default. With AllowWrites it
// snapshots the radio, changes only the allowed settings, records IF in each
// mode and after a 1 kHz retune, then restores and verifies.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"d75scope/dsp"
	"d75scope/ifsource"
	"d75scope/radio"
)

// Options configure a probe run.
type Options struct {
	OutDir        string
	AllowWrites   bool
	RecordSeconds float64 // per IF recording; default 10
	ShortSeconds  float64 // as-found and extra-mode recordings; default 5
	SettleSeconds float64 // discarded after each change; default 0.3

	Port        string // serial device; empty = discover
	Baud        int    // default 115200, falling back to 9600
	AudioDevice string // audio device ID or name; empty = discover

	IFCentreHz   float64 // expected IF centre; default 12000
	RefCarrierHz int64   // known carrier near the dial, for Δ_mode
	TuneStepHz   int64   // retune for the inversion test; default 1000

	StorePath       string // snapshot file; default under the user config dir
	RestorePrevious bool   // restore a dirty snapshot from an earlier session first
	RestoreTimeout  time.Duration // after an interrupt; default 2 s (a normal finish allows 10 s)

	Notes  map[string]string
	Prompt Prompter // nil: non-interactive
	Mock   bool

	Audio     ifsource.Provider
	ListPorts func() ([]radio.Port, error)
	OpenPort  func(path string, baud int) (io.ReadWriteCloser, error)
	Session   radio.SessionConfig
	Log       *slog.Logger
	Tool      Tool
}

// Prompter asks the owner a question at the terminal.
type Prompter interface {
	// Ask shows the question and returns the answer; ok is false if no
	// answer came (timeout or end of input).
	Ask(question string, timeout time.Duration) (answer string, ok bool)
}

// The modes the IF is recorded in, and the extra modes tested for IF output.
var (
	ifModes    = []radio.Mode{radio.ModeUSB, radio.ModeLSB, radio.ModeCW, radio.ModeAM}
	extraModes = []radio.Mode{radio.ModeFM, radio.ModeNFM}
)

type prober struct {
	o   Options
	rep *Report
	log *slog.Logger

	mu sync.Mutex // guards rep.Transcript and rep.CAT.Foreign from the traffic hook

	cat   *radio.CAT
	guard *radio.Guard
	src   ifsource.Source
	recs  map[string]*Analysis

	aiStop atomic.Bool
}

// Run runs the probe and returns its report. The report is returned even on
// error, as complete as it got.
func Run(ctx context.Context, o Options) (*Report, error) {
	if o.RecordSeconds <= 0 {
		o.RecordSeconds = 10
	}
	if o.ShortSeconds <= 0 {
		o.ShortSeconds = 5
	}
	if o.SettleSeconds <= 0 {
		o.SettleSeconds = 0.3
	}
	if o.Baud == 0 {
		o.Baud = 115200
	}
	if o.IFCentreHz == 0 {
		o.IFCentreHz = 12000
	}
	if o.TuneStepHz == 0 {
		o.TuneStepHz = 1000
	}
	if o.RestoreTimeout <= 0 {
		o.RestoreTimeout = 2 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.ListPorts == nil {
		o.ListPorts = radio.ListPorts
	}
	if o.OpenPort == nil {
		o.OpenPort = func(path string, baud int) (io.ReadWriteCloser, error) { return radio.OpenSerial(path, baud) }
	}
	if o.StorePath == "" {
		p, err := radio.DefaultStorePath()
		if err != nil {
			return nil, err
		}
		o.StorePath = p
	}
	if o.Tool.Name == "" {
		o.Tool = Tool{Name: "d75probe", Version: "dev"}
	}
	o.Tool.GoVersion, o.Tool.OS, o.Tool.Arch = runtime.Version(), runtime.GOOS, runtime.GOARCH
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, err
	}

	mode := "read-only"
	if o.AllowWrites {
		mode = "allow-writes"
	}
	p := &prober{o: o, log: o.Log, recs: map[string]*Analysis{}, rep: &Report{
		SchemaVersion: SchemaVersion,
		Tool:          o.Tool,
		Started:       time.Now().UTC(),
		Mode:          mode,
		Mock:          o.Mock,
		Notes:         map[string]string{},
		Settings: Settings{
			RecordSeconds: o.RecordSeconds, Port: o.Port, Baud: o.Baud, AudioDevice: o.AudioDevice,
			IFCentreHz: o.IFCentreHz, RefCarrierHz: o.RefCarrierHz, TuneStepHz: o.TuneStepHz,
			Interactive: o.Prompt != nil, RestorePrevious: o.RestorePrevious,
		},
		Steps: []Step{}, Recordings: []Recording{}, Inversion: []Inversion{}, Offsets: []Offset{},
		Answers: []Answer{}, Warnings: []string{}, Errors: []string{}, Transcript: []radio.TrafficEntry{},
	}}
	for k, v := range o.Notes {
		p.rep.Notes[k] = v
	}
	err := p.run(ctx)
	if err != nil {
		p.errorf("%v", err)
	}
	p.finish()
	return p.rep, err
}

func (p *prober) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	p.log.Warn(msg)
	p.rep.Warnings = append(p.rep.Warnings, msg)
}

func (p *prober) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	p.log.Error(msg)
	p.rep.Errors = append(p.rep.Errors, msg)
}

func (p *prober) step(action string, err error, result string) {
	s := Step{Time: time.Now().UTC(), Action: action, Result: result}
	if err != nil {
		s.Error = err.Error()
		p.log.Warn("step failed", "action", action, "err", err)
	} else {
		p.log.Info(action, "result", result)
	}
	p.rep.Steps = append(p.rep.Steps, s)
}

func (p *prober) run(ctx context.Context) error {
	p.discoverAudio()
	if err := p.discoverSerial(); err != nil {
		p.rep.Serial.Error = err.Error()
		p.errorf("%v", err)
	} else if err := p.connect(ctx); err != nil {
		p.errorf("%v", err)
	}
	defer func() {
		if p.cat != nil {
			p.cat.Close()
		}
	}()
	if p.cat != nil && p.rep.CAT.Responding {
		p.readAll(ctx)
	}

	// Audio, opened once and kept running.
	if p.rep.Audio.Radio != nil {
		src, err := p.o.Audio.Open(p.rep.Audio.Radio.ID)
		if err != nil {
			p.rep.Audio.Error = err.Error()
			p.errorf("open audio device %q: %v", p.rep.Audio.Radio.Name, err)
		} else {
			p.src = src
			f := src.Format()
			p.rep.Audio.Format = &f
			defer src.Close()
		}
	}
	if p.src != nil {
		output := "unknown"
		if v, ok := p.rep.CAT.State["io"].(string); ok {
			output = v
		}
		p.record(ctx, "as-found", "whatever the radio was already sending (IO as found: "+output+")", p.o.ShortSeconds, "")
	}

	if !p.o.AllowWrites {
		return ctx.Err()
	}
	switch {
	case p.cat == nil || !p.rep.CAT.Responding:
		return errors.New("--allow-writes: skipped, the radio is not answering CAT")
	case p.src == nil:
		return errors.New("--allow-writes: skipped, no audio from the radio to record")
	}
	err := p.writeSequence(ctx)
	if p.o.Prompt != nil && len(p.rep.Recordings) > 1 && ctx.Err() == nil {
		p.askOwner()
	}
	return err
}

// askOwner collects the observations only the owner can make.
func (p *prober) askOwner() {
	qs := []struct{ key, text string }{
		{"speaker_during_if", "While the probe recorded IF, did the radio's speaker keep playing? (yes/no/unsure)"},
		{"band_a_changed", "Did anything on band A's display change during the probe (fine-tune or step indicators)? (yes/no/unsure)"},
		{"menu_170_cw_pitch", "What is Menu 170 (CW pitch) set to, in Hz? (Enter to skip)"},
		{"menu_91a", "What is Menu 91A (USB audio output level) set to, 1–7? (Enter to skip)"},
	}
	for _, q := range qs {
		if _, ok := p.rep.Notes[q.key]; ok {
			continue
		}
		if ans, ok := p.o.Prompt.Ask(q.text, 2*time.Minute); ok && strings.TrimSpace(ans) != "" {
			p.rep.Notes[q.key] = strings.TrimSpace(ans)
		}
	}
}

func (p *prober) discoverAudio() {
	if p.o.Audio == nil {
		p.rep.Audio.Error = "no audio provider"
		return
	}
	devs, err := p.o.Audio.Devices()
	p.rep.Audio.Devices = devs
	if p.rep.Audio.Devices == nil {
		p.rep.Audio.Devices = []ifsource.Device{}
	}
	if err != nil {
		p.rep.Audio.Error = err.Error()
		p.errorf("list audio devices: %v", err)
		return
	}
	if want := p.o.AudioDevice; want != "" {
		for i := range devs {
			if devs[i].ID == want || devs[i].Name == want {
				d := devs[i]
				p.rep.Audio.Radio, p.rep.Audio.MatchReason = &d, "chosen with --audio-device"
				return
			}
		}
		p.rep.Audio.Error = fmt.Sprintf("no audio device %q", want)
		p.errorf("%s", p.rep.Audio.Error)
		return
	}
	d, why, err := ifsource.PickRadioDevice(devs)
	if err != nil {
		p.rep.Audio.Error = err.Error()
		p.errorf("%v", err)
		return
	}
	p.rep.Audio.Radio, p.rep.Audio.MatchReason = &d, why
}

func (p *prober) discoverSerial() error {
	ports, err := p.o.ListPorts()
	p.rep.Serial.Ports = ports
	if p.rep.Serial.Ports == nil {
		p.rep.Serial.Ports = []radio.Port{}
	}
	if err != nil {
		return fmt.Errorf("list serial ports: %w", err)
	}
	if p.o.Port != "" {
		p.rep.Serial.Port, p.rep.Serial.Match = p.o.Port, "chosen with --port"
		return nil
	}
	port, m, err := radio.PickRadioPort(ports)
	if err != nil {
		return err
	}
	p.rep.Serial.Port, p.rep.Serial.Match = port.Path, m.String()
	return nil
}

// connect opens the port and identifies the radio, falling back to 9600
// baud if the first rate gets no answer.
func (p *prober) connect(ctx context.Context) error {
	bauds := []int{p.o.Baud}
	if p.o.Baud != 9600 {
		bauds = append(bauds, 9600)
	}
	var lastErr error
	for _, baud := range bauds {
		rw, err := p.o.OpenPort(p.rep.Serial.Port, baud)
		if err != nil {
			if errors.Is(err, radio.ErrPortInUse) {
				return fmt.Errorf("%w: quit any other program using %s (CHIRP, a terminal, another d75scope)", err, p.rep.Serial.Port)
			}
			return err
		}
		p.rep.Serial.Baud = baud
		p.checkExclusive(baud)
		cfg := p.o.Session
		cfg.Traffic = p.traffic
		cat := radio.NewCAT(rw, cfg)
		res := p.exchange(ctx, cat, "ID")
		if res.Error == "" {
			p.cat = cat
			p.rep.CAT.Responding = true
			return nil
		}
		lastErr = errors.New(res.Error)
		stats := cat.Session().Stats()
		cat.Close()
		if stats.KISSFrames > 0 {
			return errors.New("the radio appears to be in KISS mode (0xC0 framing, no CAT reply): leave KISS mode on the radio and run again")
		}
	}
	return fmt.Errorf("no CAT reply to ID on %s at %v baud (%v); if the radio is in KISS mode, leave it; check Menu 980", p.rep.Serial.Port, bauds, lastErr)
}

func (p *prober) checkExclusive(baud int) {
	second, err := p.o.OpenPort(p.rep.Serial.Port, baud)
	switch {
	case err == nil:
		second.Close()
		p.rep.Serial.ExclusiveLock = "not enforced"
	case errors.Is(err, radio.ErrPortInUse):
		p.rep.Serial.ExclusiveLock = "verified"
	default:
		p.rep.Serial.ExclusiveLock = "untested: " + err.Error()
	}
}

func (p *prober) traffic(e radio.TrafficEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rep.Transcript = append(p.rep.Transcript, e)
	if (e.Kind == "nmea" || e.Kind == "aprs") && len(p.rep.CAT.Foreign) < 5 {
		p.rep.CAT.Foreign = append(p.rep.CAT.Foreign, e.Text)
	}
}

// exchange sends one read and records it.
func (p *prober) exchange(ctx context.Context, cat *radio.CAT, raw string) CommandResult {
	start := time.Now()
	reply, err := cat.Session().Console(ctx, raw)
	res := CommandResult{Command: raw, Reply: reply, ElapsedMs: float64(time.Since(start).Microseconds()) / 1000}
	if err != nil {
		res.Error = err.Error()
	}
	p.rep.CAT.Reads = append(p.rep.CAT.Reads, res)
	return res
}

// readAll performs the read-only survey.
func (p *prober) readAll(ctx context.Context) {
	state := map[string]any{}
	p.rep.CAT.State = state
	for _, raw := range []string{"FV", "FO 1", "FQ 1", "MD 1", "IO", "FT", "FS", "AI", "SH 0", "SH 1", "SH 2", "BY 1"} {
		p.exchange(ctx, p.cat, raw)
	}
	for _, r := range p.rep.CAT.Reads {
		if r.Error != "" {
			continue
		}
		l, err := radio.ParseLine(r.Reply)
		if err != nil {
			continue
		}
		v := last(l.Params)
		switch r.Command {
		case "ID":
			p.rep.CAT.Identity.Model = strings.Join(l.Params, ",")
		case "FV":
			p.rep.CAT.Identity.Firmware = strings.Join(l.Params, ",")
		case "FQ 1":
			state["frequency_hz"], _ = radio.ParseFrequency(v)
		case "MD 1":
			if m, err := strconv.Atoi(v); err == nil {
				state["mode"] = radio.Mode(m).String()
			}
		case "IO":
			if o, err := strconv.Atoi(v); err == nil {
				state["io"] = radio.IFOutput(o).String()
			}
		case "FT":
			state["fine_tune"] = v == "1"
		case "FS":
			if s, err := strconv.Atoi(v); err == nil {
				state["fine_step"] = radio.FineStep(s).String()
			}
		case "AI":
			state["auto_info"] = v == "1"
		case "BY 1":
			state["squelch_open"] = v == "1"
		case "SH 0", "SH 1", "SH 2":
			if len(l.Params) == 2 {
				fm, _ := strconv.Atoi(l.Params[0])
				idx, _ := strconv.Atoi(l.Params[1])
				state["filter_"+strings.ToLower(radio.FilterMode(fm).String())+"_hz"] = radio.FilterWidthHz(radio.FilterMode(fm), idx)
			}
		case "FO 1":
			if vfo, err := radio.ParseVFO(l.Params); err == nil {
				state["vfo"] = vfo
			} else {
				p.warnf("FO 1 reply did not parse as the D74's 21 fields: %v", err)
			}
		}
	}
	if !strings.Contains(p.rep.CAT.Identity.Model, "TH-D75") {
		p.warnf("ID says %q, not TH-D75", p.rep.CAT.Identity.Model)
	}
	p.rep.CAT.Hygiene = p.cat.Session().Stats()
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

// record captures seconds of audio into name.wav and analyses it.
func (p *prober) record(ctx context.Context, name, purpose string, seconds float64, mode string) *Recording {
	if err := ctx.Err(); err != nil {
		return nil
	}
	f := p.src.Format()
	rec := Recording{Name: name, File: name + ".wav", Purpose: purpose, Mode: mode, Seconds: seconds, Format: f}
	if p.cat != nil && p.rep.CAT.Responding {
		if hz, err := p.cat.Frequency(ctx); err == nil {
			rec.DialHz = hz
		}
		if rec.Mode == "" {
			if m, err := p.cat.Mode(ctx); err == nil {
				rec.Mode = m.String()
			}
		}
		if o, err := p.cat.IFOutput(ctx); err == nil {
			rec.Output = o.String()
		}
		if open, err := p.cat.Squelch(ctx); err == nil {
			rec.SquelchOpen = &open
		}
	}
	if fl, ok := p.src.(ifsource.Flusher); ok {
		fl.Flush()
	}
	settle := make([]float32, int(p.o.SettleSeconds*f.SampleRate)*f.Channels)
	if _, err := p.src.Read(settle); err != nil {
		p.errorf("record %s: %v", name, err)
		return nil
	}
	var before uint64
	if ov, ok := p.src.(ifsource.Overruns); ok {
		before = ov.Overruns()
	}
	rec.Started = time.Now().UTC()
	buf := make([]float32, int(seconds*f.SampleRate)*f.Channels)
	chunk := int(f.SampleRate/10) * f.Channels
	for off := 0; off < len(buf); off += chunk {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if _, err := p.src.Read(buf[off:min(len(buf), off+chunk)]); err != nil {
			p.errorf("record %s: %v", name, err)
			return nil
		}
	}
	if ov, ok := p.src.(ifsource.Overruns); ok {
		rec.Overruns = ov.Overruns() - before
	}
	if p.cat != nil && p.rep.CAT.Responding {
		if hz, err := p.cat.Frequency(ctx); err == nil {
			rec.DialAfterHz = hz
			if rec.DialHz != 0 && hz != rec.DialHz {
				p.warnf("the dial moved during %s (%d → %d Hz); was the knob turned?", name, rec.DialHz, hz)
			}
		}
	}
	w, err := dsp.CreateWAV(filepath.Join(p.o.OutDir, rec.File), dsp.WAVFormat{SampleRate: int(f.SampleRate), Channels: f.Channels})
	if err == nil {
		err = w.Write(buf)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		p.errorf("write %s: %v", rec.File, err)
	}
	rec.Analysis = Analyse(buf, f, p.o.IFCentreHz)
	p.recs[name] = &rec.Analysis
	p.rep.Recordings = append(p.rep.Recordings, rec)
	p.log.Info("recorded", "name", name, "mode", rec.Mode, "dial", rec.DialHz, "peak_dbfs", rec.Analysis.Levels[0].PeakDBFS)
	return &p.rep.Recordings[len(p.rep.Recordings)-1]
}

// writeSequence is the --allow-writes part. Every write goes through the
// guard; the radio is restored however this returns.
func (p *prober) writeSequence(ctx context.Context) (err error) {
	store := radio.Store{Path: p.o.StorePath}
	if prev, lerr := store.Load(); lerr == nil && prev.Dirty {
		ps := &PreviousSession{Snapshot: prev}
		p.rep.PreviousSession = ps
		if !p.o.RestorePrevious {
			return errors.New("a previous session left the radio modified (snapshot " + p.o.StorePath + "); run again with --restore-previous to put it back first")
		}
		if prev.Radio.Model != "" && prev.Radio.Model != p.rep.CAT.Identity.Model {
			return fmt.Errorf("the previous session's snapshot is for a %q, not this %q; not restoring it", prev.Radio.Model, p.rep.CAT.Identity.Model)
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.o.RestoreTimeout)
		rr, rerr := radio.RestorePrevious(rctx, p.cat, store, prev)
		cancel()
		ps.Restore = &rr
		if rerr != nil {
			ps.Error = rerr.Error()
			return fmt.Errorf("restore previous session: %w", rerr)
		}
		ps.Restored = true
	}

	vfo, filters := p.vfoAndFilters()
	p.guard = radio.NewGuard(p.cat, store)
	snap, err := p.guard.Begin(ctx, p.rep.CAT.Identity, p.rep.Serial.Port, vfo, filters)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	p.rep.Snapshot = snap
	p.step("snapshot", nil, "saved to "+p.o.StorePath)

	defer func() {
		// Interrupted: the spec's 2 s cap. A normal finish has no one
		// waiting, so give a slow radio room rather than leave it dirty.
		timeout := 10 * time.Second
		if ctx.Err() != nil {
			timeout = p.o.RestoreTimeout
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		rr, rerr := p.guard.Restore(rctx)
		p.rep.Restore = &rr
		p.step("restore", rerr, fmt.Sprintf("clean=%v", rr.Clean))
		if rerr != nil {
			p.errorf("restore: %v; the snapshot stays dirty and the next run (or d75scope) will offer to restore it", rerr)
			if err == nil {
				err = rerr
			}
		}
	}()

	if err := p.write(ctx, radio.FieldIO, "1"); err != nil {
		return fmt.Errorf("set IF output (IO 1): %w", err)
	}
	if err := p.setMode(ctx, radio.ModeUSB); err != nil {
		return err
	}
	// Fine tuning on with 20 Hz steps, so a 1 kHz retune and off-grid
	// frequencies are possible. The radio may refuse in some states.
	if err := p.write(ctx, radio.FieldFT, "1"); err != nil {
		p.warnf("FT 1 refused: %v", err)
	}
	if err := p.write(ctx, radio.FieldFS, "0"); err != nil {
		p.warnf("FS 0 refused: %v", err)
	}
	p.resolutionTest(ctx)

	for _, m := range ifModes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.recordMode(ctx, m); err != nil {
			p.errorf("%s: %v", m, err)
		}
	}
	for _, m := range extraModes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.setMode(ctx, m); err != nil {
			p.warnf("%s: %v", m, err)
			continue
		}
		p.record(ctx, "mode-"+strings.ToLower(m.String()), "does IF output work in "+m.String()+"?", p.o.ShortSeconds, m.String())
	}
	if err := p.setMode(ctx, radio.ModeUSB); err != nil {
		return err
	}
	if err := p.write(ctx, radio.FieldIO, "2"); err != nil {
		p.warnf("IO 2 (Detect) refused: %v", err)
	} else {
		p.record(ctx, "detect-usb", "Detect output (IO 2) in USB", p.o.RecordSeconds, "USB")
	}
	// Last, so the owner's dial turns cannot spoil the recordings.
	if p.o.Prompt != nil && ctx.Err() == nil {
		p.aiTest(ctx)
	}
	return ctx.Err()
}

func (p *prober) vfoAndFilters() ([]string, map[string]string) {
	var vfo []string
	filters := map[string]string{}
	for _, r := range p.rep.CAT.Reads {
		if r.Error != "" {
			continue
		}
		l, err := radio.ParseLine(r.Reply)
		if err != nil {
			continue
		}
		switch {
		case r.Command == "FO 1":
			vfo = l.Params
		case strings.HasPrefix(r.Command, "SH ") && len(l.Params) == 2:
			filters[l.Params[0]] = l.Params[1]
		}
	}
	return vfo, filters
}

func (p *prober) write(ctx context.Context, f radio.Field, v string) error {
	got, err := p.guard.Write(ctx, f, v)
	result := "read back " + got
	if err == nil && got != v {
		result += " (asked for " + v + ")"
	}
	p.step(string(f)+" "+v, err, result)
	return err
}

func (p *prober) setMode(ctx context.Context, m radio.Mode) error {
	before, _ := p.cat.Frequency(ctx)
	if err := p.write(ctx, radio.FieldMD, strconv.Itoa(int(m))); err != nil {
		return fmt.Errorf("set mode %s: %w", m, err)
	}
	if after, err := p.cat.Frequency(ctx); err == nil && before != 0 && after != before {
		p.warnf("changing mode to %s moved the dial from %d to %d Hz", m, before, after)
	}
	return nil
}

func (p *prober) setFrequency(ctx context.Context, hz int64) (int64, error) {
	if err := p.write(ctx, radio.FieldFQ, radio.FormatFrequency(hz)); err != nil {
		return 0, err
	}
	return p.cat.Frequency(ctx)
}

// resolutionTest asks for a frequency off the 20 Hz grid and records what
// the radio reads back.
func (p *prober) resolutionTest(ctx context.Context) {
	d0, err := p.cat.Frequency(ctx)
	if err != nil {
		return
	}
	ask := d0 + 1010
	got, err := p.setFrequency(ctx, ask)
	p.step(fmt.Sprintf("resolution test: asked %d Hz", ask), err, fmt.Sprintf("radio holds %d Hz", got))
	if _, err := p.setFrequency(ctx, d0); err != nil {
		p.warnf("could not return to %d Hz after the resolution test: %v", d0, err)
	}
}

// recordMode records the IF at the current dial and 1 kHz up.
func (p *prober) recordMode(ctx context.Context, m radio.Mode) error {
	if err := p.setMode(ctx, m); err != nil {
		return err
	}
	base, err := p.cat.Frequency(ctx)
	if err != nil {
		return err
	}
	name := strings.ToLower(m.String())
	baseRec := p.record(ctx, "if-"+name, "IF in "+m.String(), p.o.RecordSeconds, m.String())
	up, err := p.setFrequency(ctx, base+p.o.TuneStepHz)
	if err != nil {
		return fmt.Errorf("retune +%d Hz: %w", p.o.TuneStepHz, err)
	}
	upRec := p.record(ctx, "if-"+name+"-up", fmt.Sprintf("IF in %s after the dial moved up %d Hz", m, p.o.TuneStepHz), p.o.RecordSeconds, m.String())
	if back, err := p.setFrequency(ctx, base); err != nil || back != base {
		p.warnf("could not return to %d Hz in %s (holds %d): %v", base, m, back, err)
	}
	if baseRec == nil || upRec == nil {
		return errors.New("recording failed")
	}
	inv := CompareTuning(m.String(), baseRec.Name, upRec.Name, *p.recs[baseRec.Name], *p.recs[upRec.Name], up-base, p.o.IFCentreHz)
	p.rep.Inversion = append(p.rep.Inversion, inv)
	if p.o.RefCarrierHz != 0 {
		if off := SolveOffset(m.String(), baseRec.Name, *p.recs[baseRec.Name], base, p.o.RefCarrierHz, inv.S, p.o.IFCentreHz); off != nil {
			p.rep.Offsets = append(p.rep.Offsets, *off)
		}
	}
	return nil
}

// aiTest turns on AI and asks the owner to turn the dial, to capture the
// format of unsolicited updates. Afterwards it sets the dial back to where
// it was, as an ordinary write, so restore still recognises the value as
// the program's and puts the original back.
func (p *prober) aiTest(ctx context.Context) {
	t := &AITest{Lines: []string{}}
	p.rep.AITest = t
	before, ferr := p.cat.Frequency(ctx)
	defer func() {
		if ferr == nil {
			if _, err := p.setFrequency(ctx, before); err != nil {
				p.warnf("could not set the dial back after the AI test: %v", err)
			}
		}
	}()
	if err := p.write(ctx, radio.FieldAI, "1"); err != nil {
		t.Note = "AI 1 refused: " + err.Error()
		return
	}
	// Drain anything already queued.
	for len(p.cat.Events()) > 0 {
		<-p.cat.Events()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case ev := <-p.cat.Events():
				if ev.Kind == radio.EventUnsolicited {
					t.Lines = append(t.Lines, ev.Raw)
				}
			case <-time.After(1500 * time.Millisecond):
				if p.aiStop.Load() {
					return
				}
			}
		}
	}()
	_, answered := p.o.Prompt.Ask("AI test: turn the band B dial two or three clicks, then press Enter.", 30*time.Second)
	time.Sleep(700 * time.Millisecond)
	p.aiStop.Store(true)
	<-done
	t.Ran = true
	switch {
	case !answered:
		t.Note = "no answer from the owner"
	case len(t.Lines) == 0:
		t.Note = "no unsolicited lines arrived while the dial was turned"
	}
}
