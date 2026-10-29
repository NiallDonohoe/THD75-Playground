package aprsweb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"d75scope/aprs"
	"d75scope/aprsapp"
	"d75scope/aprsfi"
)

func newServer(t *testing.T) (*Server, *httptest.Server) {
	s := &Server{
		Store:  aprsapp.NewStore("EI2ABC-7"),
		Info:   Info{MyCall: "EI2ABC-7", Lat: 53.35, Lon: -6.26, Demo: true},
		Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>d75aprs</title>")}, "app.js": {Data: []byte("// app")}},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestListenLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.10:0", "example.com:0"} {
		if ln, err := Listen(addr); err == nil {
			ln.Close()
			t.Errorf("Listen(%q) accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "localhost:0"} {
		ln, err := Listen(addr)
		if err != nil {
			t.Errorf("Listen(%q): %v", addr, err)
			continue
		}
		ln.Close()
	}
}

func TestGuard(t *testing.T) {
	_, ts := newServer(t)
	do := func(method, path string, header map[string]string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, nil)
		for k, v := range header {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	host := strings.TrimPrefix(ts.URL, "http://")
	cases := []struct {
		name   string
		header map[string]string
		code   int
	}{
		{"plain", nil, 200},
		{"same origin", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://" + host}, 200},
		{"typed URL", map[string]string{"Sec-Fetch-Site": "none"}, 200},
		{"localhost name", map[string]string{"Host": "localhost:8076"}, 200},
		{"rebinding", map[string]string{"Host": "evil.example:8076"}, 403},
		{"cross site", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"same site", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"foreign origin", map[string]string{"Origin": "https://evil.example"}, 403},
	}
	for _, c := range cases {
		if resp := do("GET", "/", c.header); resp.StatusCode != c.code {
			t.Errorf("%s: %d, want %d", c.name, resp.StatusCode, c.code)
		}
	}
	resp := do("GET", "/", nil)
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP %q", csp)
	}
}

func TestEvents(t *testing.T) {
	s, ts := newServer(t)
	s.Heartbeat = 50 * time.Millisecond
	p, _ := aprs.ParseTNC2("Q1HOME>APK005:=5321.00N/00616.00W-Home")
	s.Store.Ingest(aprsapp.Arrival{Source: aprsapp.SourceRF, Packet: p, At: time.Now()})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<20)
	next := func() (string, string) {
		var typ string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				typ = line[7:]
			case strings.HasPrefix(line, "data: "):
				return typ, line[6:]
			case line == ": keepalive":
				return "keepalive", ""
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return "", ""
	}

	typ, data := next()
	var hello struct {
		Info     Info             `json:"info"`
		Snapshot aprsapp.Snapshot `json:"snapshot"`
	}
	if typ != "hello" || json.Unmarshal([]byte(data), &hello) != nil {
		t.Fatalf("first event %s %s", typ, data)
	}
	if hello.Info.Protocol != ProtocolVersion || hello.Info.MyCall != "EI2ABC-7" || len(hello.Snapshot.Stations) != 1 || len(hello.Snapshot.Packets) != 1 {
		t.Errorf("hello %+v", hello)
	}

	p, _ = aprs.ParseTNC2("Q8FAR>APRS,TCPIP*,qAC,T2DEMO:>On the air")
	s.Store.Ingest(aprsapp.Arrival{Source: aprsapp.SourceIS, Packet: p, At: time.Now()})
	seen := map[string]bool{}
	for !seen["packet"] || !seen["keepalive"] {
		typ, data := next()
		seen[typ] = true
		if typ == "packet" {
			var rec aprsapp.PacketRecord
			if json.Unmarshal([]byte(data), &rec) != nil || rec.Src != "Q8FAR" || rec.IS == nil || rec.IS.Q != "qAC" {
				t.Errorf("packet event %s", data)
			}
		}
	}
}

func TestLookup(t *testing.T) {
	s, ts := newServer(t)
	post := func(body, ctype string) (int, map[string]any) {
		resp, err := http.Post(ts.URL+"/api/lookup", ctype, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if code, out := post(`{"call":"Q9REMOTE"}`, "application/json"); code != 503 || out["error"] == nil {
		t.Errorf("lookups off: %d %v", code, out)
	}

	var asked []string
	s.Lookup = func(ctx context.Context, names ...string) ([]aprsfi.Entry, error) {
		asked = names
		if names[0] == "SLOW" {
			return nil, aprsfi.ErrBackoff
		}
		if names[0] == "BROKEN" {
			return nil, errors.New("aprs.fi: HTTP 500")
		}
		return []aprsfi.Entry{{Name: "Q9REMOTE", Type: "l", Lat: 54.7, Lng: -4.9, LastTime: time.Now().Add(-time.Hour)}}, nil
	}
	code, out := post(`{"call":"Q9REMOTE"}`, "application/json")
	if code != 200 || len(asked) != 1 || asked[0] != "Q9REMOTE" {
		t.Fatalf("lookup: %d %v", code, out)
	}
	if keys, _ := out["keys"].([]any); len(keys) != 1 || keys[0] != "Q9REMOTE" {
		t.Errorf("keys %v", out["keys"])
	}
	if st, ok := s.Store.Station("Q9REMOTE"); !ok || st.Pos == nil {
		t.Errorf("station not stored: %+v", st)
	}
	if code, _ := post(`{"call":"SLOW"}`, "application/json"); code != 429 {
		t.Errorf("backoff: %d", code)
	}
	if code, out := post(`{"call":"BROKEN"}`, "application/json"); code != 502 || out["error"] != "aprs.fi: HTTP 500" {
		t.Errorf("failure: %d %v", code, out)
	}
	for _, body := range []string{`{"call":""}`, `{"call":"TOOLONGCALL"}`, `{"call":"A,B"}`, `not json`} {
		if code, _ := post(body, "application/json"); code != 400 {
			t.Errorf("%s: %d", body, code)
		}
	}
	// A form post (which a foreign page could send without CORS) is refused.
	if code, _ := post(`call=Q9REMOTE`, "application/x-www-form-urlencoded"); code != 415 {
		t.Errorf("form post: %d", code)
	}
}

func TestAssets(t *testing.T) {
	_, ts := newServer(t)
	for path, code := range map[string]int{"/": 200, "/app.js": 200, "/geo.test.mjs": 404, "/tiles/1/0/0": 404} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != code {
			t.Errorf("%s: %d, want %d", path, resp.StatusCode, code)
		}
	}
}
