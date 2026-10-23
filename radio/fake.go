package radio

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeD75 is a pretend TH-D75 that speaks CAT the way the D74 reference
// describes. It answers the commands this program uses, replies ? and N,
// can add latency, pushes unsolicited AI lines, injects NMEA or KISS bytes,
// and can be unplugged mid-session. It logs every line it receives so tests
// can assert that nothing outside the allow-list was ever sent.
//
// Its reply formats are the reference's, not the TH-D75's: they are
// hypotheses until the probe confirms them.
type FakeD75 struct {
	mu sync.Mutex

	Model    string
	Firmware string
	freq     [2]int64
	mode     [2]int
	io       int
	ft       int
	fs       int
	ai       int
	sh       [3]int
	busy     [2]int

	// Latency delays every reply.
	Latency time.Duration
	// SnapHz, if set, rounds band B frequency writes to this many hertz,
	// as a radio tuning in steps might.
	SnapHz int64
	// NotAvailable makes these commands reply N.
	NotAvailable map[string]bool
	// Silent makes the radio ignore everything, as it does in KISS mode.
	Silent bool
	// ModeChangeSnapsHz, if set, re-snaps the frequency to this step when
	// the mode changes, to exercise restore ordering.
	ModeChangeSnapsHz int64
	// OnChange, if set, is called after any state change (for the
	// synthetic IF source to follow the dial).
	OnChange func(FakeState)

	received []string
	conn     io.ReadWriteCloser
	writeMu  sync.Mutex
}

// FakeState is the part of the fake's state the synthetic IF follows.
type FakeState struct {
	FrequencyHz int64
	Mode        Mode
	IFOutput    IFOutput
}

// NewFakeD75 returns a fake radio in a typical state: band B on 7.074 MHz
// USB, IF output off, fine tuning off.
func NewFakeD75() *FakeD75 {
	return &FakeD75{
		Model:    "TH-D75",
		Firmware: "1.03",
		freq:     [2]int64{145_500_000, 7_074_000},
		mode:     [2]int{0, int(ModeUSB)},
		io:       0,
		ft:       0,
		fs:       1,
		ai:       0,
		sh:       [3]int{2, 1, 1},
	}
}

// State returns the fake's band B state.
func (f *FakeD75) State() FakeState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stateLocked()
}

func (f *FakeD75) stateLocked() FakeState {
	return FakeState{FrequencyHz: f.freq[1], Mode: Mode(f.mode[1]), IFOutput: IFOutput(f.io)}
}

// Settings returns the six writable fields as the radio would report them.
func (f *FakeD75) Settings() map[Field]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[Field]string{
		FieldFQ: FormatFrequency(f.freq[1]),
		FieldMD: strconv.Itoa(f.mode[1]),
		FieldIO: strconv.Itoa(f.io),
		FieldFT: strconv.Itoa(f.ft),
		FieldFS: strconv.Itoa(f.fs),
		FieldAI: strconv.Itoa(f.ai),
	}
}

// Received returns every line the fake has received, in order.
func (f *FakeD75) Received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received...)
}

// Pipe connects a client to the fake through an in-memory pipe and returns
// the client's end.
func (f *FakeD75) Pipe() io.ReadWriteCloser {
	client, radio := net.Pipe()
	f.attach(radio)
	return client
}

func (f *FakeD75) attach(conn io.ReadWriteCloser) {
	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()
	go f.serve(conn)
}

func (f *FakeD75) serve(conn io.ReadWriteCloser) {
	r := bufio.NewReader(conn)
	for {
		raw, err := r.ReadString('\r')
		if err != nil {
			return
		}
		raw = strings.TrimRight(raw, "\r")
		raw = strings.TrimLeft(raw, "\n")
		if raw == "" {
			continue
		}
		f.mu.Lock()
		f.received = append(f.received, raw)
		latency, silent := f.Latency, f.Silent
		f.mu.Unlock()
		if silent {
			continue
		}
		reply := f.Handle(raw)
		if latency > 0 {
			time.Sleep(latency)
		}
		f.write(conn, reply+"\r")
	}
}

func (f *FakeD75) write(conn io.Writer, s string) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	conn.Write([]byte(s))
}

// Inject writes raw bytes to the client as if the radio sent them: NMEA
// sentences, APRS text or KISS frames.
func (f *FakeD75) Inject(b []byte) {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn != nil {
		f.writeMu.Lock()
		conn.Write(b)
		f.writeMu.Unlock()
	}
}

// TurnKnob changes band B's frequency as the owner would by hand. With AI on
// it pushes an unsolicited FQ line (the format is a guess: see
// HARDWARE_QUESTIONS.md).
func (f *FakeD75) TurnKnob(hz int64) {
	f.mu.Lock()
	f.freq[1] = hz
	ai := f.ai
	st := f.stateLocked()
	f.mu.Unlock()
	f.changed(st)
	if ai == 1 {
		f.Inject([]byte("FQ 1," + FormatFrequency(hz) + "\r"))
	}
}

// SetByHand changes a writable setting as the owner would on the radio.
func (f *FakeD75) SetByHand(field Field, v string) {
	f.mu.Lock()
	n, _ := strconv.ParseInt(v, 10, 64)
	switch field {
	case FieldFQ:
		f.freq[1] = n
	case FieldMD:
		f.mode[1] = int(n)
	case FieldIO:
		f.io = int(n)
	case FieldFT:
		f.ft = int(n)
	case FieldFS:
		f.fs = int(n)
	case FieldAI:
		f.ai = int(n)
	}
	st := f.stateLocked()
	f.mu.Unlock()
	f.changed(st)
}

// Unplug drops the connection, as pulling the USB cable would.
func (f *FakeD75) Unplug() {
	f.mu.Lock()
	conn := f.conn
	f.conn = nil
	f.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (f *FakeD75) changed(st FakeState) {
	if f.OnChange != nil {
		f.OnChange(st)
	}
}

// Handle answers one received line.
func (f *FakeD75) Handle(raw string) string {
	l, err := ParseLine(raw)
	if err != nil {
		return "?"
	}
	f.mu.Lock()
	if f.NotAvailable[l.Cmd] {
		f.mu.Unlock()
		return "N"
	}
	reply, changed, ok := f.handleLocked(l)
	st := f.stateLocked()
	f.mu.Unlock()
	if !ok {
		return "?"
	}
	if changed {
		f.changed(st)
	}
	return reply
}

func (f *FakeD75) handleLocked(l Line) (reply string, changed, ok bool) {
	p := l.Params
	band := func() (int, bool) {
		if len(p) < 1 || (p[0] != "0" && p[0] != "1") {
			return 0, false
		}
		return int(p[0][0] - '0'), true
	}
	digit := func(s string, hi int) (int, bool) {
		v, err := parseDigit(s, 0, hi)
		return v, err == nil
	}
	switch l.Cmd {
	case "ID":
		return "ID " + f.Model, false, len(p) == 0
	case "FV":
		return "FV " + f.Firmware, false, len(p) == 0
	case "FQ":
		b, okb := band()
		if !okb || len(p) > 2 {
			return "", false, false
		}
		if len(p) == 2 {
			hz, err := ParseFrequency(p[1])
			if err != nil {
				return "", false, false
			}
			if f.SnapHz > 0 {
				hz = (hz + f.SnapHz/2) / f.SnapHz * f.SnapHz
			}
			f.freq[b] = hz
			changed = true
		}
		return fmt.Sprintf("FQ %d,%s", b, FormatFrequency(f.freq[b])), changed, true
	case "MD":
		b, okb := band()
		if !okb || len(p) > 2 {
			return "", false, false
		}
		if len(p) == 2 {
			m, okm := digit(p[1], 9)
			if !okm {
				return "", false, false
			}
			f.mode[b] = m
			if f.ModeChangeSnapsHz > 0 {
				f.freq[b] = f.freq[b] / f.ModeChangeSnapsHz * f.ModeChangeSnapsHz
			}
			changed = true
		}
		return fmt.Sprintf("MD %d,%d", b, f.mode[b]), changed, true
	case "FO":
		b, okb := band()
		if !okb || len(p) != 1 {
			return "", false, false
		}
		fields := []string{
			strconv.Itoa(b), FormatFrequency(f.freq[b]), FormatFrequency(0), "0", "0",
			strconv.Itoa(f.mode[b]), strconv.Itoa(f.ft), strconv.Itoa(f.fs),
			"0", "0", "0", "0", "0", "0", "08", "08", "000", "0", "CQCQCQ", "0", "00",
		}
		return "FO " + strings.Join(fields, ","), false, true
	case "BY":
		b, okb := band()
		if !okb || len(p) != 1 {
			return "", false, false
		}
		return fmt.Sprintf("BY %d,%d", b, f.busy[b]), false, true
	case "SH":
		if len(p) != 1 {
			return "", false, false
		}
		m, okm := digit(p[0], 2)
		if !okm {
			return "", false, false
		}
		return fmt.Sprintf("SH %d,%d", m, f.sh[m]), false, true
	case "IO", "FT", "FS", "AI":
		ptr, hi := f.simple(l.Cmd)
		if len(p) > 1 {
			return "", false, false
		}
		if len(p) == 1 {
			v, okv := digit(p[0], hi)
			if !okv {
				return "", false, false
			}
			*ptr = v
			changed = true
		}
		return fmt.Sprintf("%s %d", l.Cmd, *ptr), changed, true
	}
	return "", false, false
}

func (f *FakeD75) simple(cmd string) (*int, int) {
	switch cmd {
	case "IO":
		return &f.io, 2
	case "FT":
		return &f.ft, 1
	case "FS":
		return &f.fs, 3
	default:
		return &f.ai, 1
	}
}
