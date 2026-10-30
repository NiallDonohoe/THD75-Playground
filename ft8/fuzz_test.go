package ft8

import (
	"strings"
	"testing"
)

// Payloads come off the air: any 77 bits must unpack or fail cleanly.
func FuzzUnpack(f *testing.F) {
	for _, msg := range []string{"CQ K1ABC FN42", "<W9XYZ> PJ4/K1ABC RRR", "TNX BOB 73 GL"} {
		p, _ := Pack(msg)
		f.Add(bytesOf(&p))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var p Payload
		for i := range p {
			if i/8 < len(b) {
				p[i] = b[i/8] >> (7 - i%8) & 1
			}
		}
		unpack(&p, NewCallsigns())
	})
}

// Whatever Pack accepts must come back as the same words.
func FuzzPack(f *testing.F) {
	for _, s := range []string{"CQ DX K1ABC FN42", "K1ABC W9XYZ R-09", "HELLO", "CQ PJ4/K1ABC"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := Pack(s)
		if err != nil {
			return
		}
		want := strings.Join(strings.Fields(strings.ToUpper(s)), " ")
		calls := NewCallsigns()
		for _, w := range strings.Fields(want) {
			if strings.HasPrefix(w, "<") && strings.HasSuffix(w, ">") {
				calls.Add(w[1 : len(w)-1])
			}
		}
		got, err := unpack(&p, calls)
		if err != nil || got != want {
			t.Fatalf("%q packed but came back as %q (%v)", want, got, err)
		}
	})
}

func bytesOf(p *Payload) []byte {
	b := make([]byte, 10)
	for i, v := range p {
		b[i/8] |= v << (7 - i%8)
	}
	return b
}
