package radio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// Session owns a CAT transport. One goroutine serialises requests so that
// exactly one command is in flight; a second goroutine reads the port,
// splits it into lines and routes each line by its prefix: to the waiting
// request if it echoes that command, otherwise out as an event (unsolicited
// AI updates, NMEA, APRS or KISS traffic).
type Session struct {
	rw      io.ReadWriteCloser
	cfg     SessionConfig
	reqs    chan *request
	events  chan Event
	done    chan struct{}
	closeMu sync.Once
	readErr atomic.Pointer[error]

	mu      sync.Mutex
	pending *request

	// Unix nanoseconds of the last NMEA/APRS/KISS traffic, for the write hold.
	lastForeign atomic.Int64
	stats       PortStats
	statsMu     sync.Mutex
}

// SessionConfig tunes a Session. Zero values give the spec's defaults.
type SessionConfig struct {
	Timeout time.Duration // per attempt; default 500 ms
	Retries int           // extra attempts after a timeout: 0 means the default of 1, negative means none
	// CleanFor is how long the port must be free of NMEA, APRS and KISS
	// traffic before writes are allowed again; default 3 s.
	CleanFor time.Duration
	// Traffic, if set, is called for every line sent or received. It must
	// not block.
	Traffic func(TrafficEntry)
	Now     func() time.Time
}

// TrafficEntry is one line of CAT traffic, for logs and the console.
type TrafficEntry struct {
	Time time.Time `json:"t"`
	Dir  string    `json:"dir"` // "tx" or "rx"
	Text string    `json:"text"`
	Kind string    `json:"kind,omitempty"` // for rx: reply, unsolicited, nmea, aprs, kiss
}

// EventKind classifies an Event.
type EventKind string

const (
	EventUnsolicited EventKind = "unsolicited" // a CAT line nobody asked for (AI updates)
	EventNMEA        EventKind = "nmea"        // GPS sentence on the port (Menu 981)
	EventAPRS        EventKind = "aprs"        // APRS packet text on the port (Menu 982)
	EventKISS        EventKind = "kiss"        // KISS framing (0xC0) on the port (Menu 983 / KISS mode)
	EventClosed      EventKind = "closed"      // the port closed or the radio was unplugged
)

// Event is something the radio said without being asked.
type Event struct {
	Kind EventKind
	Time time.Time
	Line Line   // for EventUnsolicited
	Raw  string // the raw text, or a description
	Err  error  // for EventClosed
}

// PortStats counts foreign traffic seen on the port.
type PortStats struct {
	NMEALines   int `json:"nmea_lines"`
	APRSLines   int `json:"aprs_lines"`
	KISSFrames  int `json:"kiss_frames"`
	Unsolicited int `json:"unsolicited_lines"`
	Unparsable  int `json:"unparsable_lines"`
}

var (
	// ErrClosed means the session has ended (closed, or the radio went away).
	ErrClosed = errors.New("CAT session closed")
	// ErrTimeout means the radio did not answer.
	ErrTimeout = errors.New("no reply from radio")
	// ErrPortBusy means foreign traffic (NMEA, APRS, KISS) is on the port,
	// so writes are held back.
	ErrPortBusy = errors.New("NMEA, APRS or KISS traffic on the USB serial port; writes are held until it stops (set Menus 981–983 to Bluetooth)")
)

type request struct {
	line  Line
	ctx   context.Context
	reply chan string // the matching raw reply line
	done  chan result
}

type result struct {
	reply Line
	raw   string
	err   error
}

// NewSession starts a session on rw. Close it to stop its goroutines; it
// closes rw.
func NewSession(rw io.ReadWriteCloser, cfg SessionConfig) *Session {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 500 * time.Millisecond
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	} else if cfg.Retries == 0 {
		cfg.Retries = 1
	}
	if cfg.CleanFor <= 0 {
		cfg.CleanFor = 3 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Session{
		rw:     rw,
		cfg:    cfg,
		reqs:   make(chan *request),
		events: make(chan Event, 64),
		done:   make(chan struct{}),
	}
	go s.readLoop()
	go s.writeLoop()
	return s
}

// Events returns unsolicited traffic. If nobody drains it, events are
// dropped rather than blocking the reader.
func (s *Session) Events() <-chan Event { return s.events }

// Stats returns counts of foreign traffic seen so far.
func (s *Session) Stats() PortStats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.stats
}

// PortClean reports whether writes are currently allowed.
func (s *Session) PortClean() bool {
	last := s.lastForeign.Load()
	return last == 0 || s.cfg.Now().Sub(time.Unix(0, last)) >= s.cfg.CleanFor
}

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns why the session ended, or nil while it is running.
func (s *Session) Err() error {
	if p := s.readErr.Load(); p != nil {
		return *p
	}
	select {
	case <-s.done:
		return ErrClosed
	default:
		return nil
	}
}

// Close ends the session and closes the transport.
func (s *Session) Close() error {
	var err error
	s.closeMu.Do(func() {
		close(s.done)
		err = s.rw.Close()
	})
	return err
}

// Send checks l against the allow-list, sends it, and returns the radio's
// reply. Error replies come back as ErrUnknownCommand or ErrNotAvailable.
func (s *Session) Send(ctx context.Context, l Line) (Line, error) {
	if err := Check(l); err != nil {
		return Line{}, err
	}
	if IsWrite(l) && !s.PortClean() {
		return Line{}, ErrPortBusy
	}
	req := &request{line: l, ctx: ctx, reply: make(chan string, 1), done: make(chan result, 1)}
	select {
	case s.reqs <- req:
	case <-s.done:
		return Line{}, s.closedErr()
	case <-ctx.Done():
		return Line{}, ctx.Err()
	}
	select {
	case r := <-req.done:
		return r.reply, r.err
	case <-s.done:
		return Line{}, s.closedErr()
	}
}

// Console sends one line of console text through the same allow-list and
// returns the raw reply text.
func (s *Session) Console(ctx context.Context, text string) (string, error) {
	l, err := CheckRaw(text)
	if err != nil {
		return "", err
	}
	reply, err := s.Send(ctx, l)
	if err != nil {
		if errors.Is(err, ErrUnknownCommand) {
			return "?", err
		}
		if errors.Is(err, ErrNotAvailable) {
			return "N", err
		}
		return "", err
	}
	return reply.String(), nil
}

func (s *Session) closedErr() error {
	if p := s.readErr.Load(); p != nil {
		return fmt.Errorf("%w: %v", ErrClosed, *p)
	}
	return ErrClosed
}

func (s *Session) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case req := <-s.reqs:
			req.done <- s.do(req)
		}
	}
}

func (s *Session) do(req *request) result {
	wire := req.line.String()
	attempts := 1 + s.cfg.Retries
	for attempt := 0; attempt < attempts; attempt++ {
		s.mu.Lock()
		s.pending = req
		s.mu.Unlock()
		s.traffic(TrafficEntry{Dir: "tx", Text: wire})
		if _, err := s.rw.Write([]byte(wire + "\r")); err != nil {
			s.clearPending(req)
			return result{err: fmt.Errorf("write %q: %w", wire, err)}
		}
		timer := time.NewTimer(s.cfg.Timeout)
		select {
		case raw := <-req.reply:
			timer.Stop()
			s.clearPending(req)
			return parseReply(req.line, raw)
		case <-timer.C:
			s.clearPending(req)
		case <-req.ctx.Done():
			timer.Stop()
			s.clearPending(req)
			return result{err: req.ctx.Err()}
		case <-s.done:
			timer.Stop()
			s.clearPending(req)
			return result{err: s.closedErr()}
		}
	}
	return result{err: fmt.Errorf("%w to %q after %d attempts", ErrTimeout, wire, attempts)}
}

func (s *Session) clearPending(req *request) {
	s.mu.Lock()
	if s.pending == req {
		s.pending = nil
	}
	s.mu.Unlock()
}

// parseReply turns a matched raw reply into a result.
func parseReply(sent Line, raw string) result {
	if err, ok := IsError(raw); ok {
		return result{raw: raw, err: fmt.Errorf("%s: %w", sent, err)}
	}
	l, err := ParseLine(raw)
	if err != nil || !echoes(sent, l) {
		return result{raw: raw, err: fmt.Errorf("%w: sent %q, got %q", ErrBadReply, sent, raw)}
	}
	return result{reply: l, raw: raw}
}

// echoes reports whether reply answers sent: same command and, for commands
// whose first parameter selects a band or filter, the same selector.
func echoes(sent, reply Line) bool {
	if sent.Cmd != reply.Cmd {
		return false
	}
	switch sent.Cmd {
	case "FQ", "MD", "FO", "BY", "SH":
		return len(sent.Params) > 0 && len(reply.Params) > 0 && sent.Params[0] == reply.Params[0]
	}
	return true
}

// matches decides whether an incoming line belongs to the pending request.
func (s *Session) matches(raw string) (*request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return nil, false
	}
	if _, isErr := IsError(raw); isErr {
		return s.pending, true
	}
	l, err := ParseLine(raw)
	if err != nil || !echoes(s.pending.line, l) {
		return nil, false
	}
	return s.pending, true
}

var aprsLine = regexp.MustCompile(`^[A-Z0-9]{1,6}(-[0-9]{1,2})?>[A-Z0-9-]+[^:]*:`)

func (s *Session) readLoop() {
	buf := make([]byte, 512)
	var line bytes.Buffer
	inKISS := false
	for {
		n, err := s.rw.Read(buf)
		for _, b := range buf[:n] {
			switch {
			case b == 0xC0:
				// KISS frame delimiter. Swallow everything between delimiters.
				if inKISS {
					s.foreign(EventKISS, "KISS frame")
				}
				inKISS = !inKISS
				line.Reset()
			case inKISS:
			case b == '\r' || b == '\n':
				if line.Len() > 0 {
					s.route(line.String())
					line.Reset()
				}
			default:
				if line.Len() < 1024 {
					line.WriteByte(b)
				}
			}
		}
		if err != nil {
			select {
			case <-s.done:
				err = ErrClosed
			default:
			}
			s.readErr.Store(&err)
			s.emit(Event{Kind: EventClosed, Time: s.cfg.Now(), Err: err, Raw: err.Error()})
			s.Close()
			return
		}
	}
}

func (s *Session) route(raw string) {
	switch {
	case len(raw) > 1 && raw[0] == '$':
		s.traffic(TrafficEntry{Dir: "rx", Text: raw, Kind: "nmea"})
		s.foreign(EventNMEA, raw)
		return
	case aprsLine.MatchString(raw):
		s.traffic(TrafficEntry{Dir: "rx", Text: raw, Kind: "aprs"})
		s.foreign(EventAPRS, raw)
		return
	}
	if req, ok := s.matches(raw); ok {
		s.traffic(TrafficEntry{Dir: "rx", Text: raw, Kind: "reply"})
		select {
		case req.reply <- raw:
		default:
		}
		return
	}
	s.traffic(TrafficEntry{Dir: "rx", Text: raw, Kind: "unsolicited"})
	l, err := ParseLine(raw)
	s.statsMu.Lock()
	if err != nil {
		s.stats.Unparsable++
	} else {
		s.stats.Unsolicited++
	}
	s.statsMu.Unlock()
	s.emit(Event{Kind: EventUnsolicited, Time: s.cfg.Now(), Line: l, Raw: raw})
}

func (s *Session) foreign(kind EventKind, raw string) {
	now := s.cfg.Now()
	s.lastForeign.Store(now.UnixNano())
	s.statsMu.Lock()
	switch kind {
	case EventNMEA:
		s.stats.NMEALines++
	case EventAPRS:
		s.stats.APRSLines++
	case EventKISS:
		s.stats.KISSFrames++
		s.traffic(TrafficEntry{Dir: "rx", Text: raw, Kind: "kiss"})
	}
	s.statsMu.Unlock()
	s.emit(Event{Kind: kind, Time: now, Raw: raw})
}

func (s *Session) emit(e Event) {
	select {
	case s.events <- e:
	default: // nobody listening; drop rather than block the reader
	}
}

func (s *Session) traffic(e TrafficEntry) {
	if s.cfg.Traffic != nil {
		e.Time = s.cfg.Now()
		s.cfg.Traffic(e)
	}
}
