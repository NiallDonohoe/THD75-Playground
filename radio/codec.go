// Package radio talks to the TH-D75 over its CAT serial port: the command
// codec, the safety allow-list, the session goroutine that owns the port,
// discovery, snapshot and restore, and a fake TH-D75 for tests.
//
// Command syntax follows LA3QMA's TH-D74 reference (firmware V1.10). None of
// it is verified on a TH-D75 yet; see HARDWARE_QUESTIONS.md.
package radio

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Line is one CAT line without its terminator: a command name and its
// comma-separated parameters. "FQ 1,0007074000" is Cmd "FQ", Params
// ["1" "0007074000"]. "ID" has no parameters.
type Line struct {
	Cmd    string
	Params []string
}

// Errors returned by the radio for a command.
var (
	// ErrUnknownCommand is the radio's "?" reply: unknown or malformed.
	ErrUnknownCommand = errors.New("radio replied ? (unknown or malformed command)")
	// ErrNotAvailable is the radio's "N" reply: not available in this state.
	ErrNotAvailable = errors.New("radio replied N (not available in the current state)")
	// ErrBadReply means the reply did not echo the command or could not be parsed.
	ErrBadReply = errors.New("reply does not match the command")
)

// ParseLine parses one line of CAT text. It accepts only printable ASCII and
// rejects embedded terminators, so a console user cannot smuggle a second
// command into one line.
func ParseLine(s string) (Line, error) {
	if s == "" {
		return Line{}, errors.New("empty line")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return Line{}, fmt.Errorf("non-printable byte 0x%02x at offset %d", s[i], i)
		}
	}
	cmd, rest, hasParams := strings.Cut(s, " ")
	if cmd == "" {
		return Line{}, errors.New("missing command name")
	}
	l := Line{Cmd: cmd}
	if hasParams {
		l.Params = strings.Split(rest, ",")
	}
	return l, nil
}

// String formats the line as it goes on the wire, without the terminator.
func (l Line) String() string {
	if len(l.Params) == 0 {
		return l.Cmd
	}
	return l.Cmd + " " + strings.Join(l.Params, ",")
}

// IsError reports whether a reply line is one of the radio's error replies,
// and returns the matching error.
func IsError(reply string) (error, bool) {
	switch reply {
	case "?":
		return ErrUnknownCommand, true
	case "N":
		return ErrNotAvailable, true
	}
	return nil, false
}

// Band B is the only band this program reads or writes.
const BandB = "1"

// Band B's receive range, in hertz (TH-D75A: 0.1–524 MHz).
const (
	BandBMinHz = 100_000
	BandBMaxHz = 524_000_000
)

// FormatFrequency formats hertz as the 10-digit field FQ and FO use.
func FormatFrequency(hz int64) string {
	return fmt.Sprintf("%010d", hz)
}

// ParseFrequency parses a 10-digit frequency field.
func ParseFrequency(s string) (int64, error) {
	if len(s) != 10 || !allDigits(s) {
		return 0, fmt.Errorf("frequency %q is not 10 digits", s)
	}
	return strconv.ParseInt(s, 10, 64)
}

// Mode is band B's operating mode (MD).
type Mode int

const (
	ModeFM  Mode = 0
	ModeDV  Mode = 1
	ModeAM  Mode = 2
	ModeLSB Mode = 3
	ModeUSB Mode = 4
	ModeCW  Mode = 5
	ModeNFM Mode = 6
	ModeDR  Mode = 7
	ModeWFM Mode = 8
	ModeRCW Mode = 9
)

var modeNames = [...]string{"FM", "DV", "AM", "LSB", "USB", "CW", "NFM", "DR", "WFM", "R-CW"}

func (m Mode) String() string {
	if m >= 0 && int(m) < len(modeNames) {
		return modeNames[m]
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// ParseModeName maps a name such as "USB" to its Mode.
func ParseModeName(s string) (Mode, error) {
	for i, n := range modeNames {
		if strings.EqualFold(n, s) {
			return Mode(i), nil
		}
	}
	return 0, fmt.Errorf("unknown mode %q", s)
}

// IFOutput is the USB audio output select (IO, Menu 102). The spec calls 0
// "AF"; the D74 reference calls it "Off". Which is right is a hardware
// question, so the name says both.
type IFOutput int

const (
	IOAF     IFOutput = 0
	IOIF     IFOutput = 1
	IODetect IFOutput = 2
)

func (o IFOutput) String() string {
	switch o {
	case IOAF:
		return "AF/Off"
	case IOIF:
		return "IF"
	case IODetect:
		return "Detect"
	}
	return fmt.Sprintf("IFOutput(%d)", int(o))
}

// FineStep is the fine-tuning step (FS).
type FineStep int

// Hz returns the step size in hertz.
func (s FineStep) Hz() int {
	switch s {
	case 0:
		return 20
	case 1:
		return 100
	case 2:
		return 500
	case 3:
		return 1000
	}
	return 0
}

func (s FineStep) String() string {
	if hz := s.Hz(); hz > 0 {
		return fmt.Sprintf("%d Hz", hz)
	}
	return fmt.Sprintf("FineStep(%d)", int(s))
}

// FilterMode selects which of the radio's receive filters SH reads.
type FilterMode int

const (
	FilterSSB FilterMode = 0
	FilterCW  FilterMode = 1
	FilterAM  FilterMode = 2
)

func (f FilterMode) String() string {
	switch f {
	case FilterSSB:
		return "SSB"
	case FilterCW:
		return "CW"
	case FilterAM:
		return "AM"
	}
	return fmt.Sprintf("FilterMode(%d)", int(f))
}

// filterWidths is the SH index table from the D74 reference, in hertz:
// SSB high cut, CW width, AM high cut. 0 marks an index the reference lists
// as "N" (not available).
var filterWidths = [3][5]int{
	{2200, 2400, 2600, 2800, 3000},
	{300, 500, 1000, 1500, 2000},
	{3000, 4500, 6000, 7500, 0},
}

// FilterWidthHz returns the D74 reference's width for an SH index, or 0 if
// the index is unknown.
func FilterWidthHz(mode FilterMode, index int) int {
	if mode < 0 || int(mode) >= len(filterWidths) || index < 0 || index >= len(filterWidths[0]) {
		return 0
	}
	return filterWidths[mode][index]
}

// VFO is band B's full VFO record (FO), kept raw because only some fields
// are understood. Field numbers below are the reference's p1..p21.
type VFO struct {
	Raw         []string `json:"raw"`
	FrequencyHz int64    `json:"frequency_hz"` // p2
	OffsetHz    int64    `json:"offset_hz"`    // p3
	Step        string   `json:"step"`         // p4, step-size code
	TxStep      string   `json:"tx_step"`      // p5
	Mode        Mode     `json:"mode"`         // p6
	FineMode    bool     `json:"fine_mode"`    // p7
	FineStep    FineStep `json:"fine_step"`    // p8
}

// FOFields is the number of parameters in an FO reply, band included.
const FOFields = 21

// ParseVFO parses the parameters of an FO reply.
func ParseVFO(params []string) (VFO, error) {
	if len(params) != FOFields {
		return VFO{}, fmt.Errorf("%w: FO has %d fields, want %d", ErrBadReply, len(params), FOFields)
	}
	v := VFO{Raw: append([]string(nil), params...)}
	var err error
	if v.FrequencyHz, err = ParseFrequency(params[1]); err != nil {
		return VFO{}, fmt.Errorf("%w: FO frequency: %v", ErrBadReply, err)
	}
	if v.OffsetHz, err = ParseFrequency(params[2]); err != nil {
		return VFO{}, fmt.Errorf("%w: FO offset: %v", ErrBadReply, err)
	}
	v.Step, v.TxStep = params[3], params[4]
	m, err := parseDigit(params[5], 0, 9)
	if err != nil {
		return VFO{}, fmt.Errorf("%w: FO mode: %v", ErrBadReply, err)
	}
	v.Mode = Mode(m)
	fm, err := parseDigit(params[6], 0, 1)
	if err != nil {
		return VFO{}, fmt.Errorf("%w: FO fine mode: %v", ErrBadReply, err)
	}
	v.FineMode = fm == 1
	fs, err := parseDigit(params[7], 0, 3)
	if err != nil {
		return VFO{}, fmt.Errorf("%w: FO fine step: %v", ErrBadReply, err)
	}
	v.FineStep = FineStep(fs)
	return v, nil
}

// StepSizeHz decodes the reference's step-size code (FO p4/p5).
func StepSizeHz(code string) float64 {
	steps := map[string]float64{
		"0": 5000, "1": 6250, "2": 8330, "3": 9000, "4": 10000, "5": 12500,
		"6": 15000, "7": 20000, "8": 25000, "9": 30000, "A": 50000, "B": 100000,
	}
	return steps[code]
}

func parseDigit(s string, lo, hi int) (int, error) {
	if len(s) != 1 || s[0] < '0' || s[0] > '9' {
		return 0, fmt.Errorf("%q is not a single digit", s)
	}
	v := int(s[0] - '0')
	if v < lo || v > hi {
		return 0, fmt.Errorf("%d is outside %d..%d", v, lo, hi)
	}
	return v, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
