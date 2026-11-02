package winlink_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"d75scope/winlink"
	"d75scope/winlink/winlinktest"
)

const (
	myCall   = "Q1ABC"
	password = "S3cret-pw"
)

// memMailbox is a Mailbox in memory.
type memMailbox struct {
	mu       sync.Mutex
	out      []winlink.Outgoing
	have     map[string]bool
	received []*winlink.Message
	raw      [][]byte
	sent     map[string]bool // MID to rejected
}

func newMailbox(out ...*winlink.Message) *memMailbox {
	mb := &memMailbox{have: map[string]bool{}, sent: map[string]bool{}}
	for _, m := range out {
		raw, err := m.Format()
		if err != nil {
			panic(err)
		}
		mb.out = append(mb.out, winlink.Outgoing{MID: m.MID, Subject: m.Subject, Raw: raw})
	}
	return mb
}

func (mb *memMailbox) Outbox() []winlink.Outgoing { return mb.out }
func (mb *memMailbox) Have(mid string) bool       { return mb.have[mid] }
func (mb *memMailbox) Received(raw []byte, m *winlink.Message) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.received = append(mb.received, m)
	mb.raw = append(mb.raw, raw)
	return nil
}
func (mb *memMailbox) Sent(mid string, rejected bool) { mb.sent[mid] = rejected }

func message(i int, to string) *winlink.Message {
	return &winlink.Message{
		MID:     fmt.Sprintf("Q1MSG%07d", i),
		Date:    time.Date(2026, 11, 2, 10, i%60, 0, 0, time.UTC),
		From:    myCall,
		To:      []string{to},
		Subject: fmt.Sprintf("Test %d", i),
		Body:    strings.Repeat(fmt.Sprintf("Message %d over Winlink. ", i), 20+i),
	}
}

func inbound(i int) winlinktest.Mail {
	m := &winlink.Message{
		MID:     fmt.Sprintf("Q2IN%08d", i),
		Date:    time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC),
		From:    "SMTP:friend@example.com",
		To:      []string{myCall},
		Subject: fmt.Sprintf("Reply %d", i),
		Body:    fmt.Sprintf("Got your message %d. 73", i),
		Files:   []winlink.File{{Name: "map.txt", Data: bytes.Repeat([]byte("grid IO63 "), 100)}},
	}
	raw, err := m.Format()
	if err != nil {
		panic(err)
	}
	return winlinktest.Mail{MID: m.MID, Subject: m.Subject, Raw: raw}
}

// connect serves one connection from the fake over loopback TCP. (Not
// net.Pipe: it has no buffering, so a server writing an error while the
// client is still writing deadlocks, which TCP would absorb.)
func connect(t *testing.T, cms *winlinktest.CMS) (net.Conn, chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			served <- err
			return
		}
		served <- cms.Serve(conn)
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return client, served
}

type logLine struct {
	sent bool
	line string
}

// run connects a session to the fake over a pipe, through the Telnet
// login, and returns what the session returned and its log.
func run(t *testing.T, cms *winlinktest.CMS, s *winlink.Session) (winlink.Stats, error, []logLine) {
	t.Helper()
	if cms.Passwords == nil {
		cms.Passwords = map[string]string{myCall: password}
	}
	client, served := connect(t, cms)

	var mu sync.Mutex
	var log []logLine
	logf := func(sent bool, line string) {
		mu.Lock()
		log = append(log, logLine{sent, line})
		mu.Unlock()
	}
	if s.MyCall == "" {
		s.MyCall = myCall
	}
	if s.Password == "" {
		s.Password = password
	}
	s.Target, s.Version, s.Log = winlink.CMSTarget, "0.1.0", logf

	client.SetDeadline(time.Now().Add(10 * time.Second))
	if err := winlink.Login(client, s.MyCall, logf); err != nil {
		t.Fatalf("Login: %v", err)
	}
	stats, err := s.Run(client)
	client.Close()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Error("the fake CMS did not finish")
	}
	return stats, err, log
}

func TestExchange(t *testing.T) {
	cms := &winlinktest.CMS{}
	for i := 1; i <= 3; i++ {
		cms.Queue(myCall, inbound(i))
	}
	var out []*winlink.Message
	for i := 1; i <= 7; i++ { // more than one turn's worth
		out = append(out, message(i, "SMTP:friend@example.com"))
	}
	mb := newMailbox(out...)
	stats, err, log := run(t, cms, &winlink.Session{Mailbox: mb, Locator: "IO63"})
	if err != nil {
		t.Fatal(err)
	}

	got := cms.Received()
	if len(got) != 7 {
		t.Fatalf("the CMS received %d messages, want 7", len(got))
	}
	for i, m := range got {
		if !bytes.Equal(m.Raw, mb.out[i].Raw) || m.From != myCall {
			t.Errorf("message %d arrived different", i+1)
		}
		if rejected, ok := mb.sent[m.MID]; !ok || rejected {
			t.Errorf("%s not recorded as sent", m.MID)
		}
	}
	if len(stats.Sent) != 7 || len(stats.Received) != 3 {
		t.Errorf("stats = %+v", stats)
	}
	if len(mb.received) != 3 {
		t.Fatalf("received %d messages, want 3", len(mb.received))
	}
	for i, m := range mb.received {
		want := inbound(i + 1)
		if m == nil || m.MID != want.MID || !bytes.Equal(mb.raw[i], want.Raw) || len(m.Files) != 1 {
			t.Errorf("received message %d differs: %+v", i+1, m)
		}
	}
	if w := cms.Waiting(myCall); len(w) != 0 {
		t.Errorf("%d messages still waiting at the CMS", len(w))
	}

	var sentLines []string
	for _, l := range log {
		if strings.Contains(l.line, password) {
			t.Errorf("the password appears in the log: %q", l.line)
		}
		if l.sent {
			sentLines = append(sentLines, l.line)
		}
	}
	wantStart := []string{myCall, "CMSTelnet", ";FW: Q1ABC", "[d75winlink-0.1.0-B2FHM$]", ";PR: ********", "; wl2k DE Q1ABC (IO63)"}
	if !reflect.DeepEqual(sentLines[:len(wantStart)], wantStart) {
		t.Errorf("first lines sent:\n%q\nwant\n%q", sentLines[:len(wantStart)], wantStart)
	}
	if last := sentLines[len(sentLines)-1]; last != "FQ" {
		t.Errorf("last line sent %q, want FQ", last)
	}
}

func TestNothingToDo(t *testing.T) {
	stats, err, log := run(t, &winlinktest.CMS{}, &winlink.Session{Mailbox: newMailbox()})
	if err != nil || len(stats.Sent)+len(stats.Received) != 0 {
		t.Fatalf("stats %+v, err %v", stats, err)
	}
	var tail []string
	for _, l := range log[len(log)-2:] {
		tail = append(tail, fmt.Sprintf("%v %s", l.sent, l.line))
	}
	if want := []string{"true FF", "false FQ"}; !reflect.DeepEqual(tail, want) {
		t.Errorf("session ended %q, want %q", tail, want)
	}
}

func TestLoginFailed(t *testing.T) {
	mb := newMailbox(message(1, "Q2XYZ"))
	_, err, _ := run(t, &winlinktest.CMS{}, &winlink.Session{Mailbox: mb, Password: "wrong"})
	var re *winlink.RemoteError
	if !errors.As(err, &re) || !re.LoginFailed() {
		t.Fatalf("got %v, want a failed secure login", err)
	}
	if len(mb.sent) != 0 {
		t.Error("messages recorded as sent after a failed login")
	}
}

func TestNoPassword(t *testing.T) {
	client, server := net.Pipe()
	go (&winlinktest.CMS{}).Serve(server)
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if err := winlink.Login(client, myCall, nil); err != nil {
		t.Fatal(err)
	}
	_, err := (&winlink.Session{MyCall: myCall, Mailbox: newMailbox()}).Run(client)
	if !errors.Is(err, winlink.ErrNoPassword) {
		t.Fatalf("got %v, want ErrNoPassword", err)
	}
}

func TestRejectAndDefer(t *testing.T) {
	m1, m2, m3 := message(1, "Q2XYZ"), message(2, "Q2XYZ"), message(3, "Q2XYZ")
	cms := &winlinktest.CMS{Have: map[string]bool{m1.MID: true}, Defer: map[string]bool{m2.MID: true}}
	mb := newMailbox(m1, m2, m3)
	stats, err, _ := run(t, cms, &winlink.Session{Mailbox: mb})
	if err != nil {
		t.Fatal(err)
	}
	if rejected, ok := mb.sent[m1.MID]; !ok || !rejected {
		t.Error("the message the CMS already had is not recorded as rejected")
	}
	if _, ok := mb.sent[m2.MID]; ok {
		t.Error("the deferred message is recorded as sent")
	}
	if rejected, ok := mb.sent[m3.MID]; !ok || rejected {
		t.Error("the accepted message is not recorded as sent")
	}
	if !reflect.DeepEqual(stats.Sent, []string{m3.MID}) || !reflect.DeepEqual(stats.Rejected, []string{m1.MID}) {
		t.Errorf("stats = %+v", stats)
	}
}

func TestWeAlreadyHaveIt(t *testing.T) {
	cms := &winlinktest.CMS{}
	in := inbound(1)
	cms.Queue(myCall, in)
	mb := newMailbox()
	mb.have[in.MID] = true
	stats, err, _ := run(t, cms, &winlink.Session{Mailbox: mb})
	if err != nil || len(mb.received) != 0 || len(stats.Received) != 0 {
		t.Fatalf("received %d, stats %+v, err %v", len(mb.received), stats, err)
	}
	if len(cms.Waiting(myCall)) != 0 {
		t.Error("the CMS still holds a message we said we had")
	}
}

func TestDroppedConnection(t *testing.T) {
	m := message(1, "Q2XYZ")
	mb := newMailbox(m)
	_, err, _ := run(t, &winlinktest.CMS{DropAfterBlocks: true}, &winlink.Session{Mailbox: mb})
	if err == nil {
		t.Fatal("no error when the connection dropped")
	}
	if _, ok := mb.sent[m.MID]; ok {
		t.Error("a message is recorded as sent though the CMS never confirmed it")
	}
}

func TestGarbage(t *testing.T) {
	_, err, _ := run(t, &winlinktest.CMS{Garbage: true}, &winlink.Session{Mailbox: newMailbox(message(1, "Q2XYZ"))})
	if !errors.Is(err, winlink.ErrProtocol) {
		t.Fatalf("got %v, want ErrProtocol", err)
	}
}

func TestMaxSend(t *testing.T) {
	small, big := message(1, "Q2XYZ"), message(2, "Q2XYZ")
	rng := rand.New(rand.NewPCG(5, 6))
	digits := make([]byte, 20000) // random digits barely compress
	for i := range digits {
		digits[i] = byte('0' + rng.IntN(10))
	}
	big.Body = string(digits)
	mb := newMailbox(big, small)
	stats, err, _ := run(t, &winlinktest.CMS{}, &winlink.Session{Mailbox: mb, MaxSend: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stats.Sent, []string{small.MID}) {
		t.Errorf("sent %v, want only the small message", stats.Sent)
	}
}

func TestMaxReceive(t *testing.T) {
	cms := &winlinktest.CMS{}
	in := inbound(1)
	cms.Queue(myCall, in)
	stats, err, _ := run(t, cms, &winlink.Session{Mailbox: newMailbox(), MaxReceive: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Received) != 0 || !reflect.DeepEqual(stats.Deferred, []string{in.MID}) {
		t.Errorf("stats = %+v", stats)
	}
	if len(cms.Waiting(myCall)) != 1 {
		t.Error("the deferred message is no longer waiting at the CMS")
	}
}

// The whole path over TCP, as d75winlink will run it against the real CMS.
func TestDialCMS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cms := &winlinktest.CMS{Passwords: map[string]string{myCall: password}}
	cms.Queue(myCall, inbound(1))
	go cms.ListenAndServe(ln)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := winlink.DialCMS(ctx, ln.Addr().String(), myCall, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	mb := newMailbox(message(1, "SMTP:friend@example.com"))
	stats, err := (&winlink.Session{MyCall: myCall, Password: password, Target: winlink.CMSTarget, Version: "test", Mailbox: mb}).Run(conn)
	if err != nil || len(stats.Sent) != 1 || len(stats.Received) != 1 {
		t.Fatalf("stats %+v, err %v", stats, err)
	}
}
