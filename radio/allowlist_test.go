package radio

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// blockedExamples gives a plausible full line for each hard-blocked command.
var blockedExamples = map[string]string{
	"TX": "TX",
	"BE": "BE",
	"PT": "PT 1",
	"TN": "TN 1,0",
	"AS": "AS 1",
	"0M": "0M PROGRAM",
	"0G": "0G KENWOOD",
	"SR": "SR",
	"PS": "PS 0",
	"ME": "ME 000,0118700000,0000000000,5,5,2,0,1,0,0,0,0,0,0,0,08,08,000,0,CQCQCQ,0,00,0",
	"MR": "MR 1,000",
	"DW": "DW",
	"UP": "UP",
	"BT": "BT 1",
	"PC": "PC 1,0",
}

func TestEveryBlockedCommandHasAnExample(t *testing.T) {
	for cmd := range BlockedCommands {
		if _, ok := blockedExamples[cmd]; !ok {
			t.Errorf("no example for blocked command %s", cmd)
		}
	}
}

// TestBlockedCommands sends each blocked command through the typed path and
// the console path, and checks that it is refused and never reaches the radio.
func TestBlockedCommands(t *testing.T) {
	for cmd, example := range blockedExamples {
		t.Run(cmd, func(t *testing.T) {
			fake := NewFakeD75()
			s := NewSession(fake.Pipe(), SessionConfig{Timeout: 200 * time.Millisecond})
			defer s.Close()
			ctx := context.Background()

			l, err := ParseLine(example)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Send(ctx, l); !errors.Is(err, ErrBlocked) {
				t.Errorf("typed path: %q: err = %v, want ErrBlocked", example, err)
			}
			if _, err := s.Console(ctx, example); !errors.Is(err, ErrBlocked) {
				t.Errorf("console path: %q: err = %v, want ErrBlocked", example, err)
			}
			if _, err := s.Console(ctx, strings.ToLower(example)); err == nil {
				t.Errorf("console path: lower-case %q accepted", example)
			}
			// Bare command too, in case a read form looks harmless.
			if _, err := s.Console(ctx, cmd); !errors.Is(err, ErrBlocked) {
				t.Errorf("console path: bare %q: err = %v", cmd, err)
			}
			// Prove the session works, then that nothing blocked got through.
			if _, err := s.Console(ctx, "ID"); err != nil {
				t.Fatalf("ID: %v", err)
			}
			if got := fake.Received(); !slices.Equal(got, []string{"ID"}) {
				t.Errorf("radio received %q, want only ID", got)
			}
		})
	}
}

func TestAllowedLines(t *testing.T) {
	ok := []string{
		"ID", "FV", "FO 1", "BY 1", "SH 0", "SH 1", "SH 2",
		"FQ 1", "FQ 1,0007074000", "FQ 1,0000100000", "FQ 1,0524000000",
		"MD 1", "MD 1,0", "MD 1,2", "MD 1,3", "MD 1,4", "MD 1,5", "MD 1,6", "MD 1,8", "MD 1,9",
		"IO", "IO 0", "IO 1", "IO 2", "FT", "FT 0", "FT 1",
		"FS", "FS 0", "FS 3", "AI", "AI 0", "AI 1",
	}
	for _, raw := range ok {
		if _, err := CheckRaw(raw); err != nil {
			t.Errorf("%q refused: %v", raw, err)
		}
	}
}

func TestRefusedLines(t *testing.T) {
	refused := map[string]string{
		"FQ 0":              "band A read",
		"FQ 0,0145500000":   "band A write",
		"MD 0,4":            "band A mode",
		"FO 0":              "band A VFO",
		"BY 0":              "band A squelch",
		"FQ 1,0000099999":   "below band B",
		"FQ 1,0524000001":   "above band B",
		"FQ 1,7074000":      "short frequency",
		"FQ 1,0007074000,1": "extra parameter",
		"MD 1,1":            "DV",
		"MD 1,7":            "DR",
		"MD 1,10":           "two-digit mode",
		"IO 3":              "bad IO",
		"FT 2":              "bad FT",
		"FS 4":              "bad FS",
		"AI 2":              "bad AI",
		"SH 0,1":            "SH write",
		"SH 3":              "bad SH mode",
		"FO 1,0007074000,0000000000,0,0,4,0,0,0,0,0,0,0,0,08,08,000,0,CQCQCQ,0,00": "FO write",
		"ID 1":           "ID with parameter",
		"SM 1":           "S-meter squelch (not in the table)",
		"RA 1":           "attenuator (not in the table yet)",
		"AE":             "serial number (not in the table)",
		"RX":             "unknown",
		"FQ 1,-07074000": "non-numeric",
		"FQ  1":          "double space",
		"IO 1\rTX":       "embedded terminator",
		"io 1":           "lower case",
	}
	for raw, why := range refused {
		if _, err := CheckRaw(raw); err == nil {
			t.Errorf("%q accepted (%s)", raw, why)
		}
	}
}

func TestIsWrite(t *testing.T) {
	for raw, want := range map[string]bool{
		"FQ 1": false, "FQ 1,0007074000": true, "MD 1": false, "MD 1,4": true,
		"IO": false, "IO 1": true, "FT 1": true, "FS 0": true, "AI 1": true, "ID": false, "SH 0": false,
	} {
		l, _ := ParseLine(raw)
		if got := IsWrite(l); got != want {
			t.Errorf("IsWrite(%q) = %v, want %v", raw, got, want)
		}
	}
}
