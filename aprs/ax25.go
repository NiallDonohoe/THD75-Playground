package aprs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AX.25 UI frames as a KISS TNC carries them: the address field, control and
// PID bytes, then the information field. KISS strips the flags and the FCS.

// ErrNotUI marks an AX.25 frame that is not a UI frame with no layer 3
// protocol, the only kind APRS uses.
var ErrNotUI = errors.New("not an AX.25 UI frame with PID 0xF0")

const (
	ax25UI      = 0x03 // control: unnumbered information
	ax25NoL3    = 0xf0 // PID: no layer 3 protocol
	ax25AddrLen = 7
	maxDigis    = 8
)

// DecodeAX25 parses an AX.25 UI frame (without flags or FCS) into a Packet.
// A digipeater's H bit becomes Hop.Used.
func DecodeAX25(frame []byte) (Packet, error) {
	var names []string
	var hbits []bool
	i := 0
	for {
		if len(names) == 2+maxDigis {
			return Packet{}, fmt.Errorf("%w: AX.25 frame has more than %d digipeaters", ErrMalformed, maxDigis)
		}
		if len(frame)-i < ax25AddrLen {
			return Packet{}, fmt.Errorf("%w: AX.25 address field is cut short", ErrMalformed)
		}
		name, hbit, err := decodeAddress(frame[i : i+ax25AddrLen])
		if err != nil {
			return Packet{}, fmt.Errorf("%w: AX.25 address %d: %v", ErrMalformed, len(names)+1, err)
		}
		names = append(names, name)
		hbits = append(hbits, hbit)
		last := frame[i+ax25AddrLen-1]&1 == 1
		i += ax25AddrLen
		if last {
			break
		}
	}
	if len(names) < 2 {
		return Packet{}, fmt.Errorf("%w: AX.25 frame has no source address", ErrMalformed)
	}
	if len(frame)-i < 2 {
		return Packet{}, fmt.Errorf("%w: AX.25 frame ends before the control and PID bytes", ErrMalformed)
	}
	if frame[i] != ax25UI || frame[i+1] != ax25NoL3 {
		return Packet{}, fmt.Errorf("%w (control 0x%02x, PID 0x%02x)", ErrNotUI, frame[i], frame[i+1])
	}
	p := Packet{Dst: names[0], Src: names[1], Info: string(frame[i+2:])}
	last := -1
	for k := 2; k < len(names); k++ {
		p.Path = append(p.Path, Hop{Call: names[k]})
		if hbits[k] {
			last = k - 2
		}
	}
	for k := 0; k <= last; k++ {
		p.Path[k].Used = true
	}
	return p, nil
}

func decodeAddress(a []byte) (name string, hbit bool, err error) {
	var call []byte
	padded := false
	for k := 0; k < 6; k++ {
		if a[k]&1 != 0 {
			return "", false, fmt.Errorf("byte %d has its low bit set", k)
		}
		c := a[k] >> 1
		switch {
		case c == ' ':
			padded = true
		case padded:
			return "", false, errors.New("character after the padding")
		case c >= 'A' && c <= 'Z' || isDigit(c):
			call = append(call, c)
		default:
			return "", false, fmt.Errorf("invalid character %q", c)
		}
	}
	if len(call) == 0 {
		return "", false, errors.New("empty callsign")
	}
	name = string(call)
	if ssid := int(a[6]>>1) & 0x0f; ssid != 0 {
		name += "-" + strconv.Itoa(ssid)
	}
	return name, a[6]&0x80 != 0, nil
}

// AX25 encodes the packet as an AX.25 UI command frame, without flags or
// FCS. Every name must be an AX.25 address (see SplitCall), and the path can
// have at most eight hops; APRS-IS names such as "qAR" or "T2IRELAND" cannot
// be encoded.
func (p Packet) AX25() ([]byte, error) {
	if len(p.Path) > maxDigis {
		return nil, fmt.Errorf("%w: %d digipeaters, AX.25 allows %d", ErrMalformed, len(p.Path), maxDigis)
	}
	frame := make([]byte, 0, ax25AddrLen*(2+len(p.Path))+2+len(p.Info))
	type addr struct {
		name string
		top  bool // the C bit on the destination, the H bit on a digipeater
	}
	addrs := []addr{{p.Dst, true}, {p.Src, false}}
	last := p.lastUsed()
	for i, h := range p.Path {
		addrs = append(addrs, addr{h.Call, i <= last})
	}
	for i, a := range addrs {
		call, ssid, err := SplitCall(a.name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		var b [ax25AddrLen]byte
		for k := 0; k < 6; k++ {
			c := byte(' ')
			if k < len(call) {
				c = call[k]
			}
			b[k] = c << 1
		}
		b[6] = 0x60 | byte(ssid)<<1
		if a.top {
			b[6] |= 0x80
		}
		if i == len(addrs)-1 {
			b[6] |= 0x01
		}
		frame = append(frame, b[:]...)
	}
	frame = append(frame, ax25UI, ax25NoL3)
	return append(frame, p.Info...), nil
}

// SplitCall splits an AX.25 address such as "EI2ABC-9" into its callsign and
// SSID: one to six upper-case letters or digits, then optionally a hyphen
// and an SSID from 0 to 15.
func SplitCall(name string) (call string, ssid int, err error) {
	call, s, hasSSID := strings.Cut(name, "-")
	if len(call) < 1 || len(call) > 6 {
		return "", 0, fmt.Errorf("%q: the callsign must be 1 to 6 characters", name)
	}
	for i := 0; i < len(call); i++ {
		if c := call[i]; !(c >= 'A' && c <= 'Z' || isDigit(c)) {
			return "", 0, fmt.Errorf("%q: the callsign may only contain A-Z and 0-9", name)
		}
	}
	if hasSSID {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 15 || s != strconv.Itoa(n) {
			return "", 0, fmt.Errorf("%q: the SSID must be 0 to 15", name)
		}
		ssid = n
	}
	return call, ssid, nil
}
