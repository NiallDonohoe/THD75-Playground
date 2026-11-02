// Package winlink is the Winlink side of d75winlink: the message format,
// the B2F forwarding protocol and a client session over any byte stream,
// and the Telnet dialler for the CMS. See docs/WINLINK.md.
package winlink

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrInvalid marks a message or address that cannot be sent as it is.
var ErrInvalid = errors.New("invalid message")

// DateLayout is the Date header's format, always in UTC.
const DateLayout = "2006/01/02 15:04"

// Limits on what Parse accepts and Format produces.
const (
	MaxMIDLen     = 12
	maxHeaderLine = 1000
	maxBodyLine   = 1000 - 2 // a body line, without its CRLF
)

// Message is a Winlink message: headers, a text body and attachments.
// Addresses are as ParseAddress returns them.
type Message struct {
	MID     string
	Date    time.Time
	Type    string // "Private" when empty
	From    string
	To      []string
	Cc      []string
	Subject string
	Mbo     string // the station that originated it; From when empty
	Body    string
	Files   []File
}

// File is an attachment.
type File struct {
	Name string
	Data []byte
}

// NewMID returns a random message ID: 12 upper-case letters and digits.
func NewMID() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, MaxMIDLen)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// ParseAddress turns what the owner typed into an address as messages
// carry it: a callsign or other Winlink address in upper case ("EI2ABC",
// also from "ei2abc@winlink.org"), or "SMTP:" and an internet address.
func ParseAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 5 && strings.EqualFold(s[:5], "SMTP:") {
		s = s[5:]
		if !strings.Contains(s, "@") {
			return "", fmt.Errorf("%w: %q has no @", ErrInvalid, s)
		}
	}
	if at := strings.LastIndexByte(s, '@'); at >= 0 {
		local, domain := s[:at], s[at+1:]
		if strings.EqualFold(domain, "winlink.org") {
			s = local
		} else {
			if local == "" || !strings.Contains(domain, ".") || strings.ContainsAny(s, " \t,;:<>()[]\"\\") || hasControl(s) {
				return "", fmt.Errorf("%w: %q is not an email address", ErrInvalid, s)
			}
			return "SMTP:" + s, nil
		}
	}
	s = strings.ToUpper(s)
	if s == "" || len(s) > 30 {
		return "", fmt.Errorf("%w: %q is not a Winlink address", ErrInvalid, s)
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '/' || c == '.') {
			return "", fmt.Errorf("%w: %q is not a Winlink address", ErrInvalid, s)
		}
	}
	return s, nil
}

func hasControl(s string) bool {
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

func validMID(mid string) bool {
	if mid == "" || len(mid) > MaxMIDLen {
		return false
	}
	for _, c := range mid {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// Validate checks that the message can be formatted and sent.
func (m *Message) Validate() error {
	switch {
	case !validMID(m.MID):
		return fmt.Errorf("%w: message ID %q", ErrInvalid, m.MID)
	case m.From == "":
		return fmt.Errorf("%w: no From address", ErrInvalid)
	case len(m.To) == 0:
		return fmt.Errorf("%w: no To address", ErrInvalid)
	case m.Body == "":
		return fmt.Errorf("%w: empty body", ErrInvalid)
	}
	for _, a := range append(append([]string{m.From}, m.To...), m.Cc...) {
		if got, err := ParseAddress(a); err != nil || got != a {
			return fmt.Errorf("%w: address %q", ErrInvalid, a)
		}
	}
	if hasControl(m.Subject) || hasControl(m.Type) || hasControl(m.Mbo) {
		return fmt.Errorf("%w: control characters in a header", ErrInvalid)
	}
	for _, c := range m.Body {
		if c > 0xff {
			return fmt.Errorf("%w: the body has %q, which Winlink's ISO-8859-1 cannot carry", ErrInvalid, c)
		}
	}
	for _, f := range m.Files {
		if f.Name == "" || hasControl(f.Name) || strings.ContainsAny(f.Name, "/\\") {
			return fmt.Errorf("%w: attachment name %q", ErrInvalid, f.Name)
		}
	}
	return nil
}

// Format returns the message as B2F carries it: CRLF header lines, a blank
// line, the body in ISO-8859-1, then each attachment, each section after
// the body ended by CRLF.
func (m *Message) Format() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	body := encodeBody(m.Body)

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("Mid", m.MID)
	header("Date", m.Date.UTC().Format(DateLayout))
	header("Type", cmp(m.Type, "Private"))
	header("From", m.From)
	for _, a := range m.To {
		header("To", a)
	}
	for _, a := range m.Cc {
		header("Cc", a)
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Mbo", cmp(m.Mbo, m.From))
	header("Content-Type", "text/plain; charset=ISO-8859-1")
	header("Content-Transfer-Encoding", "8bit")
	header("Body", strconv.Itoa(len(body)))
	for _, f := range m.Files {
		header("File", fmt.Sprintf("%d %s", len(f.Data), mime.QEncoding.Encode("utf-8", f.Name)))
	}
	b.WriteString("\r\n")
	b.Write(body)
	if len(m.Files) > 0 {
		b.WriteString("\r\n")
	}
	for _, f := range m.Files {
		b.Write(f.Data)
		b.WriteString("\r\n")
	}
	return b.Bytes(), nil
}

func cmp(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// encodeBody makes every line end in CRLF, splits lines over 1000 bytes
// and converts to ISO-8859-1 (Validate has checked every rune fits).
func encodeBody(s string) []byte {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	var out []byte
	for _, line := range strings.Split(s, "\n") {
		n := 0
		for _, c := range line {
			if n == maxBodyLine {
				out = append(out, '\r', '\n')
				n = 0
			}
			out = append(out, byte(c))
			n++
		}
		out = append(out, '\r', '\n')
	}
	return out
}

// Parse reads a message in B2F form. Unknown headers are ignored. The body
// is decoded from the charset its Content-Type names, ISO-8859-1 when it
// names none.
func Parse(raw []byte) (*Message, error) {
	r := bufio.NewReader(bytes.NewReader(bytes.TrimLeft(raw, " \t\r\n")))
	h, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		return nil, fmt.Errorf("%w: headers: %v", ErrInvalid, err)
	}
	dec := new(mime.WordDecoder)
	word := func(s string) string {
		if d, err := dec.DecodeHeader(s); err == nil {
			return d
		}
		return s
	}
	m := &Message{
		MID:     h.Get("Mid"),
		Type:    h.Get("Type"),
		From:    h.Get("From"),
		To:      h.Values("To"),
		Cc:      h.Values("Cc"),
		Subject: word(h.Get("Subject")),
		Mbo:     h.Get("Mbo"),
	}
	if !validMID(m.MID) {
		return nil, fmt.Errorf("%w: message ID %q", ErrInvalid, m.MID)
	}
	if d, err := time.Parse(DateLayout, h.Get("Date")); err == nil {
		m.Date = d
	}

	size, err := strconv.Atoi(h.Get("Body"))
	if err != nil || size < 0 || size > len(raw) {
		return nil, fmt.Errorf("%w: Body header %q", ErrInvalid, h.Get("Body"))
	}
	body, err := section(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: body: %v", ErrInvalid, err)
	}
	m.Body = decodeBody(body, h.Get("Content-Type"))

	for _, v := range h.Values("File") {
		sz, name, ok := strings.Cut(v, " ")
		n, err := strconv.Atoi(sz)
		if !ok || err != nil || n < 0 || n > len(raw) {
			return nil, fmt.Errorf("%w: File header %q", ErrInvalid, v)
		}
		data, err := section(r, n)
		if err != nil {
			return nil, fmt.Errorf("%w: attachment %q: %v", ErrInvalid, name, err)
		}
		m.Files = append(m.Files, File{Name: word(name), Data: data})
	}
	return m, nil
}

// section reads n bytes and the CRLF after them, which may be missing at
// the end of the message.
func section(r *bufio.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("cut short: %v", err)
	}
	end, _ := r.Peek(2)
	switch {
	case string(end) == "\r\n":
		r.Discard(2)
	case len(end) != 0:
		return nil, errors.New("not followed by CRLF")
	}
	return b, nil
}

func decodeBody(b []byte, contentType string) string {
	_, params, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(params["charset"], "utf-8") && utf8.Valid(b) {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}
