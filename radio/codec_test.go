package radio

import (
	"errors"
	"slices"
	"testing"
)

func TestParseLineRoundTrip(t *testing.T) {
	cases := []struct {
		raw    string
		cmd    string
		params []string
	}{
		{"ID", "ID", nil},
		{"ID TH-D75", "ID", []string{"TH-D75"}},
		{"FV 1.03", "FV", []string{"1.03"}},
		{"FQ 1", "FQ", []string{"1"}},
		{"FQ 1,0007074000", "FQ", []string{"1", "0007074000"}},
		{"MD 1,4", "MD", []string{"1", "4"}},
		{"IO", "IO", nil},
		{"IO 1", "IO", []string{"1"}},
		{"FT 0", "FT", []string{"0"}},
		{"FS 3", "FS", []string{"3"}},
		{"SH 0,2", "SH", []string{"0", "2"}},
		{"BY 1,0", "BY", []string{"1", "0"}},
		{"AI 1", "AI", []string{"1"}},
		{"0M PROGRAM", "0M", []string{"PROGRAM"}},
	}
	for _, c := range cases {
		l, err := ParseLine(c.raw)
		if err != nil {
			t.Fatalf("ParseLine(%q): %v", c.raw, err)
		}
		if l.Cmd != c.cmd || !slices.Equal(l.Params, c.params) {
			t.Errorf("ParseLine(%q) = %q %q, want %q %q", c.raw, l.Cmd, l.Params, c.cmd, c.params)
		}
		if got := l.String(); got != c.raw {
			t.Errorf("String() = %q, want %q", got, c.raw)
		}
	}
}

func TestParseLineRejectsMalformed(t *testing.T) {
	for _, raw := range []string{"", " FQ 1", "FQ 1\rTX", "FQ\n1", "FQ 1,\x00", "ID\xff"} {
		if _, err := ParseLine(raw); err == nil {
			t.Errorf("ParseLine(%q) accepted", raw)
		}
	}
}

func TestIsError(t *testing.T) {
	if err, ok := IsError("?"); !ok || !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("? not recognised")
	}
	if err, ok := IsError("N"); !ok || !errors.Is(err, ErrNotAvailable) {
		t.Errorf("N not recognised")
	}
	if _, ok := IsError("NO"); ok {
		t.Errorf("NO treated as an error reply")
	}
}

func TestFrequencyFormat(t *testing.T) {
	if got := FormatFrequency(7_074_000); got != "0007074000" {
		t.Errorf("FormatFrequency = %q", got)
	}
	if got := FormatFrequency(524_000_000); got != "0524000000" {
		t.Errorf("FormatFrequency = %q", got)
	}
	hz, err := ParseFrequency("0145500000")
	if err != nil || hz != 145_500_000 {
		t.Errorf("ParseFrequency = %d, %v", hz, err)
	}
	for _, bad := range []string{"", "7074000", "00070740000", "000707400a", "-007074000"} {
		if _, err := ParseFrequency(bad); err == nil {
			t.Errorf("ParseFrequency(%q) accepted", bad)
		}
	}
}

func TestParseVFO(t *testing.T) {
	l, _ := ParseLine("FO 1,0007074000,0000000000,0,0,4,1,0,0,0,0,0,0,0,08,08,000,0,CQCQCQ,0,00")
	v, err := ParseVFO(l.Params)
	if err != nil {
		t.Fatal(err)
	}
	if v.FrequencyHz != 7_074_000 || v.Mode != ModeUSB || !v.FineMode || v.FineStep.Hz() != 20 {
		t.Errorf("ParseVFO = %+v", v)
	}
	if len(v.Raw) != FOFields {
		t.Errorf("raw has %d fields", len(v.Raw))
	}
	short, _ := ParseLine("FO 1,0007074000,0000000000,0")
	if _, err := ParseVFO(short.Params); !errors.Is(err, ErrBadReply) {
		t.Errorf("short FO: err = %v", err)
	}
}

func TestModeNames(t *testing.T) {
	for m := ModeFM; m <= ModeRCW; m++ {
		back, err := ParseModeName(m.String())
		if err != nil || back != m {
			t.Errorf("mode %d: %q -> %v, %v", m, m.String(), back, err)
		}
	}
}

func TestFilterWidths(t *testing.T) {
	if FilterWidthHz(FilterSSB, 1) != 2400 || FilterWidthHz(FilterCW, 0) != 300 || FilterWidthHz(FilterAM, 4) != 0 {
		t.Error("filter table mismatch")
	}
}
