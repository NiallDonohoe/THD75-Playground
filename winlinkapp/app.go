package winlinkapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"d75scope/lzhuf"
	"d75scope/winlink"
)

// Limits on what the page can compose (docs/WINLINK.md).
const (
	MaxAttachments = 120 << 10 // all of a message's attachments together
	MaxMessage     = 150 << 10 // the whole message, as sent
)

// ErrBusy is returned when a session is asked for while one is running.
var ErrBusy = errors.New("a session is already running")

// Route is one way to reach Winlink.
type Route struct {
	ID    string `json:"id"`    // "telnet"; "rf" from W4
	Label string `json:"label"` // for the page
	// Dial opens the connection, logged in and ready for the session.
	Dial func(ctx context.Context, log func(sent bool, line string)) (net.Conn, error) `json:"-"`
	// Target is the session's target name: "wl2k" for the CMS.
	Target string `json:"-"`
	// MaxSend caps the compressed bytes a session sends; 0 means none.
	MaxSend int `json:"maxSend,omitempty"`
}

// Config is what the app needs to run sessions.
type Config struct {
	MyCall   string
	Password string
	Locator  string
	Version  string
	Routes   []Route
	// Idle is how long a session may wait for the other side before it
	// gives up; 0 means 2 minutes.
	Idle time.Duration
	// SessionLog, if set, gets every session's log, a line each.
	SessionLog io.Writer
}

// Event is one change, already encoded, for the page's event stream.
type Event struct {
	Type string // "mailbox", "session", "log" or "progress"
	Data json.RawMessage
}

// LogLine is one line of a session's log.
type LogLine struct {
	T    time.Time `json:"t"`
	Dir  string    `json:"dir"` // "tx", "rx" or "info"
	Text string    `json:"text"`
}

// SessionState is the current or last session.
type SessionState struct {
	State    string            `json:"state"` // idle, connecting, exchanging, done, failed, stopped
	Route    string            `json:"route,omitempty"`
	Started  time.Time         `json:"started,omitzero"`
	Ended    time.Time         `json:"ended,omitzero"`
	Detail   string            `json:"detail,omitempty"`
	Stats    *winlink.Stats    `json:"stats,omitempty"`
	Progress *winlink.Progress `json:"progress,omitempty"`
}

// Snapshot is everything the page needs to draw itself from scratch.
type Snapshot struct {
	Mailbox []Summary      `json:"mailbox"`
	Counts  map[string]int `json:"counts"`
	Session SessionState   `json:"session"`
	Log     []LogLine      `json:"log"`
	Routes  []Route        `json:"routes"`
}

// keepLog is how many session log lines a snapshot carries.
const keepLog = 400

// subQueue is how many events a subscriber may fall behind before it is
// dropped; the page reconnects and starts again from a snapshot.
const subQueue = 512

// App ties the mailbox to sessions and to the page.
type App struct {
	cfg Config
	mb  *Mailbox

	mu      sync.Mutex
	subs    map[chan Event]struct{}
	session SessionState
	log     []LogLine
	cancel  context.CancelFunc
	done    chan struct{}
	now     func() time.Time
}

// New returns an app over an open mailbox.
func New(cfg Config, mb *Mailbox) *App {
	if cfg.Idle == 0 {
		cfg.Idle = 2 * time.Minute
	}
	a := &App{cfg: cfg, mb: mb, subs: map[chan Event]struct{}{}, session: SessionState{State: "idle"}, now: time.Now}
	mb.setOnChange(a.mailboxChanged)
	return a
}

// Mailbox is the app's mailbox.
func (a *App) Mailbox() *Mailbox { return a.mb }

func (a *App) publishLocked(typ string, v any) {
	if len(a.subs) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := Event{Type: typ, Data: b}
	for ch := range a.subs {
		select {
		case ch <- ev:
		default:
			delete(a.subs, ch)
			close(ch)
		}
	}
}

func (a *App) mailboxChanged() {
	list, counts := a.mb.List(), a.mb.Counts()
	a.mu.Lock()
	a.publishLocked("mailbox", struct {
		Mailbox []Summary      `json:"mailbox"`
		Counts  map[string]int `json:"counts"`
	}{list, counts})
	a.mu.Unlock()
}

// Subscribe returns a snapshot and a channel of the events after it. The
// channel is closed if the subscriber falls too far behind; cancel ends
// the subscription.
func (a *App) Subscribe() (Snapshot, <-chan Event, func()) {
	list, counts := a.mb.List(), a.mb.Counts()
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := Snapshot{
		Mailbox: list,
		Counts:  counts,
		Session: a.session,
		Log:     append([]LogLine{}, a.log...),
		Routes:  append([]Route{}, a.cfg.Routes...),
	}
	ch := make(chan Event, subQueue)
	a.subs[ch] = struct{}{}
	cancel := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if _, ok := a.subs[ch]; ok {
			delete(a.subs, ch)
			close(ch)
		}
	}
	return snap, ch, cancel
}

// Session returns the current or last session's state.
func (a *App) Session() SessionState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session
}

// ---- Compose -------------------------------------------------------------

// Draft is a message as the page composes it.
type Draft struct {
	To      string      `json:"to"` // addresses separated by commas, semicolons or spaces
	Cc      string      `json:"cc"`
	Subject string      `json:"subject"`
	Body    string      `json:"body"`
	Files   []DraftFile `json:"files"`
}

// DraftFile is an attachment from the page; JSON carries Data as base64.
type DraftFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Estimate is what a draft would cost to send.
type Estimate struct {
	Size       int     `json:"size"`
	Compressed int     `json:"compressed"`
	Secs1200   float64 `json:"secs1200"`
	Secs9600   float64 `json:"secs9600"`
	SMTP       bool    `json:"smtp"` // goes to an internet address
}

// Airtime is a rough transfer time, in seconds, for compressed bytes over
// AX.25 packet at bps: 1200 bps carries about 70 bytes a second once
// headers, acks and turnarounds are counted, 9600 about 400. The handshake
// adds a few seconds more.
func Airtime(compressed, bps int) float64 {
	rate := 70.0
	if bps >= 9600 {
		rate = 400
	}
	return float64(compressed) / rate
}

func splitAddresses(s string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' }) {
		a, err := winlink.ParseAddress(f)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// message builds a message from a draft, checking it against the limits.
func (a *App) message(d Draft) (*winlink.Message, []byte, error) {
	to, err := splitAddresses(d.To)
	if err != nil {
		return nil, nil, fmt.Errorf("To: %w", err)
	}
	cc, err := splitAddresses(d.Cc)
	if err != nil {
		return nil, nil, fmt.Errorf("Cc: %w", err)
	}
	total := 0
	m := &winlink.Message{
		MID:     winlink.NewMID(),
		Date:    a.now().UTC(),
		From:    a.cfg.MyCall,
		To:      to,
		Cc:      cc,
		Subject: strings.TrimSpace(d.Subject),
		Body:    d.Body,
	}
	for _, f := range d.Files {
		total += len(f.Data)
		m.Files = append(m.Files, winlink.File{Name: f.Name, Data: f.Data})
	}
	if total > MaxAttachments {
		return nil, nil, fmt.Errorf("%w: attachments come to %d kB, over the %d kB limit", winlink.ErrInvalid, (total+1023)/1024, MaxAttachments>>10)
	}
	if m.Subject == "" {
		return nil, nil, fmt.Errorf("%w: no subject", winlink.ErrInvalid)
	}
	raw, err := m.Format()
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > MaxMessage {
		return nil, nil, fmt.Errorf("%w: the message is %d kB, over the %d kB limit", winlink.ErrInvalid, (len(raw)+1023)/1024, MaxMessage>>10)
	}
	return m, raw, nil
}

// Estimate sizes a draft without queueing it.
func (a *App) Estimate(d Draft) (Estimate, error) {
	m, raw, err := a.message(d)
	if err != nil {
		return Estimate{}, err
	}
	c := len(lzhuf.CompressB2(raw))
	e := Estimate{Size: len(raw), Compressed: c, Secs1200: Airtime(c, 1200), Secs9600: Airtime(c, 9600)}
	for _, addr := range append(m.To, m.Cc...) {
		if strings.HasPrefix(addr, "SMTP:") {
			e.SMTP = true
		}
	}
	return e, nil
}

// Queue puts a draft in the outbox.
func (a *App) Queue(d Draft) (Summary, error) {
	m, _, err := a.message(d)
	if err != nil {
		return Summary{}, err
	}
	return a.mb.Queue(m)
}

// ---- Sessions ------------------------------------------------------------

func (a *App) route(id string) (Route, bool) {
	for _, r := range a.cfg.Routes {
		if r.ID == id {
			return r, true
		}
	}
	return Route{}, false
}

// Connect starts a session on a route, in the background.
func (a *App) Connect(routeID string) error {
	r, ok := a.route(routeID)
	if !ok {
		return fmt.Errorf("no route %q", routeID)
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.done = make(chan struct{})
	a.log = nil
	a.session = SessionState{State: "connecting", Route: r.ID, Started: a.now()}
	a.publishLocked("session", a.session)
	done := a.done
	a.mu.Unlock()

	go func() {
		defer close(done)
		a.run(ctx, r)
	}()
	return nil
}

// Stop ends the running session, if there is one, and waits for it.
func (a *App) Stop() {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	if cancel != nil {
		a.session.Detail = "stopping…"
	}
	a.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// Wait waits for the running session, if any, to end.
func (a *App) Wait() {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *App) addLog(dir, text string) {
	l := LogLine{T: a.now(), Dir: dir, Text: text}
	a.mu.Lock()
	a.log = append(a.log, l)
	if len(a.log) > keepLog {
		a.log = a.log[len(a.log)-keepLog:]
	}
	a.publishLocked("log", l)
	w := a.cfg.SessionLog
	a.mu.Unlock()
	if w != nil {
		mark := map[string]string{"tx": ">", "rx": "<", "info": "-"}[dir]
		fmt.Fprintf(w, "%s %s %s\n", l.T.UTC().Format(time.RFC3339), mark, text)
	}
}

func (a *App) setState(state, detail string) {
	a.mu.Lock()
	a.session.State = state
	a.session.Detail = detail
	a.publishLocked("session", a.session)
	a.mu.Unlock()
}

func (a *App) run(ctx context.Context, r Route) {
	logf := func(sent bool, line string) {
		if strings.HasPrefix(line, "(") {
			a.addLog("info", strings.Trim(line, "()"))
		} else if sent {
			a.addLog("tx", line)
		} else {
			a.addLog("rx", line)
		}
	}
	a.addLog("info", "connecting: "+r.Label)
	conn, err := r.Dial(ctx, logf)
	if err == nil {
		a.setState("exchanging", "")
		s := &winlink.Session{
			MyCall:   a.cfg.MyCall,
			Password: a.cfg.Password,
			Target:   r.Target,
			Locator:  a.cfg.Locator,
			Version:  a.cfg.Version,
			Mailbox:  a.mb,
			MaxSend:  r.MaxSend,
			Log:      logf,
			Progress: func(p winlink.Progress) {
				a.mu.Lock()
				a.session.Progress = &p
				a.publishLocked("progress", p)
				a.mu.Unlock()
			},
		}
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		var stats winlink.Stats
		stats, err = s.Run(&idleConn{Conn: conn, idle: a.cfg.Idle})
		stop()
		conn.Close()
		a.mu.Lock()
		a.session.Stats = &stats
		a.mu.Unlock()
	}

	a.mu.Lock()
	a.cancel, a.done = nil, nil
	a.session.Ended = a.now()
	a.session.Progress = nil
	stats := a.session.Stats
	switch {
	case ctx.Err() != nil:
		a.session.State, a.session.Detail = "stopped", "stopped by you"
	case err != nil:
		a.session.State, a.session.Detail = "failed", describe(err)
	default:
		a.session.State, a.session.Detail = "done", summary(stats)
	}
	state := a.session
	a.publishLocked("session", state)
	a.mu.Unlock()
	a.addLog("info", state.State+": "+state.Detail)
}

func describe(err error) string {
	var re *winlink.RemoteError
	switch {
	case errors.As(err, &re) && re.LoginFailed():
		return "the server refused the password (" + re.Text + ")"
	case errors.Is(err, winlink.ErrNoPassword):
		return "the server asked for your Winlink password, and none is set"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return "the connection dropped"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "no answer from the server"
	}
	return err.Error()
}

func summary(s *winlink.Stats) string {
	if s == nil {
		return ""
	}
	parts := []string{
		plural(len(s.Sent), "message sent", "messages sent"),
		plural(len(s.Received), "received", "received"),
	}
	if n := len(s.Rejected); n > 0 {
		parts = append(parts, plural(n, "already at the server", "already at the server"))
	}
	if n := len(s.Deferred); n > 0 {
		parts = append(parts, plural(n, "left on the server", "left on the server"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// idleConn gives up when the other side has said nothing, and taken
// nothing, for idle.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(c.idle))
	return c.Conn.Write(b)
}
