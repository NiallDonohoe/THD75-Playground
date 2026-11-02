package winlink

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"d75scope/lzhuf"
)

// Mailbox is what a session needs from the mailbox.
type Mailbox interface {
	// Outbox returns the messages waiting to be sent, oldest first.
	Outbox() []Outgoing
	// Have reports whether a message ID is already in the mailbox, so a
	// proposal of it is rejected rather than downloaded again.
	Have(mid string) bool
	// Received stores a message that arrived: raw is its B2F form, and m
	// is nil when it could not be parsed (it is kept all the same). An
	// error ends the session before the peer is told it arrived.
	Received(raw []byte, m *Message) error
	// Sent records that the peer took a message, or rejected it because
	// it already had it.
	Sent(mid string, rejected bool)
}

// Outgoing is a message ready to offer.
type Outgoing struct {
	MID     string
	Subject string
	Raw     []byte // as Message.Format returns it
}

// Session runs the client side of one B2F exchange.
type Session struct {
	MyCall   string // the Winlink account's callsign
	Password string // answers the secure login challenge; never logged or sent
	Target   string // "wl2k" for the CMS, or the gateway's callsign
	Locator  string // optional Maidenhead locator
	Version  string // our version, for the SID
	Mailbox  Mailbox

	// MaxSend caps the compressed bytes offered in this session; messages
	// that would go over it stay in the outbox. 0 means no cap.
	MaxSend int
	// MaxReceive is the largest message accepted, uncompressed; larger
	// ones are deferred, to stay on the server. 0 means DefaultMaxReceive.
	MaxReceive int

	// Log, if set, gets every protocol line, sent or received, and a line
	// for each message's blocks. The secure login answer is masked.
	Log func(sent bool, line string)
}

// DefaultMaxReceive is Session.MaxReceive's default.
const DefaultMaxReceive = 1 << 20

// Stats says what a session moved.
type Stats struct {
	Sent     []string // message IDs the peer took
	Rejected []string // offered, but the peer already had them
	Received []string
	Deferred []string // offered to us, left on the server
}

// ErrNoPassword is returned when the server asks for a secure login and
// the session has no password.
var ErrNoPassword = errors.New("the server asked for the Winlink password, and none is set")

type session struct {
	*Session
	w     io.Writer
	r     *bufio.Reader
	stats Stats
}

// Run exchanges messages over rw until one side quits. rw is a connection
// that has just been opened (after the Telnet login, for the CMS). Closing
// it from another goroutine stops Run.
func (s *Session) Run(rw io.ReadWriter) (Stats, error) {
	x := &session{Session: s, w: rw, r: bufio.NewReader(rw)}
	err := x.run()
	return x.stats, err
}

func (x *session) log(sent bool, line string) {
	if x.Log != nil {
		x.Log(sent, line)
	}
}

func (x *session) send(line string) error {
	shown := line
	if strings.HasPrefix(line, ";PR:") {
		shown = ";PR: ********"
	}
	x.log(true, shown)
	_, err := io.WriteString(x.w, line+"\r")
	return err
}

func (x *session) recv() (string, error) {
	line, err := readLine(x.r)
	if err != nil {
		return "", err
	}
	if line != "" {
		x.log(false, line)
	}
	if strings.HasPrefix(line, "***") {
		return "", &RemoteError{Text: strings.TrimLeft(line, "* ")}
	}
	return line, nil
}

func (x *session) run() error {
	if err := x.handshake(); err != nil {
		return err
	}
	queue := x.outgoing()
	remoteDone := false
	for {
		// Our turn.
		batch := queue[:min(len(queue), maxProposals)]
		queue = queue[len(batch):]
		var awaiting []string
		if len(batch) == 0 {
			if remoteDone {
				return x.send("FQ")
			}
			if err := x.send("FF"); err != nil {
				return err
			}
		} else {
			var err error
			if awaiting, err = x.propose(batch); err != nil {
				return err
			}
		}

		// Their turn. The first line back confirms the messages we sent.
		quit, done, err := x.inbound(func() {
			for _, mid := range awaiting {
				x.Mailbox.Sent(mid, false)
				x.stats.Sent = append(x.stats.Sent, mid)
			}
			awaiting = nil
		})
		if err != nil || quit {
			return err
		}
		remoteDone = done
	}
}

func (x *session) handshake() error {
	var flags, challenge string
	for {
		line, err := x.recv()
		if err != nil {
			return err
		}
		if f, ok := sidFlags(line); ok {
			flags = f
		} else if c, ok := strings.CutPrefix(line, ";PQ:"); ok {
			challenge = strings.TrimSpace(c)
		} else if strings.HasSuffix(line, ">") {
			break
		}
	}
	if !strings.Contains(flags, "B2F") {
		return fmt.Errorf("%w: the server does not offer B2F (SID flags %q)", ErrProtocol, flags)
	}
	if challenge != "" && x.Password == "" {
		return ErrNoPassword
	}
	lines := []string{
		";FW: " + x.MyCall,
		formatSID("d75winlink", x.Version),
	}
	if challenge != "" {
		lines = append(lines, ";PR: "+SecureLoginResponse(challenge, x.Password))
	}
	de := fmt.Sprintf("; %s DE %s", x.Target, x.MyCall)
	if x.Locator != "" {
		de += " (" + x.Locator + ")"
	}
	lines = append(lines, de)
	for _, l := range lines {
		if err := x.send(l); err != nil {
			return err
		}
	}
	return nil
}

type offer struct {
	Outgoing
	prop       Proposal
	compressed []byte
}

// outgoing compresses the outbox and keeps what fits under MaxSend.
func (x *session) outgoing() []offer {
	var out []offer
	total := 0
	for _, o := range x.Mailbox.Outbox() {
		c := lzhuf.CompressB2(o.Raw)
		if x.MaxSend > 0 && total+len(c) > x.MaxSend {
			x.log(true, fmt.Sprintf("(%s held back: %d bytes would pass this session's limit of %d)", o.MID, len(c), x.MaxSend))
			continue
		}
		total += len(c)
		out = append(out, offer{Outgoing: o, prop: Proposal{MID: o.MID, Size: len(o.Raw), CompressedSize: len(c)}, compressed: c})
	}
	return out
}

// propose offers a batch, reads the answers, and sends what was accepted.
// It returns the IDs sent, to be confirmed by the peer's next line.
func (x *session) propose(batch []offer) ([]string, error) {
	var sum byte
	for _, o := range batch {
		l := o.prop.line()
		sum = proposalChecksum(sum, l)
		if err := x.send(l); err != nil {
			return nil, err
		}
	}
	if err := x.send(fmt.Sprintf("F> %02X", -sum)); err != nil {
		return nil, err
	}
	var line string
	for {
		var err error
		if line, err = x.recv(); err != nil {
			return nil, err
		}
		if line != "" && line[0] != ';' {
			break
		}
	}
	answers, offsets, err := parseAnswers(line, len(batch))
	if err != nil {
		return nil, err
	}
	var sent []string
	for i, o := range batch {
		switch answers[i] {
		case Reject:
			x.Mailbox.Sent(o.MID, true)
			x.stats.Rejected = append(x.stats.Rejected, o.MID)
		case Accept:
			if offsets[i] >= len(o.compressed) {
				return nil, fmt.Errorf("%w: offset %d for %s, which is %d bytes", ErrProtocol, offsets[i], o.MID, len(o.compressed))
			}
			x.log(true, fmt.Sprintf("(sending %s: %d bytes from offset %d)", o.MID, len(o.compressed), offsets[i]))
			if err := writeBlocks(x.w, o.Subject, offsets[i], o.compressed); err != nil {
				return nil, err
			}
			sent = append(sent, o.MID)
		}
	}
	return sent, nil
}

// inbound handles the peer's turn. confirm runs on the first line that is
// not an error. It reports whether the peer quit, and whether it said it
// has nothing more (FF).
func (x *session) inbound(confirm func()) (quit, done bool, err error) {
	var props []Proposal
	var codes []byte
	var sum byte
	for {
		line, err := x.recv()
		if err != nil {
			return false, false, err
		}
		confirm()
		if line == "" || line[0] == ';' {
			continue
		}
		switch {
		case line == "FF":
			return false, true, nil
		case line == "FQ":
			return true, false, nil
		case strings.HasPrefix(line, "F>"):
			var theirs byte
			if _, err := fmt.Sscanf(strings.TrimSpace(line[2:]), "%X", &theirs); err != nil || theirs != -sum {
				return false, false, fmt.Errorf("%w: proposal checksum %q, expected %02X", ErrProtocol, line, -sum)
			}
			if len(props) == 0 {
				return false, true, nil
			}
			return false, false, x.receive(props, codes)
		case len(line) > 2 && line[0] == 'F' && strings.ContainsRune("ABCD", rune(line[1])):
			p, code, err := parseProposal(line)
			if err != nil {
				return false, false, err
			}
			if len(props) == maxProposals*2 {
				return false, false, fmt.Errorf("%w: too many proposals in one turn", ErrProtocol)
			}
			sum = proposalChecksum(sum, line)
			props = append(props, p)
			codes = append(codes, code)
		default:
			return false, false, fmt.Errorf("%w: unexpected line %q", ErrProtocol, line)
		}
	}
}

// receive answers a turn's proposals and reads the accepted messages.
func (x *session) receive(props []Proposal, codes []byte) error {
	limit := x.MaxReceive
	if limit <= 0 {
		limit = DefaultMaxReceive
	}
	answers := make([]byte, len(props))
	seen := map[string]bool{}
	for i, p := range props {
		switch {
		case codes[i] != 'C', seen[p.MID], p.Size > limit, p.CompressedSize > limit:
			answers[i] = Defer
			x.stats.Deferred = append(x.stats.Deferred, p.MID)
		case x.Mailbox.Have(p.MID):
			answers[i] = Reject
		default:
			answers[i] = Accept
		}
		seen[p.MID] = true
	}
	if err := x.send("FS " + string(answers)); err != nil {
		return err
	}
	for i, p := range props {
		if answers[i] != Accept {
			continue
		}
		_, data, err := readBlocks(x.r, p.CompressedSize)
		if err != nil {
			return err
		}
		x.log(false, fmt.Sprintf("(received %s: %d bytes)", p.MID, len(data)))
		raw, err := lzhuf.DecompressB2(data, p.Size)
		if err != nil {
			return fmt.Errorf("%s: %w", p.MID, err)
		}
		if len(raw) != p.Size {
			return fmt.Errorf("%w: %s is %d bytes, proposed as %d", ErrProtocol, p.MID, len(raw), p.Size)
		}
		m, err := Parse(raw)
		if err != nil {
			x.log(false, fmt.Sprintf("(%s does not parse: %v; kept as received)", p.MID, err))
			m = nil
		}
		if err := x.Mailbox.Received(raw, m); err != nil {
			return fmt.Errorf("storing %s: %w", p.MID, err)
		}
		x.stats.Received = append(x.stats.Received, p.MID)
	}
	return nil
}
