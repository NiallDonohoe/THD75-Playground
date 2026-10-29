package aprs

import (
	"reflect"
	"testing"
)

// Everything this package parses comes from the air or the internet, so
// none of it may panic, and what parses must format back to the same packet.

var fuzzSeeds = []string{
	"EI2ABC-9>APK005,EI2XYZ*,WIDE2-1,qAR,EI7IG:=5320.51N/00615.60W>Hello",
	"EI2ABC-9>US2PUQ,WIDE1-1:`|+XoZO>/\"4T}Hello",
	"A>APRS:=/5L!!<*e7>7P[",
	"A>APRS:;LEADER   *092345z4903.50N/07201.75W>088/036",
	"A>APRS:)AID #2!4903.50N/07201.75WA",
	"A>APRS::WU2Z     :Hi there{AB}CD",
	"A>APRS:}EI2DEF-7>APK005,TCPIP,EI7IG*:>On the air",
	"A>APRS:!49  .  N/072  .  W-",
	"A>APRS:@092345z4903.50N/07201.75W>088/036/A=001234",
}

func FuzzParseTNC2(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		p, err := ParseTNC2(line)
		if err != nil {
			return
		}
		_, _ = Decode(p)
		again, err := ParseTNC2(p.String())
		if err != nil {
			t.Fatalf("String() %q does not parse: %v", p.String(), err)
		}
		if !reflect.DeepEqual(again, p) {
			t.Fatalf("round trip changed the packet:\n%+v\n%+v", p, again)
		}
	})
}

func FuzzDecodeAX25(f *testing.F) {
	for _, s := range fuzzSeeds {
		p, _ := ParseTNC2(s)
		p.Path = nil // drop names AX.25 cannot carry
		if b, err := p.AX25(); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, frame []byte) {
		p, err := DecodeAX25(frame)
		if err != nil {
			return
		}
		_, _ = Decode(p)
		b, err := p.AX25()
		if err != nil {
			t.Fatalf("decoded packet %+v does not encode: %v", p, err)
		}
		again, err := DecodeAX25(b)
		if err != nil || !reflect.DeepEqual(again, p) {
			t.Fatalf("round trip changed the packet:\n%+v\n%+v (%v)", p, again, err)
		}
	})
}
