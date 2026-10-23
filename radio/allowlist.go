package radio

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The safety allow-list. Every line the program sends goes through Check,
// whether it came from typed code, the probe or the CAT console. There is no
// bypass.

var (
	// ErrBlocked marks a command that is hard-blocked because it can
	// transmit, take over the port, enter a special mode or change memory.
	ErrBlocked = errors.New("command is blocked")
	// ErrNotAllowed marks a command, or a form of one, that is not on the
	// allow-list.
	ErrNotAllowed = errors.New("command is not on the allow-list")
)

// PolicyError explains why Check refused a line.
type PolicyError struct {
	Line   string
	Reason string
	kind   error // ErrBlocked or ErrNotAllowed
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("%v: %q: %s", e.kind, e.Line, e.Reason)
}

func (e *PolicyError) Unwrap() error { return e.kind }

// BlockedCommands are refused outright, with their reason. The list is the
// spec's; "0M" and "0G" are the first words of "0M PROGRAM" and
// "0G KENWOOD".
var BlockedCommands = map[string]string{
	"TX": "transmits",
	"BE": "sends an APRS beacon (transmits)",
	"PT": "controls APRS beaconing (transmits)",
	"TN": "enters KISS TNC mode, which takes over the port",
	"AS": "sets the TNC baud rate (KISS mode)",
	"0M": "enters programming mode",
	"0G": "enters service mode",
	"SR": "resets the radio",
	"PS": "controls power",
	"ME": "writes memory channels",
	"MR": "changes memory channels",
	"DW": "emulates the microphone down key",
	"UP": "emulates the microphone up key",
	"BT": "controls Bluetooth",
	"PC": "sets transmit output power",
}

// WritableFields are the only settings the program may change.
var WritableFields = []Field{FieldFQ, FieldMD, FieldIO, FieldFT, FieldFS, FieldAI}

// writableModes excludes DV (1) and DR (7): D-STAR is a non-goal.
var writableModes = map[string]bool{"0": true, "2": true, "3": true, "4": true, "5": true, "6": true, "8": true, "9": true}

// IsWrite reports whether an allowed line is a write (it sets a value).
// It is only meaningful for lines that pass Check.
func IsWrite(l Line) bool {
	switch l.Cmd {
	case "FQ", "MD":
		return len(l.Params) == 2
	case "IO", "FT", "FS", "AI":
		return len(l.Params) == 1
	}
	return false
}

// Check returns nil if the line may be sent to the radio.
func Check(l Line) error {
	raw := l.String()
	// Case-insensitive, in case the radio accepts lower case.
	if reason, ok := BlockedCommands[strings.ToUpper(l.Cmd)]; ok {
		return &PolicyError{Line: raw, Reason: reason, kind: ErrBlocked}
	}
	deny := func(format string, args ...any) error {
		return &PolicyError{Line: raw, Reason: fmt.Sprintf(format, args...), kind: ErrNotAllowed}
	}
	for _, p := range l.Params {
		if !allDigits(p) {
			return deny("parameter %q is not numeric", p)
		}
	}
	p := l.Params
	switch l.Cmd {
	case "ID", "FV":
		if len(p) != 0 {
			return deny("%s takes no parameters", l.Cmd)
		}
	case "FO", "BY":
		if len(p) != 1 {
			return deny("only reads of %s are allowed", l.Cmd)
		}
		if p[0] != BandB {
			return deny("%s must target band B", l.Cmd)
		}
	case "SH":
		if len(p) != 1 {
			return deny("SH is read-only")
		}
		if !oneOf(p[0], "0", "1", "2") {
			return deny("SH filter mode must be 0, 1 or 2")
		}
	case "FQ":
		if len(p) < 1 || len(p) > 2 {
			return deny("FQ takes a band and, to set, a frequency")
		}
		if p[0] != BandB {
			return deny("FQ must target band B")
		}
		if len(p) == 2 {
			hz, err := ParseFrequency(p[1])
			if err != nil {
				return deny("%v", err)
			}
			if hz < BandBMinHz || hz > BandBMaxHz {
				return deny("%d Hz is outside band B (%d–%d Hz)", hz, BandBMinHz, BandBMaxHz)
			}
		}
	case "MD":
		if len(p) < 1 || len(p) > 2 {
			return deny("MD takes a band and, to set, a mode")
		}
		if p[0] != BandB {
			return deny("MD must target band B")
		}
		if len(p) == 2 && !writableModes[p[1]] {
			return deny("mode %q may not be set", p[1])
		}
	case "IO":
		if len(p) > 1 || (len(p) == 1 && !oneOf(p[0], "0", "1", "2")) {
			return deny("IO takes no parameter or one of 0, 1, 2")
		}
	case "FT", "AI":
		if len(p) > 1 || (len(p) == 1 && !oneOf(p[0], "0", "1")) {
			return deny("%s takes no parameter or 0 or 1", l.Cmd)
		}
	case "FS":
		if len(p) > 1 || (len(p) == 1 && !oneOf(p[0], "0", "1", "2", "3")) {
			return deny("FS takes no parameter or one of 0..3")
		}
	default:
		return deny("unknown command")
	}
	return nil
}

// CheckRaw parses console text and checks it. It is the console's only way in.
func CheckRaw(s string) (Line, error) {
	l, err := ParseLine(s)
	if err != nil {
		return Line{}, &PolicyError{Line: strconv.Quote(s), Reason: err.Error(), kind: ErrNotAllowed}
	}
	return l, Check(l)
}

func oneOf(s string, options ...string) bool {
	for _, o := range options {
		if s == o {
			return true
		}
	}
	return false
}
