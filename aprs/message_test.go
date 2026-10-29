package aprs

import (
	"strings"
	"testing"
	"time"
)

func TestMessages(t *testing.T) {
	cases := []struct {
		info string
		want Message
	}{
		{":WU2Z     :Testing{003", Message{To: "WU2Z", Text: "Testing", ID: "003"}},
		{":WU2Z     :Testing", Message{To: "WU2Z", Text: "Testing"}},
		{":KB2ICI-14:ack003", Message{Kind: MessageAck, To: "KB2ICI-14", ID: "003"}},
		{":KB2ICI-14:rej003", Message{Kind: MessageRej, To: "KB2ICI-14", ID: "003"}},
		{":KB2ICI-14:ack003 ", Message{Kind: MessageAck, To: "KB2ICI-14", ID: "003"}},
		// Reply-acks: "{MM}" says the sender supports them; "{MM}AA" also acks AA.
		{":WU2Z     :Hi there{AB}", Message{To: "WU2Z", Text: "Hi there", ID: "AB", ReplyAcks: true}},
		{":WU2Z     :Hi there{AB}CD", Message{To: "WU2Z", Text: "Hi there", ID: "AB", ReplyAcks: true, ReplyAck: "CD"}},
		{":WU2Z     :ackAB}CD", Message{Kind: MessageAck, To: "WU2Z", ID: "AB", ReplyAcks: true, ReplyAck: "CD"}},
		// Text that starts with "ack" but is not an ack.
		{":WU2Z     :acknowledged{12", Message{To: "WU2Z", Text: "acknowledged", ID: "12"}},
		{":WU2Z     :acknowledged", Message{To: "WU2Z", Text: "acknowledged"}},
		// A brace that is not a message number stays in the text.
		{":WU2Z     :x{toolong1", Message{To: "WU2Z", Text: "x{toolong1"}},
		{":BLN1     :Snow on the hills", Message{To: "BLN1", Text: "Snow on the hills"}},
	}
	for _, c := range cases {
		d := decodeInfo(t, "APRS", c.info)
		if d.Kind != KindMessage || d.Message == nil {
			t.Fatalf("%q: Kind = %v", c.info, d.Kind)
		}
		if *d.Message != c.want {
			t.Errorf("%q: %+v, want %+v", c.info, *d.Message, c.want)
		}
	}
	if !(Message{To: "BLN1"}).IsBulletin() || (Message{To: "EI2ABC"}).IsBulletin() {
		t.Errorf("IsBulletin")
	}
}

func TestMessageInfo(t *testing.T) {
	s, err := MessageInfo("WU2Z", "Testing", "003")
	if err != nil || s != ":WU2Z     :Testing{003" {
		t.Errorf("MessageInfo = %q, %v", s, err)
	}
	if s, _ := MessageInfo("EI2ABC-10", "No ack wanted", ""); s != ":EI2ABC-10:No ack wanted" {
		t.Errorf("MessageInfo without number = %q", s)
	}
	if s, _ := AckInfo("KB2ICI-14", "003"); s != ":KB2ICI-14:ack003" {
		t.Errorf("AckInfo = %q", s)
	}
	if s, _ := RejInfo("KB2ICI-14", "003"); s != ":KB2ICI-14:rej003" {
		t.Errorf("RejInfo = %q", s)
	}

	// Round trip through the decoder.
	s, _ = MessageInfo("EI2ABC-9", "See you on 145.500", "42")
	d := decodeInfo(t, "APZD75", s)
	if m := d.Message; m == nil || m.To != "EI2ABC-9" || m.Text != "See you on 145.500" || m.ID != "42" {
		t.Errorf("round trip: %+v", d.Message)
	}

	for name, args := range map[string][3]string{
		"empty text":   {"WU2Z", "", "1"},
		"68 chars":     {"WU2Z", strings.Repeat("x", 68), "1"},
		"brace":        {"WU2Z", "a{b", "1"},
		"pipe":         {"WU2Z", "a|b", "1"},
		"tilde":        {"WU2Z", "a~b", "1"},
		"control":      {"WU2Z", "a\rb", "1"},
		"non-ASCII":    {"WU2Z", "café", "1"},
		"long number":  {"WU2Z", "hi", "123456"},
		"bad number":   {"WU2Z", "hi", "1-2"},
		"long to":      {"TOOLONGADDR", "hi", "1"},
		"empty to":     {"", "hi", "1"},
		"colon in to:": {"WU:2Z", "hi", "1"},
	} {
		if _, err := MessageInfo(args[0], args[1], args[2]); err == nil {
			t.Errorf("MessageInfo %s: accepted", name)
		}
	}
	if _, err := AckInfo("WU2Z", ""); err == nil {
		t.Errorf("AckInfo without a number: accepted")
	}
	if _, err := MessageInfo("WU2Z", strings.Repeat("x", MaxMessageText), "1"); err != nil {
		t.Errorf("67 characters refused: %v", err)
	}
}

func TestPositionInfo(t *testing.T) {
	home := Position{Lat: 49 + 3.50/60, Lon: -(72 + 1.75/60), Symbol: Symbol{'/', '-'}}
	s, err := PositionInfo(home, "Test", true)
	if err != nil || s != "=4903.50N/07201.75W-Test" {
		t.Errorf("PositionInfo = %q, %v", s, err)
	}

	moving := home
	moving.Symbol = Symbol{'/', '>'}
	moving.HasCourseSpeed, moving.Course, moving.Speed = true, 88, 36
	moving.HasAltitude, moving.Altitude = true, 1234*0.3048
	s, err = PositionInfo(moving, "Hi", false)
	if err != nil || s != "!4903.50N/07201.75W>088/036/A=001234Hi" {
		t.Fatalf("PositionInfo = %q, %v", s, err)
	}
	d := decodeInfo(t, "APZD75", s)
	checkPos(t, s, d, wantPos{lat: moving.Lat, lon: moving.Lon, sym: "/>", hasCourse: true, course: 88, speed: 36, hasAlt: true, alt: 1234 * 0.3048, comment: "Hi"})

	// Rounding that carries into the degrees, and the southern and eastern
	// hemispheres.
	s, _ = PositionInfo(Position{Lat: -(49.99999), Lon: 151.2, Symbol: Symbol{'\\', '#'}}, "", false)
	if s != "!5000.00S\\15112.00E#" {
		t.Errorf("PositionInfo = %q", s)
	}
	s, _ = PositionInfo(Position{Lat: 10, Lon: 1, Symbol: Symbol{'/', '-'}, HasAltitude: true, Altitude: -12 * 0.3048}, "", false)
	if s != "!1000.00N/00100.00E-/A=-00012" {
		t.Errorf("negative altitude: %q", s)
	}

	for name, c := range map[string]struct {
		p       Position
		comment string
	}{
		"latitude 91":            {Position{Lat: 91, Symbol: Symbol{'/', '-'}}, ""},
		"longitude":              {Position{Lon: -181, Symbol: Symbol{'/', '-'}}, ""},
		"table":                  {Position{Symbol: Symbol{'x', '-'}}, ""},
		"code":                   {Position{Symbol: Symbol{'/', ' '}}, ""},
		"comment 44":             {Position{Symbol: Symbol{'/', '-'}}, strings.Repeat("x", 44)},
		"comment pipe":           {Position{Symbol: Symbol{'/', '-'}}, "a|b"},
		"course 361":             {Position{Symbol: Symbol{'/', '-'}, HasCourseSpeed: true, Course: 361}, ""},
		"comment 37 with course": {Position{Symbol: Symbol{'/', '-'}, HasCourseSpeed: true, Course: 1}, strings.Repeat("x", 37)},
	} {
		if _, err := PositionInfo(c.p, c.comment, false); err == nil {
			t.Errorf("PositionInfo %s: accepted", name)
		}
	}
	if _, err := PositionInfo(Position{Symbol: Symbol{'/', '-'}}, strings.Repeat("x", 43), false); err != nil {
		t.Errorf("43-character comment refused: %v", err)
	}
}

func TestParseTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	dublin := time.FixedZone("IST", 3600)
	cases := []struct {
		ts   string
		now  time.Time
		want time.Time
	}{
		{"291430z", now, time.Date(2026, 10, 29, 14, 30, 0, 0, time.UTC)},
		{"011200z", now, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
		{"291545z", now, time.Date(2026, 10, 29, 15, 45, 0, 0, time.UTC)}, // a fast clock
		{"301200z", now, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)},
		{"311200z", now, time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, // September has no 31st
		{"311200z", time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)},
		{"291600/", now.In(dublin), time.Date(2026, 10, 29, 16, 0, 0, 0, dublin)},
		{"141500h", now, time.Date(2026, 10, 29, 14, 15, 0, 0, time.UTC)},
		{"235959h", now, time.Date(2026, 10, 28, 23, 59, 59, 0, time.UTC)},
	}
	for _, c := range cases {
		got, err := ParseTimestamp(c.ts, c.now)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("ParseTimestamp(%q) = %v, %v, want %v", c.ts, got, err, c.want)
		}
	}
	for _, ts := range []string{"321200z", "002400z", "012400z", "011260z", "12345z", "011200x", "a11200z", "246000h", "235960h"} {
		if _, err := ParseTimestamp(ts, now); err == nil {
			t.Errorf("ParseTimestamp(%q) accepted", ts)
		}
	}
}
