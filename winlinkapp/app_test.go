package winlinkapp_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"d75scope/winlink"
	"d75scope/winlinkapp"
	"d75scope/winlinkdemo"
)

const myCall = "Q1ABC"

func newApp(t *testing.T) (*winlinkapp.App, *winlinkdemo.Demo, string) {
	t.Helper()
	dir := t.TempDir()
	mb, err := winlinkapp.OpenMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	demo, err := winlinkdemo.Start(myCall)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { demo.Close() })
	demo.Rate = 0
	var log bytes.Buffer
	app := winlinkapp.New(winlinkapp.Config{
		MyCall: myCall, Password: winlinkdemo.Password, Version: "test", SessionLog: &log,
		Routes: []winlinkapp.Route{{ID: "telnet", Label: "demo", Dial: demo.Dial, Target: winlink.CMSTarget}},
	}, mb)
	return app, demo, dir
}

func session(t *testing.T, app *winlinkapp.App) winlinkapp.SessionState {
	t.Helper()
	if err := app.Connect("telnet"); err != nil {
		t.Fatal(err)
	}
	app.Wait()
	return app.Session()
}

func folder(app *winlinkapp.App, f string) []winlinkapp.Summary {
	var out []winlinkapp.Summary
	for _, s := range app.Mailbox().List() {
		if s.Folder == f {
			out = append(out, s)
		}
	}
	return out
}

func TestComposeSendReceive(t *testing.T) {
	app, demo, dir := newApp(t)
	sum, err := app.Queue(winlinkapp.Draft{
		To: "friend@example.com, q2xyz", Subject: "Hello", Body: "Testing 1 2 3",
		Files: []winlinkapp.DraftFile{{Name: "a.txt", Data: []byte("attached")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Folder != winlinkapp.Out || sum.Compressed == 0 || sum.From != myCall {
		t.Errorf("queued summary %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", sum.MID+".b2f")); err != nil {
		t.Errorf("not on disk: %v", err)
	}

	s := session(t, app)
	if s.State != "done" || s.Stats == nil || len(s.Stats.Sent) != 1 || len(s.Stats.Received) != 3 {
		t.Fatalf("first session: %+v (stats %+v)", s, s.Stats)
	}
	if got := folder(app, winlinkapp.Sent); len(got) != 1 || got[0].MID != sum.MID {
		t.Errorf("sent folder %+v", got)
	}
	if got := folder(app, winlinkapp.In); len(got) != 3 || got[0].Read {
		t.Errorf("inbox %+v", got)
	}
	if c := app.Mailbox().Counts(); c["unread"] != 3 || c[winlinkapp.Out] != 0 {
		t.Errorf("counts %v", c)
	}
	rec := demo.CMS().Received()
	if len(rec) != 1 || rec[0].MID != sum.MID {
		t.Fatalf("the CMS received %+v", rec)
	}
	m, err := winlink.Parse(rec[0].Raw)
	if err != nil || len(m.To) != 2 || m.To[0] != "SMTP:friend@example.com" || m.To[1] != "Q2XYZ" || len(m.Files) != 1 {
		t.Errorf("as received: %+v, %v", m, err)
	}

	// The demo replied: the next session collects it.
	s = session(t, app)
	if s.State != "done" || len(s.Stats.Received) != 1 {
		t.Fatalf("second session: %+v", s)
	}
	_, reply, _, err := app.Mailbox().Get(s.Stats.Received[0])
	if err != nil || reply.Subject != "Re: Hello" {
		t.Errorf("reply %+v, %v", reply, err)
	}

	// A fresh mailbox from the same directory sees the same state.
	mb2, err := winlinkapp.OpenMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := mb2.Counts(); c[winlinkapp.In] != 4 || c[winlinkapp.Sent] != 1 || c["unread"] != 4 {
		t.Errorf("reopened counts %v", c)
	}
}

func TestDemoSeedsOnlyOnce(t *testing.T) {
	app, _, dir := newApp(t)
	session(t, app)
	// A second demo run offers the same seeded IDs; the mailbox has them.
	demo2, err := winlinkdemo.Start(myCall)
	if err != nil {
		t.Fatal(err)
	}
	defer demo2.Close()
	demo2.Rate = 0
	mb, _ := winlinkapp.OpenMailbox(dir)
	app2 := winlinkapp.New(winlinkapp.Config{
		MyCall: myCall, Password: winlinkdemo.Password, Version: "test",
		Routes: []winlinkapp.Route{{ID: "telnet", Dial: demo2.Dial, Target: winlink.CMSTarget}},
	}, mb)
	s := session(t, app2)
	if s.State != "done" || len(s.Stats.Received) != 0 {
		t.Errorf("second run received %+v", s.Stats)
	}
	if n := len(folder(app2, winlinkapp.In)); n != 3 {
		t.Errorf("inbox has %d", n)
	}
}

func TestWrongPassword(t *testing.T) {
	app, demo, _ := newApp(t)
	mb := app.Mailbox()
	app = winlinkapp.New(winlinkapp.Config{
		MyCall: myCall, Password: "nope", Version: "test",
		Routes: []winlinkapp.Route{{ID: "telnet", Dial: demo.Dial, Target: winlink.CMSTarget}},
	}, mb)
	s := session(t, app)
	if s.State != "failed" || !strings.Contains(s.Detail, "refused the password") {
		t.Errorf("session %+v", s)
	}
	_, _, cancel := app.Subscribe()
	cancel()
}

func TestStop(t *testing.T) {
	app, demo, _ := newApp(t)
	demo.Rate = 200 // slow enough to stop mid-session
	if _, err := app.Queue(winlinkapp.Draft{To: "Q2XYZ", Subject: "s", Body: strings.Repeat("long message ", 2000)}); err != nil {
		t.Fatal(err)
	}
	if err := app.Connect("telnet"); err != nil {
		t.Fatal(err)
	}
	if err := app.Connect("telnet"); !errors.Is(err, winlinkapp.ErrBusy) {
		t.Errorf("second Connect: %v, want ErrBusy", err)
	}
	time.Sleep(300 * time.Millisecond)
	app.Stop()
	s := app.Session()
	if s.State != "stopped" {
		t.Errorf("state %q", s.State)
	}
	if n := len(folder(app, winlinkapp.Out)); n != 1 {
		t.Errorf("outbox has %d after a stopped session, want 1", n)
	}
}

func TestDraftChecks(t *testing.T) {
	app, _, _ := newApp(t)
	cases := map[string]winlinkapp.Draft{
		"no recipient":   {Subject: "s", Body: "b"},
		"bad address":    {To: "not an@address", Subject: "s", Body: "b"},
		"no subject":     {To: "Q2XYZ", Body: "b"},
		"empty body":     {To: "Q2XYZ", Subject: "s"},
		"emoji":          {To: "Q2XYZ", Subject: "s", Body: "73 📻"},
		"big attachment": {To: "Q2XYZ", Subject: "s", Body: "b", Files: []winlinkapp.DraftFile{{Name: "big", Data: make([]byte, winlinkapp.MaxAttachments+1)}}},
		"bad file name":  {To: "Q2XYZ", Subject: "s", Body: "b", Files: []winlinkapp.DraftFile{{Name: "../x", Data: []byte("x")}}},
	}
	for name, d := range cases {
		if _, err := app.Queue(d); !errors.Is(err, winlink.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	e, err := app.Estimate(winlinkapp.Draft{To: "a@example.com", Subject: "s", Body: strings.Repeat("abc ", 500)})
	if err != nil || !e.SMTP || e.Compressed >= e.Size || e.Secs1200 <= e.Secs9600 {
		t.Errorf("estimate %+v, %v", e, err)
	}
	if n := len(app.Mailbox().List()); n != 0 {
		t.Errorf("estimating queued something: %d messages", n)
	}
}

func TestMailboxMoves(t *testing.T) {
	app, _, _ := newApp(t)
	session(t, app)
	mb := app.Mailbox()
	in := folder(app, winlinkapp.In)[0]

	if err := mb.Delete(in.MID); !errors.Is(err, winlinkapp.ErrNotAllowed) {
		t.Errorf("deleting from the inbox: %v", err)
	}
	if err := mb.Move(in.MID, winlinkapp.Out); !errors.Is(err, winlinkapp.ErrNotAllowed) {
		t.Errorf("moving to the outbox: %v", err)
	}
	if err := mb.MarkRead(in.MID); err != nil {
		t.Fatal(err)
	}
	if err := mb.Move(in.MID, winlinkapp.Archive); err != nil {
		t.Fatal(err)
	}
	if err := mb.Move(in.MID, winlinkapp.In); err != nil {
		t.Fatal(err)
	}
	if err := mb.Move(in.MID, winlinkapp.Archive); err != nil {
		t.Fatal(err)
	}
	if err := mb.Delete(in.MID); err != nil {
		t.Fatal(err)
	}
	if mb.Have(in.MID) {
		t.Error("deleted message still in the mailbox")
	}
	if err := mb.Move("NOSUCHMID", winlinkapp.Archive); !errors.Is(err, winlinkapp.ErrNotFound) {
		t.Errorf("unknown MID: %v", err)
	}

	// Read state survives a reopen.
	other := folder(app, winlinkapp.In)[0]
	mb.MarkRead(other.MID)
	mb2, _ := winlinkapp.OpenMailbox(mb.Dir())
	for _, s := range mb2.List() {
		if s.MID == other.MID && !s.Read {
			t.Error("read state lost on reopen")
		}
	}
}

func TestEvents(t *testing.T) {
	app, _, _ := newApp(t)
	snap, ch, cancel := app.Subscribe()
	defer cancel()
	if snap.Session.State != "idle" || len(snap.Routes) != 1 {
		t.Errorf("snapshot %+v", snap)
	}
	session(t, app)
	types := map[string]int{}
	for {
		select {
		case ev := <-ch:
			types[ev.Type]++
			continue
		default:
		}
		break
	}
	for _, typ := range []string{"session", "log", "mailbox", "progress"} {
		if types[typ] == 0 {
			t.Errorf("no %s events (got %v)", typ, types)
		}
	}
}
