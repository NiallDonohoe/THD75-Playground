package aprs

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestParseTNC2(t *testing.T) {
	const line = "EI2ABC-9>APK005,EI2XYZ*,WIDE2-1,qAR,EI7IG:=5320.51N/00615.60W>Hello"
	p, err := ParseTNC2(line + "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Packet{
		Src:  "EI2ABC-9",
		Dst:  "APK005",
		Path: []Hop{{"EI2XYZ", true}, {"WIDE2-1", false}, {"qAR", false}, {"EI7IG", false}},
		Info: "=5320.51N/00615.60W>Hello",
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got %+v\nwant %+v", p, want)
	}
	if s := p.String(); s != line {
		t.Errorf("String() = %q, want %q", s, line)
	}
}

func TestTNC2UsedHops(t *testing.T) {
	// TNC2 marks only the last used hop; every hop before it is used too.
	p, err := ParseTNC2("A>B,D1,D2*,D3:x")
	if err != nil {
		t.Fatal(err)
	}
	if got := []bool{p.Path[0].Used, p.Path[1].Used, p.Path[2].Used}; !slices.Equal(got, []bool{true, true, false}) {
		t.Errorf("used = %v", got)
	}
	if got := p.Digipeaters(); !slices.Equal(got, []string{"D1", "D2"}) {
		t.Errorf("Digipeaters() = %q", got)
	}
	if s := p.String(); s != "A>B,D1,D2*,D3:x" {
		t.Errorf("String() = %q", s)
	}
	direct, _ := ParseTNC2("A>B,WIDE1-1,WIDE2-1:x")
	if got := direct.Digipeaters(); got != nil {
		t.Errorf("heard direct, but Digipeaters() = %q", got)
	}
}

func TestParseTNC2Malformed(t *testing.T) {
	for _, line := range []string{
		"",
		"EI2ABC>APRS no colon",
		"EI2ABC:no arrow",
		">APRS:x",
		"EI2ABC>:x",
		"EI2ABC>APRS,:x",
		"EI2ABC>APRS,WIDE1-1**:x",
		"TOOLONGCALL>APRS:x",
		"EI2 ABC>APRS:x",
		"EI2ABC>APRS:in\rfo",
		"EI2ABC>APRS:in\nfo",
	} {
		if _, err := ParseTNC2(line); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseTNC2(%q) error = %v, want ErrMalformed", line, err)
		}
	}
}

func TestQConstruct(t *testing.T) {
	cases := []struct {
		line   string
		q      QConstruct
		ok     bool
		fromRF bool
	}{
		{"A>APRS,WIDE1-1,qAR,EI7IG:x", QConstruct{"qAR", "EI7IG"}, true, true},
		{"A>APRS,TCPIP*,qAC,T2IRELAND:x", QConstruct{"qAC", "T2IRELAND"}, true, false},
		{"A>APRS,qAO,EI7IG:x", QConstruct{"qAO", "EI7IG"}, true, true},
		{"A>APRS,qAS:x", QConstruct{"qAS", ""}, true, false},
		{"A>APRS,WIDE1-1:x", QConstruct{}, false, false},
	}
	for _, c := range cases {
		p, err := ParseTNC2(c.line)
		if err != nil {
			t.Fatal(err)
		}
		q, ok := p.Q()
		if q != c.q || ok != c.ok || q.FromRF() != c.fromRF {
			t.Errorf("%q: Q() = %+v, %v (FromRF %v), want %+v, %v (FromRF %v)", c.line, q, ok, q.FromRF(), c.q, c.ok, c.fromRF)
		}
	}
}

func TestAX25Encode(t *testing.T) {
	p := Packet{
		Src:  "EI2ABC-9",
		Dst:  "APRS",
		Path: []Hop{{"WIDE1-1", true}, {"WIDE2-1", false}},
		Info: "!x",
	}
	// Each address is six characters shifted left one bit, then an SSID byte:
	// 0x60 | SSID<<1, plus the C bit (destination) or H bit (digipeater) in
	// 0x80, plus 0x01 on the last address.
	want := []byte{
		0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0xe0, // APRS, C bit
		0x8a, 0x92, 0x64, 0x82, 0x84, 0x86, 0x72, // EI2ABC-9
		0xae, 0x92, 0x88, 0x8a, 0x62, 0x40, 0xe2, // WIDE1-1, H bit
		0xae, 0x92, 0x88, 0x8a, 0x64, 0x40, 0x63, // WIDE2-1, last
		0x03, 0xf0, '!', 'x',
	}
	got, err := p.AX25()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("AX25() =\n% x\nwant\n% x", got, want)
	}
	back, err := DecodeAX25(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, p) {
		t.Errorf("DecodeAX25 = %+v, want %+v", back, p)
	}
}

func TestAX25KeepsBinaryInfo(t *testing.T) {
	p := Packet{Src: "EI2ABC-7", Dst: "US2PUQ", Info: "`|+XoZO>/\x1c\x1d\x7f\xff"}
	f, err := p.AX25()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeAX25(f)
	if err != nil {
		t.Fatal(err)
	}
	if back.Info != p.Info {
		t.Errorf("Info = %q, want %q", back.Info, p.Info)
	}
}

func TestAX25DecodeRejects(t *testing.T) {
	good, _ := Packet{Src: "EI2ABC", Dst: "APRS", Info: "x"}.AX25()
	notUI := slices.Clone(good)
	notUI[14] = 0x3f
	lower := slices.Clone(good)
	lower[7] = 'e' << 1
	gap := slices.Clone(good)
	gap[8] = ' ' << 1 // "E IABC": a character after the padding
	tooMany := []byte{}
	for range 11 {
		tooMany = append(tooMany, 0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60)
	}
	cases := map[string]struct {
		frame []byte
		err   error
	}{
		"short":          {good[:10], ErrMalformed},
		"no control":     {good[:14], ErrMalformed},
		"not UI":         {notUI, ErrNotUI},
		"lower case":     {lower, ErrMalformed},
		"gap in call":    {gap, ErrMalformed},
		"11 addresses":   {tooMany, ErrMalformed},
		"one address":    {append([]byte{0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x61}, 0x03, 0xf0), ErrMalformed},
		"empty callsign": {append([]byte{0x40, 0x40, 0x40, 0x40, 0x40, 0x40, 0x60}, good[7:]...), ErrMalformed},
	}
	for name, c := range cases {
		if _, err := DecodeAX25(c.frame); !errors.Is(err, c.err) {
			t.Errorf("%s: error = %v, want %v", name, err, c.err)
		}
	}
}

func TestAX25EncodeRejects(t *testing.T) {
	nine := make([]Hop, 9)
	for i := range nine {
		nine[i] = Hop{Call: "WIDE1-1"}
	}
	for name, p := range map[string]Packet{
		"q construct": {Src: "EI2ABC", Dst: "APRS", Path: []Hop{{Call: "qAR"}}},
		"long name":   {Src: "T2IRELAND", Dst: "APRS"},
		"SSID 16":     {Src: "EI2ABC-16", Dst: "APRS"},
		"nine hops":   {Src: "EI2ABC", Dst: "APRS", Path: nine},
	} {
		if _, err := p.AX25(); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: error = %v, want ErrMalformed", name, err)
		}
	}
}

func TestSplitCall(t *testing.T) {
	for _, c := range []struct {
		name string
		call string
		ssid int
	}{{"EI2ABC", "EI2ABC", 0}, {"EI2ABC-9", "EI2ABC", 9}, {"N0CALL-15", "N0CALL", 15}, {"A-0", "A", 0}} {
		call, ssid, err := SplitCall(c.name)
		if err != nil || call != c.call || ssid != c.ssid {
			t.Errorf("SplitCall(%q) = %q, %d, %v", c.name, call, ssid, err)
		}
	}
	for _, name := range []string{"", "-1", "EI2ABCD", "ei2abc", "EI2ABC-", "EI2ABC-16", "EI2ABC-01", "EI2ABC-+1", "EI2ABC-WX"} {
		if _, _, err := SplitCall(name); err == nil {
			t.Errorf("SplitCall(%q) accepted", name)
		}
	}
}
