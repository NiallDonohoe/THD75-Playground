package winlinkweb

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"d75scope/winlink"
	"d75scope/winlinkapp"
	"d75scope/winlinkdemo"
)

const myCall = "Q1ABC"

func newServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	mb, err := winlinkapp.OpenMailbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	demo, err := winlinkdemo.Start(myCall)
	if err != nil {
		t.Fatal(err)
	}
	demo.Rate = 0
	t.Cleanup(func() { demo.Close() })
	app := winlinkapp.New(winlinkapp.Config{
		MyCall: myCall, Password: winlinkdemo.Password, Version: "test",
		Routes: []winlinkapp.Route{{ID: "telnet", Label: "demo", Dial: demo.Dial, Target: winlink.CMSTarget}},
	}, mb)
	s := &Server{
		App:    app,
		Info:   Info{MyCall: myCall, Demo: true},
		Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>d75winlink</title>")}},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func post(t *testing.T, ts *httptest.Server, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestGuard(t *testing.T) {
	_, ts := newServer(t)
	for name, h := range map[string][2]string{
		"foreign host":   {"Host", "evil.example.com"},
		"cross site":     {"Sec-Fetch-Site", "cross-site"},
		"foreign origin": {"Origin", "http://evil.example.com"},
	} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/connect", strings.NewReader(`{"route":"telnet"}`))
		req.Header.Set("Content-Type", "application/json")
		if h[0] == "Host" {
			req.Host = h[1]
		} else {
			req.Header.Set(h[0], h[1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	// A form post (no JSON content type) is refused too.
	resp, err := http.Post(ts.URL+"/api/connect", "application/x-www-form-urlencoded", strings.NewReader("route=telnet"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post: status %d", resp.StatusCode)
	}
}

func TestListenLoopbackOnly(t *testing.T) {
	if ln, err := Listen("0.0.0.0:0"); err == nil {
		ln.Close()
		t.Error("listened on all interfaces")
	}
}

func TestComposeAndSession(t *testing.T) {
	s, ts := newServer(t)

	resp, est := post(t, ts, "/api/estimate", map[string]any{"to": "friend@example.com", "subject": "Hi", "body": "Hello there"})
	if resp.StatusCode != 200 || est["smtp"] != true || est["compressed"].(float64) <= 0 {
		t.Fatalf("estimate: %d %v", resp.StatusCode, est)
	}
	resp, bad := post(t, ts, "/api/queue", map[string]any{"to": "", "subject": "Hi", "body": "x"})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(bad["error"].(string), "To") {
		t.Errorf("queue without To: %d %v", resp.StatusCode, bad)
	}
	resp, sum := post(t, ts, "/api/queue", map[string]any{
		"to": "friend@example.com", "subject": "Hi", "body": "Hello there",
		"files": []map[string]any{{"name": "../../etc/notes.txt", "data": "aGVsbG8="}},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("queue with a path in the file name: %d %v", resp.StatusCode, sum)
	}
	resp, sum = post(t, ts, "/api/queue", map[string]any{
		"to": "friend@example.com", "subject": "Hi", "body": "Hello there",
		"files": []map[string]any{{"name": "notes.txt", "data": "aGVsbG8="}},
	})
	if resp.StatusCode != 200 || sum["folder"] != "out" {
		t.Fatalf("queue: %d %v", resp.StatusCode, sum)
	}
	mid := sum["mid"].(string)

	if resp, _ := post(t, ts, "/api/connect", map[string]string{"route": "telnet"}); resp.StatusCode != 200 {
		t.Fatalf("connect: %d", resp.StatusCode)
	}
	s.App.Wait()
	if st := s.App.Session(); st.State != "done" {
		t.Fatalf("session %+v", st)
	}

	// The sent message, with its attachment as a download.
	r, err := http.Get(ts.URL + "/api/message/" + mid)
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	json.NewDecoder(r.Body).Decode(&m)
	r.Body.Close()
	if m.Folder != "sent" || m.Body != "Hello there\r\n" || len(m.Files) != 1 || m.Files[0].Name != "notes.txt" {
		t.Errorf("message %+v", m)
	}
	r, err = http.Get(ts.URL + "/api/message/" + mid + "/file/0")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(data) != "hello" || r.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("attachment: %q %v", data, r.Header)
	}
	for _, bad := range []string{"/file/1", "/file/-1", "/file/x"} {
		r, _ := http.Get(ts.URL + "/api/message/" + mid + bad)
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}

	// Opening an inbox message marks it read.
	var unread string
	for _, sm := range s.App.Mailbox().List() {
		if sm.Folder == "in" && !sm.Read {
			unread = sm.MID
		}
	}
	r, _ = http.Get(ts.URL + "/api/message/" + unread)
	r.Body.Close()
	for _, sm := range s.App.Mailbox().List() {
		if sm.MID == unread && !sm.Read {
			t.Error("opening a message did not mark it read")
		}
	}

	if resp, _ := post(t, ts, "/api/move", map[string]string{"mid": unread, "to": "out"}); resp.StatusCode != http.StatusConflict {
		t.Errorf("move to outbox: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/api/move", map[string]string{"mid": unread, "to": "archive"}); resp.StatusCode != 200 {
		t.Errorf("archive: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/api/delete", map[string]string{"mid": unread}); resp.StatusCode != 200 {
		t.Errorf("delete from archive: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/api/delete", map[string]string{"mid": unread}); resp.StatusCode != 404 {
		t.Errorf("delete again: %d", resp.StatusCode)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"notes.txt":        "notes.txt",
		"../../etc/passwd": "passwd",
		`C:\Users\x\a.doc`: "a.doc",
		".hidden":          "hidden",
		"a\r\nb\"c":        "abc",
		"..":               "attachment",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvents(t *testing.T) {
	_, ts := newServer(t)
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<20)
	deadline := time.After(5 * time.Second)
	lines := make(chan string)
	go func() {
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var hello string
	for hello == "" {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream ended")
			}
			if strings.HasPrefix(l, "data: ") {
				hello = l[6:]
			}
		case <-deadline:
			t.Fatal("no hello")
		}
	}
	var h struct {
		Info     Info
		Snapshot winlinkapp.Snapshot
	}
	if err := json.Unmarshal([]byte(hello), &h); err != nil {
		t.Fatal(err)
	}
	if h.Info.Protocol != ProtocolVersion || h.Info.MyCall != myCall || h.Snapshot.Session.State != "idle" || len(h.Snapshot.Routes) != 1 {
		t.Errorf("hello %+v", h)
	}
}
