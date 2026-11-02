package winlink

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseAddress(t *testing.T) {
	good := map[string]string{
		"ei2abc":                "EI2ABC",
		" EI2ABC ":              "EI2ABC",
		"ei2abc@winlink.org":    "EI2ABC",
		"EI2ABC@Winlink.Org":    "EI2ABC",
		"Q1ABC-10":              "Q1ABC-10",
		"name@example.com":      "SMTP:name@example.com",
		"SMTP:name@example.com": "SMTP:name@example.com",
		"smtp:First.Last@ex.ie": "SMTP:First.Last@ex.ie",
		"EOC-ROCHESTER":         "EOC-ROCHESTER",
	}
	for in, want := range good {
		got, err := ParseAddress(in)
		if err != nil || got != want {
			t.Errorf("ParseAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "two words", "a@b", "@example.com", "x@exa mple.com", "SMTP:nobody", "bad\r\nTo: x@y.z", "a<b>@c.d", strings.Repeat("A", 31)} {
		if got, err := ParseAddress(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseAddress(%q) = %q, %v; want ErrInvalid", in, got, err)
		}
	}
}

func testMessage() *Message {
	return &Message{
		MID:     "Q1TESTMID001",
		Date:    time.Date(2026, 11, 2, 9, 41, 0, 0, time.UTC),
		From:    "Q1ABC",
		To:      []string{"Q2XYZ", "SMTP:someone@example.com"},
		Cc:      []string{"Q3CC"},
		Subject: "Test from the TH-D75",
		Body:    "Hello from Dublin.\nCafé at 10?\n\n73",
		Files:   []File{{Name: "notes.txt", Data: []byte("line one\r\nline two")}, {Name: "b.bin", Data: []byte{0, 1, 2, '\r', '\n', 255}}},
	}
}

func TestFormat(t *testing.T) {
	raw, err := testMessage().Format()
	if err != nil {
		t.Fatal(err)
	}
	want := "Mid: Q1TESTMID001\r\n" +
		"Date: 2026/11/02 09:41\r\n" +
		"Type: Private\r\n" +
		"From: Q1ABC\r\n" +
		"To: Q2XYZ\r\n" +
		"To: SMTP:someone@example.com\r\n" +
		"Cc: Q3CC\r\n" +
		"Subject: Test from the TH-D75\r\n" +
		"Mbo: Q1ABC\r\n" +
		"Content-Type: text/plain; charset=ISO-8859-1\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"Body: 39\r\n" +
		"File: 18 notes.txt\r\n" +
		"File: 6 b.bin\r\n" +
		"\r\n" +
		"Hello from Dublin.\r\nCaf\xe9 at 10?\r\n\r\n73\r\n" +
		"\r\n" +
		"line one\r\nline two\r\n" +
		"\x00\x01\x02\r\n\xff\r\n"
	if string(raw) != want {
		t.Errorf("Format:\n%q\nwant\n%q", raw, want)
	}
}

func TestParseRoundTrip(t *testing.T) {
	m := testMessage()
	m.Subject = "Grüße — über alles"
	raw, err := m.Format()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("Subject: =?utf-8?q?")) {
		t.Errorf("non-ASCII subject not word-encoded: %q", raw)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := *m
	want.Type = "Private"
	want.Mbo = "Q1ABC"
	want.Body = "Hello from Dublin.\r\nCafé at 10?\r\n\r\n73\r\n"
	if !reflect.DeepEqual(got, &want) {
		t.Errorf("Parse:\n%+v\nwant\n%+v", got, &want)
	}
}

// A message as the CMS and Winlink Express write them: headers in another
// order, no attachments, no CRLF after the body, a UTF-8 body declared.
func TestParseForeign(t *testing.T) {
	raw := "\r\nMid: ABCDEFGHIJKL\r\nBody: 7\r\nContent-Type: text/plain; charset=utf-8\r\n" +
		"Date: 2026/11/01 18:00\r\nFrom: SERVICE\r\nMbo: SMTP\r\nSubject: =?ISO-8859-1?Q?Gr=FC=DFe?=\r\n" +
		"To: Q1ABC\r\nType: Private\r\nX-Unknown: kept out\r\n\r\nÅse!\r\n"
	m, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Åse!\r\n" || m.Subject != "Grüße" || m.From != "SERVICE" || m.Date.Hour() != 18 {
		t.Errorf("Parse = %+v", m)
	}
}

func TestParseRejects(t *testing.T) {
	ok, _ := testMessage().Format()
	cases := map[string][]byte{
		"no MID":         bytes.Replace(ok, []byte("Mid: Q1TESTMID001"), []byte("Mxd: Q1TESTMID001"), 1),
		"body too long":  bytes.Replace(ok, []byte("Body: 39"), []byte("Body: 4000"), 1),
		"negative body":  bytes.Replace(ok, []byte("Body: 39"), []byte("Body: -1"), 1),
		"file cut short": ok[:len(ok)-4],
		"file no size":   bytes.Replace(ok, []byte("File: 6 b.bin"), []byte("File: b.bin"), 1),
		"no CRLF":        bytes.Replace(ok, []byte("73\r\n\r\nline"), []byte("73\r\nXXline"), 1),
	}
	for name, raw := range cases {
		if _, err := Parse(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(*Message){
		"no MID":          func(m *Message) { m.MID = "" },
		"long MID":        func(m *Message) { m.MID = "ABCDEFGHIJKLM" },
		"no To":           func(m *Message) { m.To = nil },
		"raw address":     func(m *Message) { m.To = []string{"someone@example.com"} },
		"lower-case call": func(m *Message) { m.From = "q1abc" },
		"empty body":      func(m *Message) { m.Body = "" },
		"subject newline": func(m *Message) { m.Subject = "hi\r\nTo: Q9EVIL" },
		"not Latin-1":     func(m *Message) { m.Body = "73 📻" },
		"file name slash": func(m *Message) { m.Files[0].Name = "../x" },
		"file name empty": func(m *Message) { m.Files[0].Name = "" },
	}
	for name, mod := range cases {
		m := testMessage()
		mod(m)
		if _, err := m.Format(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

func TestLongBodyLines(t *testing.T) {
	m := testMessage()
	m.Files = nil
	m.Body = strings.Repeat("x", 2500)
	raw, _ := m.Format()
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got.Body, "\r\n"), "\r\n")
	if len(lines) != 3 || len(lines[0]) != 998 || len(lines[2]) != 2500-2*998 {
		t.Errorf("2500-byte line split into %d lines", len(lines))
	}
}

func TestNewMID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		mid := NewMID()
		if len(mid) != 12 || !ValidMID(mid) || strings.ToUpper(mid) != mid || seen[mid] {
			t.Fatalf("NewMID() = %q", mid)
		}
		seen[mid] = true
	}
}

// Messages arrive from the CMS and off the air.
func FuzzParse(f *testing.F) {
	raw, _ := testMessage().Format()
	f.Add(raw)
	f.Add([]byte("Mid: A\r\nBody: 0\r\n\r\n"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		m, err := Parse(raw)
		if err != nil {
			return
		}
		if !ValidMID(m.MID) {
			t.Errorf("parsed with MID %q", m.MID)
		}
	})
}
