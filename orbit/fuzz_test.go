package orbit

import "testing"

// Element sets come from the network: parsing must fail cleanly, and
// anything accepted must initialise or be refused without panicking.
func FuzzParseTLEs(f *testing.F) {
	f.Add(issName + "\n" + issL1 + "\n" + issL2 + "\n")
	f.Add(valladoCases[0].line1 + "\n" + valladoCases[0].line2)
	f.Fuzz(func(t *testing.T, text string) {
		els, _ := ParseTLEs(text)
		for _, e := range els {
			if s, err := New(e); err == nil {
				s.Propagate(e.Epoch)
			}
		}
	})
}

func FuzzParseOMMJSON(f *testing.F) {
	f.Add(`[{"OBJECT_NAME":"ISS","NORAD_CAT_ID":25544,"EPOCH":"2008-09-20T12:25:40.104192","MEAN_MOTION":15.72,"ECCENTRICITY":0.0007,"INCLINATION":51.6,"BSTAR":-1e-5}]`)
	f.Fuzz(func(t *testing.T, js string) {
		els, _ := ParseOMMJSON([]byte(js))
		for _, e := range els {
			if s, err := New(e); err == nil {
				s.Propagate(e.Epoch)
			}
		}
	})
}
