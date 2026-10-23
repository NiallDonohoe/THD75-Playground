package radio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestCAT(t *testing.T, fake *FakeD75, cfg SessionConfig) *CAT {
	t.Helper()
	if cfg.Timeout == 0 {
		cfg.Timeout = 300 * time.Millisecond
	}
	c := NewCAT(fake.Pipe(), cfg)
	t.Cleanup(func() { c.Close() })
	return c
}

// assertOnlyAllowed fails if the radio ever received a line the allow-list
// would refuse.
func assertOnlyAllowed(t *testing.T, fake *FakeD75) {
	t.Helper()
	for _, raw := range fake.Received() {
		if _, err := CheckRaw(raw); err != nil {
			t.Errorf("radio received a refused line %q: %v", raw, err)
		}
	}
}

func TestCATReadsEveryCommand(t *testing.T) {
	fake := NewFakeD75()
	c := newTestCAT(t, fake, SessionConfig{})
	ctx := context.Background()

	id, err := c.Identify(ctx)
	if err != nil || id.Model != "TH-D75" || id.Firmware != "1.03" {
		t.Fatalf("Identify = %+v, %v", id, err)
	}
	if hz, err := c.Frequency(ctx); err != nil || hz != 7_074_000 {
		t.Errorf("Frequency = %d, %v", hz, err)
	}
	if m, err := c.Mode(ctx); err != nil || m != ModeUSB {
		t.Errorf("Mode = %v, %v", m, err)
	}
	if o, err := c.IFOutput(ctx); err != nil || o != IOAF {
		t.Errorf("IFOutput = %v, %v", o, err)
	}
	if on, err := c.FineTune(ctx); err != nil || on {
		t.Errorf("FineTune = %v, %v", on, err)
	}
	if s, err := c.FineStep(ctx); err != nil || s.Hz() != 100 {
		t.Errorf("FineStep = %v, %v", s, err)
	}
	if on, err := c.AutoInfo(ctx); err != nil || on {
		t.Errorf("AutoInfo = %v, %v", on, err)
	}
	for m, want := range map[FilterMode]int{FilterSSB: 2, FilterCW: 1, FilterAM: 1} {
		if got, err := c.Filter(ctx, m); err != nil || got != want {
			t.Errorf("Filter(%v) = %d, %v", m, got, err)
		}
	}
	if open, err := c.Squelch(ctx); err != nil || open {
		t.Errorf("Squelch = %v, %v", open, err)
	}
	if v, err := c.VFO(ctx); err != nil || v.FrequencyHz != 7_074_000 || v.Mode != ModeUSB {
		t.Errorf("VFO = %+v, %v", v, err)
	}
	assertOnlyAllowed(t, fake)
}

func TestCATWritesReadBack(t *testing.T) {
	fake := NewFakeD75()
	fake.SnapHz = 20
	c := newTestCAT(t, fake, SessionConfig{})
	ctx := context.Background()

	got, err := c.SetFrequency(ctx, 7_074_013)
	if err != nil || got != 7_074_020 {
		t.Errorf("SetFrequency = %d, %v; want the radio's rounded 7074020", got, err)
	}
	if m, err := c.SetMode(ctx, ModeCW); err != nil || m != ModeCW {
		t.Errorf("SetMode = %v, %v", m, err)
	}
	if o, err := c.SetIFOutput(ctx, IOIF); err != nil || o != IOIF {
		t.Errorf("SetIFOutput = %v, %v", o, err)
	}
	if on, err := c.SetFineTune(ctx, true); err != nil || !on {
		t.Errorf("SetFineTune = %v, %v", on, err)
	}
	if s, err := c.SetFineStep(ctx, 0); err != nil || s != 0 {
		t.Errorf("SetFineStep = %v, %v", s, err)
	}
	if on, err := c.SetAutoInfo(ctx, true); err != nil || !on {
		t.Errorf("SetAutoInfo = %v, %v", on, err)
	}
	if _, err := c.SetMode(ctx, ModeDV); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("SetMode(DV) err = %v", err)
	}
	if _, err := c.SetFrequency(ctx, 600_000_000); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("SetFrequency(600 MHz) err = %v", err)
	}
	assertOnlyAllowed(t, fake)
}

func TestErrorReplies(t *testing.T) {
	fake := NewFakeD75()
	fake.NotAvailable = map[string]bool{"FT": true}
	c := newTestCAT(t, fake, SessionConfig{})
	ctx := context.Background()
	if _, err := c.FineTune(ctx); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("N reply: err = %v", err)
	}
	reply, err := c.Session().Console(ctx, "FT 1")
	if reply != "N" || !errors.Is(err, ErrNotAvailable) {
		t.Errorf("console N: %q, %v", reply, err)
	}
}

func TestUnknownCommandReply(t *testing.T) {
	// A transport where every command gets "?".
	fake := &scriptRadio{reply: func(string) string { return "?" }}
	s := NewSession(fake.start(), SessionConfig{Timeout: 200 * time.Millisecond})
	defer s.Close()
	if _, err := s.Send(context.Background(), Line{Cmd: "ID"}); !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("err = %v", err)
	}
}

func TestReplyThatDoesNotEchoIsAnError(t *testing.T) {
	// The radio answers FV to an ID request: routed as unsolicited, so ID
	// times out (after one retry) and the stray line is reported.
	fake := &scriptRadio{reply: func(string) string { return "FV 1.00" }}
	s := NewSession(fake.start(), SessionConfig{Timeout: 100 * time.Millisecond})
	defer s.Close()
	start := time.Now()
	_, err := s.Send(context.Background(), Line{Cmd: "ID"})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want timeout", err)
	}
	if el := time.Since(start); el < 200*time.Millisecond {
		t.Errorf("gave up after %v; want two 100 ms attempts", el)
	}
	if n := fake.count(); n != 2 {
		t.Errorf("sent %d times, want 2 (one retry)", n)
	}
	if st := s.Stats(); st.Unsolicited != 2 {
		t.Errorf("unsolicited = %d, want 2", st.Unsolicited)
	}
}

func TestRetryAfterTimeout(t *testing.T) {
	// The first ID is lost; the retry is answered.
	var n int
	fake := &scriptRadio{reply: func(string) string {
		n++
		if n == 1 {
			return ""
		}
		return "ID TH-D75"
	}}
	s := NewSession(fake.start(), SessionConfig{Timeout: 100 * time.Millisecond})
	defer s.Close()
	reply, err := s.Send(context.Background(), Line{Cmd: "ID"})
	if err != nil || reply.String() != "ID TH-D75" {
		t.Errorf("reply %q, %v", reply, err)
	}
}

func TestUnsolicitedAIRouting(t *testing.T) {
	fake := NewFakeD75()
	fake.Latency = 50 * time.Millisecond
	c := newTestCAT(t, fake, SessionConfig{})
	ctx := context.Background()
	if _, err := c.SetAutoInfo(ctx, true); err != nil {
		t.Fatal(err)
	}
	// A knob turn arrives while an MD request is in flight.
	done := make(chan error, 1)
	go func() {
		_, err := c.Mode(ctx)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	fake.TurnKnob(7_075_000)
	if err := <-done; err != nil {
		t.Fatalf("Mode during AI line: %v", err)
	}
	select {
	case ev := <-c.Events():
		if ev.Kind != EventUnsolicited || ev.Line.String() != "FQ 1,0007075000" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no unsolicited event")
	}
	// An AI line for FQ arriving while an FQ read is pending is consistent
	// with the read, so taking it as the reply is harmless.
	if hz, err := c.Frequency(ctx); err != nil || hz != 7_075_000 {
		t.Errorf("Frequency = %d, %v", hz, err)
	}
}

func TestForeignTrafficHoldsWrites(t *testing.T) {
	for name, inject := range map[string][]byte{
		"nmea": []byte("$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A\r\n"),
		"aprs": []byte("N0CALL-7>APK005,WIDE1-1:!5320.00N/00615.00W>\r"),
		"kiss": {0xC0, 0x00, 'N', '0', 'C', 0x0D, 'X', 0xC0},
	} {
		t.Run(name, func(t *testing.T) {
			fake := NewFakeD75()
			now := time.Now()
			var mu sync.Mutex
			clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
			c := newTestCAT(t, fake, SessionConfig{Now: clock, CleanFor: 3 * time.Second})
			ctx := context.Background()

			fake.Inject(inject)
			ev := waitEvent(t, c.Events())
			if string(ev.Kind) != name {
				t.Fatalf("event kind %q, want %q", ev.Kind, name)
			}
			if _, err := c.SetIFOutput(ctx, IOIF); !errors.Is(err, ErrPortBusy) {
				t.Errorf("write during %s traffic: err = %v", name, err)
			}
			// Reads still work: the stray traffic did not corrupt replies.
			if hz, err := c.Frequency(ctx); err != nil || hz != 7_074_000 {
				t.Errorf("read during %s traffic: %d, %v", name, hz, err)
			}
			for _, raw := range fake.Received() {
				if raw == "IO 1" {
					t.Errorf("IO 1 reached the radio while the port was dirty")
				}
			}
			mu.Lock()
			now = now.Add(3 * time.Second)
			mu.Unlock()
			if _, err := c.SetIFOutput(ctx, IOIF); err != nil {
				t.Errorf("write after the port went quiet: %v", err)
			}
		})
	}
}

func TestUnplugMidSession(t *testing.T) {
	fake := NewFakeD75()
	c := newTestCAT(t, fake, SessionConfig{})
	ctx := context.Background()
	if _, err := c.Identify(ctx); err != nil {
		t.Fatal(err)
	}
	fake.Unplug()
	ev := waitEvent(t, c.Events())
	if ev.Kind != EventClosed {
		t.Fatalf("event = %+v", ev)
	}
	if _, err := c.Frequency(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("after unplug: err = %v", err)
	}
	select {
	case <-c.Session().Done():
	case <-time.After(time.Second):
		t.Error("session did not end")
	}
}

func TestSilentRadioTimesOut(t *testing.T) {
	fake := NewFakeD75()
	fake.Silent = true // KISS mode: CAT does not answer
	c := newTestCAT(t, fake, SessionConfig{Timeout: 50 * time.Millisecond})
	if _, err := c.Identify(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v", err)
	}
}

func TestTrafficLog(t *testing.T) {
	fake := NewFakeD75()
	var mu sync.Mutex
	var log []TrafficEntry
	c := newTestCAT(t, fake, SessionConfig{Traffic: func(e TrafficEntry) {
		mu.Lock()
		log = append(log, e)
		mu.Unlock()
	}})
	c.Frequency(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(log) != 2 || log[0].Dir != "tx" || log[0].Text != "FQ 1" || log[1].Dir != "rx" || log[1].Text != "FQ 1,0007074000" {
		t.Errorf("log = %+v", log)
	}
}

func waitEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

// scriptRadio answers each received line with reply(line); "" means no answer.
type scriptRadio struct {
	reply func(string) string
	mu    sync.Mutex
	n     int
}

func (r *scriptRadio) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *scriptRadio) start() *pipeEnd {
	client, radio := newPipePair()
	go func() {
		buf := make([]byte, 256)
		var line []byte
		for {
			n, err := radio.Read(buf)
			if err != nil {
				return
			}
			for _, b := range buf[:n] {
				if b != '\r' {
					line = append(line, b)
					continue
				}
				r.mu.Lock()
				r.n++
				r.mu.Unlock()
				if out := r.reply(string(line)); out != "" {
					radio.Write([]byte(out + "\r"))
				}
				line = line[:0]
			}
		}
	}()
	return client
}
