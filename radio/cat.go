package radio

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Radio is the transport-agnostic view of the radio that the rest of the
// program uses. Every setter reads the value back and returns what the radio
// reports, which may differ from what was asked (for example a frequency
// rounded to the radio's step).
type Radio interface {
	Identify(ctx context.Context) (Identity, error)
	Frequency(ctx context.Context) (int64, error)
	SetFrequency(ctx context.Context, hz int64) (int64, error)
	Mode(ctx context.Context) (Mode, error)
	SetMode(ctx context.Context, m Mode) (Mode, error)
	IFOutput(ctx context.Context) (IFOutput, error)
	SetIFOutput(ctx context.Context, o IFOutput) (IFOutput, error)
	FineTune(ctx context.Context) (bool, error)
	SetFineTune(ctx context.Context, on bool) (bool, error)
	FineStep(ctx context.Context) (FineStep, error)
	SetFineStep(ctx context.Context, s FineStep) (FineStep, error)
	AutoInfo(ctx context.Context) (bool, error)
	SetAutoInfo(ctx context.Context, on bool) (bool, error)
	Filter(ctx context.Context, m FilterMode) (int, error)
	Squelch(ctx context.Context) (open bool, err error)
	VFO(ctx context.Context) (VFO, error)
	FieldAccess
	Events() <-chan Event
	Close() error
}

// FieldAccess reads and writes the six settings the program may change, as
// raw parameter text. Snapshot and restore work at this level so that they
// compare exactly what the radio reports.
type FieldAccess interface {
	ReadField(ctx context.Context, f Field) (string, error)
	WriteField(ctx context.Context, f Field, value string) (string, error)
}

// Identity is what the radio says it is.
type Identity struct {
	Model    string `json:"model"`
	Firmware string `json:"firmware"`
	// USBSerial comes from the USB descriptor, not CAT.
	USBSerial string `json:"usb_serial,omitempty"`
}

// Field names one writable setting, by its CAT command.
type Field string

const (
	FieldFQ Field = "FQ" // band B frequency
	FieldMD Field = "MD" // band B mode
	FieldIO Field = "IO" // USB output select (Menu 102)
	FieldFT Field = "FT" // fine tuning
	FieldFS Field = "FS" // fine step
	FieldAI Field = "AI" // auto-information
)

// hasBand reports whether the field's command takes band B as its first
// parameter.
func (f Field) hasBand() bool { return f == FieldFQ || f == FieldMD }

func (f Field) readLine() Line {
	if f.hasBand() {
		return Line{Cmd: string(f), Params: []string{BandB}}
	}
	return Line{Cmd: string(f)}
}

func (f Field) writeLine(v string) Line {
	if f.hasBand() {
		return Line{Cmd: string(f), Params: []string{BandB, v}}
	}
	return Line{Cmd: string(f), Params: []string{v}}
}

// value extracts the field's value from a reply.
func (f Field) value(reply Line) (string, error) {
	want := 1
	if f.hasBand() {
		want = 2
	}
	if len(reply.Params) != want {
		return "", fmt.Errorf("%w: %s reply %q has %d parameters, want %d", ErrBadReply, f, reply, len(reply.Params), want)
	}
	return reply.Params[want-1], nil
}

// CAT implements Radio over a CAT session.
type CAT struct {
	s *Session
}

// NewCAT starts a CAT session on rw (a serial port, a pseudo-terminal or a
// pipe to the fake radio).
func NewCAT(rw io.ReadWriteCloser, cfg SessionConfig) *CAT {
	return &CAT{s: NewSession(rw, cfg)}
}

// Session exposes the underlying session for the console and port checks.
func (c *CAT) Session() *Session { return c.s }

func (c *CAT) Events() <-chan Event { return c.s.Events() }
func (c *CAT) Close() error         { return c.s.Close() }

func (c *CAT) query(ctx context.Context, cmd string, params ...string) (Line, error) {
	return c.s.Send(ctx, Line{Cmd: cmd, Params: params})
}

// Identify reads ID and FV.
func (c *CAT) Identify(ctx context.Context) (Identity, error) {
	id, err := c.query(ctx, "ID")
	if err != nil {
		return Identity{}, err
	}
	fv, err := c.query(ctx, "FV")
	if err != nil {
		return Identity{Model: joinParams(id)}, err
	}
	return Identity{Model: joinParams(id), Firmware: joinParams(fv)}, nil
}

// joinParams returns a reply's parameter text as one string, for replies
// whose format is not pinned down (ID, FV).
func joinParams(l Line) string {
	return strings.Join(l.Params, ",")
}

func (c *CAT) ReadField(ctx context.Context, f Field) (string, error) {
	reply, err := c.s.Send(ctx, f.readLine())
	if err != nil {
		return "", err
	}
	return f.value(reply)
}

// WriteField sets a field and reads it back. The returned value is the
// read-back, never the value asked for.
func (c *CAT) WriteField(ctx context.Context, f Field, v string) (string, error) {
	reply, err := c.s.Send(ctx, f.writeLine(v))
	if err != nil {
		return "", err
	}
	if _, err := f.value(reply); err != nil {
		return "", err
	}
	return c.ReadField(ctx, f)
}

func (c *CAT) Frequency(ctx context.Context) (int64, error) {
	v, err := c.ReadField(ctx, FieldFQ)
	if err != nil {
		return 0, err
	}
	return ParseFrequency(v)
}

func (c *CAT) SetFrequency(ctx context.Context, hz int64) (int64, error) {
	v, err := c.WriteField(ctx, FieldFQ, FormatFrequency(hz))
	if err != nil {
		return 0, err
	}
	return ParseFrequency(v)
}

func (c *CAT) Mode(ctx context.Context) (Mode, error) {
	return readDigit[Mode](ctx, c, FieldMD, 9)
}

func (c *CAT) SetMode(ctx context.Context, m Mode) (Mode, error) {
	return writeDigit[Mode](ctx, c, FieldMD, int(m), 9)
}

func (c *CAT) IFOutput(ctx context.Context) (IFOutput, error) {
	return readDigit[IFOutput](ctx, c, FieldIO, 2)
}

func (c *CAT) SetIFOutput(ctx context.Context, o IFOutput) (IFOutput, error) {
	return writeDigit[IFOutput](ctx, c, FieldIO, int(o), 2)
}

func (c *CAT) FineTune(ctx context.Context) (bool, error) {
	v, err := readDigit[int](ctx, c, FieldFT, 1)
	return v == 1, err
}

func (c *CAT) SetFineTune(ctx context.Context, on bool) (bool, error) {
	v, err := writeDigit[int](ctx, c, FieldFT, boolDigit(on), 1)
	return v == 1, err
}

func (c *CAT) FineStep(ctx context.Context) (FineStep, error) {
	return readDigit[FineStep](ctx, c, FieldFS, 3)
}

func (c *CAT) SetFineStep(ctx context.Context, s FineStep) (FineStep, error) {
	return writeDigit[FineStep](ctx, c, FieldFS, int(s), 3)
}

func (c *CAT) AutoInfo(ctx context.Context) (bool, error) {
	v, err := readDigit[int](ctx, c, FieldAI, 1)
	return v == 1, err
}

func (c *CAT) SetAutoInfo(ctx context.Context, on bool) (bool, error) {
	v, err := writeDigit[int](ctx, c, FieldAI, boolDigit(on), 1)
	return v == 1, err
}

// Filter reads the radio's own receive filter index for a mode (SH).
func (c *CAT) Filter(ctx context.Context, m FilterMode) (int, error) {
	sel := strconv.Itoa(int(m))
	reply, err := c.query(ctx, "SH", sel)
	if err != nil {
		return 0, err
	}
	if len(reply.Params) != 2 {
		return 0, fmt.Errorf("%w: SH reply %q", ErrBadReply, reply)
	}
	return parseDigit(reply.Params[1], 0, 9)
}

// Squelch reads band B's squelch state (BY).
func (c *CAT) Squelch(ctx context.Context) (bool, error) {
	reply, err := c.query(ctx, "BY", BandB)
	if err != nil {
		return false, err
	}
	if len(reply.Params) != 2 {
		return false, fmt.Errorf("%w: BY reply %q", ErrBadReply, reply)
	}
	v, err := parseDigit(reply.Params[1], 0, 1)
	return v == 1, err
}

// VFO reads band B's VFO record (FO).
func (c *CAT) VFO(ctx context.Context) (VFO, error) {
	reply, err := c.query(ctx, "FO", BandB)
	if err != nil {
		return VFO{}, err
	}
	return ParseVFO(reply.Params)
}

func readDigit[T ~int](ctx context.Context, c *CAT, f Field, hi int) (T, error) {
	v, err := c.ReadField(ctx, f)
	if err != nil {
		return 0, err
	}
	d, err := parseDigit(v, 0, hi)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %v", ErrBadReply, f, err)
	}
	return T(d), nil
}

func writeDigit[T ~int](ctx context.Context, c *CAT, f Field, v, hi int) (T, error) {
	got, err := c.WriteField(ctx, f, strconv.Itoa(v))
	if err != nil {
		return 0, err
	}
	d, err := parseDigit(got, 0, hi)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %v", ErrBadReply, f, err)
	}
	return T(d), nil
}

func boolDigit(b bool) int {
	if b {
		return 1
	}
	return 0
}
