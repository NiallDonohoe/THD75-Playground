package aprs

import (
	"fmt"
	"strings"
)

// Kind is what an information field carries.
type Kind int

const (
	// KindUnknown is a field this package does not decode: weather without a
	// position, telemetry, queries and the like. Data.Type says which, and
	// the raw field stays in the Packet.
	KindUnknown Kind = iota
	KindPosition
	KindObject
	KindItem
	KindMessage
	KindStatus
	KindThirdParty
)

var kindNames = [...]string{"unknown", "position", "object", "item", "message", "status", "third-party"}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Data is a decoded information field. Which fields are set depends on Kind.
type Data struct {
	Kind Kind
	// Type is the data type identifier: the first byte of the field, or '!'
	// when a position starts later in it.
	Type byte
	// Timestamp is the 7-character APRS timestamp, if any; see ParseTimestamp.
	Timestamp string
	// Messaging is set when the sender says it can receive messages ('=' and
	// '@' positions).
	Messaging bool

	Position *Position // positions, objects and items
	MicE     string    // the Mic-E message, such as "En Route"
	Name     string    // object or item name
	Killed   bool      // the object or item has been deleted
	Comment  string    // what follows a position, object or item

	Message *Message
	Status  string
	Inner   *Packet // the packet carried by a third-party packet
}

// Decode decodes a packet's information field. A field of a type this
// package does not decode gives KindUnknown and no error; a field that
// breaks its type's format gives ErrMalformed.
func Decode(p Packet) (Data, error) {
	info := p.Info
	if info == "" {
		return Data{}, nil
	}
	d := Data{Type: info[0]}
	var err error
	switch info[0] {
	case '!', '=':
		if strings.HasPrefix(info, "!!") { // Peet Bros Ultimeter weather
			return d, nil
		}
		d.Messaging = info[0] == '='
		err = d.position(info[1:])
	case '/', '@':
		d.Messaging = info[0] == '@'
		var rest string
		if d.Timestamp, rest, err = timestamp(info[1:]); err == nil {
			err = d.position(rest)
		}
	case '`', '\'':
		err = d.micE(p.Dst, info)
	case ';':
		err = d.object(info[1:])
	case ')':
		err = d.item(info[1:])
	case ':':
		err = d.message(info[1:])
	case '>':
		d.status(info[1:])
	case '}':
		err = d.thirdParty(info[1:])
	default:
		// APRS101: a position may start with '!' anywhere in the first 40
		// bytes, after a TNC's own beacon text. Anything else is unknown.
		if i := strings.IndexByte(info[:min(len(info), 40)], '!'); i > 0 {
			pd := Data{Type: '!'}
			if pd.position(info[i+1:]) == nil {
				return pd, nil
			}
		}
		return d, nil
	}
	if err != nil {
		return Data{Type: d.Type}, err
	}
	return d, nil
}

// position decodes a plain or compressed position and its comment.
func (d *Data) position(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty position", ErrMalformed)
	}
	var pos Position
	var rest string
	var err error
	if isDigit(s[0]) {
		pos, rest, err = parseUncompressed(s)
	} else {
		pos, rest, err = parseCompressed(s)
	}
	if err != nil {
		return err
	}
	d.Kind = KindPosition
	d.Comment = pos.takeAltitude(rest)
	d.Position = &pos
	return nil
}

func timestamp(s string) (ts, rest string, err error) {
	if len(s) < 7 || !allDigits(s[:6]) || !strings.ContainsRune("zh/", rune(s[6])) {
		return "", "", fmt.Errorf("%w: timestamp %q", ErrMalformed, s[:min(len(s), 7)])
	}
	return s[:7], s[7:], nil
}

// object decodes ";NAME_____*DDHHMMz<position>": a nine-byte name, '*' for
// live or '_' for killed, a timestamp and a position.
func (d *Data) object(s string) error {
	if len(s) < 10 || (s[9] != '*' && s[9] != '_') {
		return fmt.Errorf("%w: object name is not nine bytes followed by '*' or '_'", ErrMalformed)
	}
	name := strings.TrimRight(s[:9], " ")
	killed := s[9] == '_'
	ts, rest, err := timestamp(s[10:])
	if err != nil {
		return err
	}
	if err := d.position(rest); err != nil {
		return err
	}
	d.Kind, d.Name, d.Killed, d.Timestamp = KindObject, name, killed, ts
	return nil
}

// item decodes ")NAME!<position>": a name of three to nine bytes, '!' for
// live or '_' for killed, and a position.
func (d *Data) item(s string) error {
	i := strings.IndexAny(s[:min(len(s), 10)], "!_")
	if i < 3 {
		return fmt.Errorf("%w: item name is not three to nine bytes followed by '!' or '_'", ErrMalformed)
	}
	if err := d.position(s[i+1:]); err != nil {
		return err
	}
	d.Kind, d.Name, d.Killed = KindItem, s[:i], s[i] == '_'
	return nil
}

// status decodes ">text" or ">DDHHMMztext".
func (d *Data) status(s string) {
	d.Kind = KindStatus
	if len(s) >= 7 && allDigits(s[:6]) && s[6] == 'z' {
		d.Timestamp, s = s[:7], s[7:]
	}
	d.Status = s
}

// thirdParty decodes "}SRC>DST,PATH:info", a packet relayed between APRS-IS
// and RF by a gateway.
func (d *Data) thirdParty(s string) error {
	inner, err := ParseTNC2(s)
	if err != nil {
		return fmt.Errorf("third-party header: %w", err)
	}
	d.Kind = KindThirdParty
	d.Inner = &inner
	return nil
}
