// Package aprs parses and formats APRS packets: the TNC2 text form used on
// APRS-IS, AX.25 UI frames as a KISS TNC carries them, and the information
// field formats the TH-D75 and its neighbours send (plain, compressed and
// Mic-E positions, objects, items, messages and status).
//
// It is pure Go with no I/O. Where a packet came from (RF or APRS-IS) is not
// part of a Packet; the station store records that.
//
// Formats follow the APRS Protocol Reference 1.0.1 (APRS101) and its
// reply-ack addendum. See docs/decisions/0006-own-aprs-parser.md.
package aprs

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMalformed marks a packet or information field that does not follow the
// APRS format.
var ErrMalformed = errors.New("malformed APRS packet")

// Packet is one APRS packet: source, destination, digipeater path and
// information field. The information field is kept as raw bytes in a
// string, because Mic-E and some telemetry use bytes outside printable ASCII.
type Packet struct {
	Src  string
	Dst  string
	Path []Hop
	Info string
}

// Hop is one entry of the path. Used is set when the hop has already
// repeated the packet: the H bit in AX.25. TNC2 text marks only the last
// used hop with "*"; every hop before it is used too.
type Hop struct {
	Call string
	Used bool
}

// maxName is the longest station or path name accepted in text form.
// APRS-IS allows nine characters, as many as AX.25's "CALLSG-15".
const maxName = 9

// ParseTNC2 parses the text form "SRC>DST,HOP,HOP*:info" used by APRS-IS and
// most TNCs. A trailing CR or LF is ignored.
func ParseTNC2(line string) (Packet, error) {
	line = strings.TrimRight(line, "\r\n")
	header, info, ok := strings.Cut(line, ":")
	if !ok {
		return Packet{}, fmt.Errorf("%w: no ':' after the header", ErrMalformed)
	}
	if strings.ContainsAny(info, "\r\n") {
		return Packet{}, fmt.Errorf("%w: line break inside the information field", ErrMalformed)
	}
	src, rest, ok := strings.Cut(header, ">")
	if !ok {
		return Packet{}, fmt.Errorf("%w: no '>' after the source", ErrMalformed)
	}
	fields := strings.Split(rest, ",")
	p := Packet{Src: src, Dst: fields[0], Info: info}
	if err := checkName(src); err != nil {
		return Packet{}, fmt.Errorf("%w: source %v", ErrMalformed, err)
	}
	if err := checkName(p.Dst); err != nil {
		return Packet{}, fmt.Errorf("%w: destination %v", ErrMalformed, err)
	}
	last := -1
	for i, f := range fields[1:] {
		call, used := strings.CutSuffix(f, "*")
		if err := checkName(call); err != nil {
			return Packet{}, fmt.Errorf("%w: path %v", ErrMalformed, err)
		}
		if used {
			last = i
		}
		p.Path = append(p.Path, Hop{Call: call})
	}
	for i := 0; i <= last; i++ {
		p.Path[i].Used = true
	}
	return p, nil
}

// String formats the packet as TNC2 text, without a line terminator.
func (p Packet) String() string {
	var b strings.Builder
	b.WriteString(p.Src)
	b.WriteByte('>')
	b.WriteString(p.Dst)
	last := p.lastUsed()
	for i, h := range p.Path {
		b.WriteByte(',')
		b.WriteString(h.Call)
		if i == last {
			b.WriteByte('*')
		}
	}
	b.WriteByte(':')
	b.WriteString(p.Info)
	return b.String()
}

func (p Packet) lastUsed() int {
	last := -1
	for i, h := range p.Path {
		if h.Used {
			last = i
		}
	}
	return last
}

// Digipeaters returns the used hops in order. On RF, an empty result means
// the packet was heard direct.
func (p Packet) Digipeaters() []string {
	var calls []string
	for i := 0; i <= p.lastUsed(); i++ {
		calls = append(calls, p.Path[i].Call)
	}
	return calls
}

// QConstruct is the APRS-IS q construct in a path, such as "qAR,EI7IG": how
// the packet entered APRS-IS and through which station or server.
type QConstruct struct {
	Code string // "qAR", "qAC", ...
	Via  string // the station or server named after the code; may be empty
}

// Q returns the packet's q construct, if its path has one.
func (p Packet) Q() (QConstruct, bool) {
	for i, h := range p.Path {
		if len(h.Call) == 3 && strings.HasPrefix(h.Call, "qA") {
			q := QConstruct{Code: h.Call}
			if i+1 < len(p.Path) {
				q.Via = p.Path[i+1].Call
			}
			return q, true
		}
	}
	return QConstruct{}, false
}

// FromRF reports whether the code says an IGate heard the packet on RF
// (qAR, qAr, qAo, qAO), as opposed to a client sending it over the internet.
func (q QConstruct) FromRF() bool {
	switch q.Code {
	case "qAR", "qAr", "qAo", "qAO":
		return true
	}
	return false
}

// checkName accepts a station or path name in text form: one to nine
// letters, digits or hyphens.
func checkName(s string) error {
	if s == "" {
		return errors.New("is empty")
	}
	if len(s) > maxName {
		return fmt.Errorf("%q is longer than %d characters", s, maxName)
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isAlnum(c) && c != '-' {
			return fmt.Errorf("%q contains %q", s, c)
		}
	}
	return nil
}

func isAlnum(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || isDigit(c)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}
