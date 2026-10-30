package satweb

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"d75scope/orbit"
	"d75scope/satapp"
	"d75scope/satdemo"
	"d75scope/web"
)

func server(t *testing.T) (*httptest.Server, *satapp.App) {
	t.Helper()
	start := time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)
	app := satapp.New(satapp.NewFastClock(start, 1), satapp.Config{}, "")
	app.SetCatalog(satdemo.Catalog(orbit.Observer{Lat: 53.35, Lon: -6.26}, start), nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	s := &Server{App: app, Assets: web.Sat(), Info: Info{Version: "test", Demo: true}, Heartbeat: time.Hour}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, app
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func TestPageAndSharedMapServed(t *testing.T) {
	ts, _ := server(t)
	for _, path := range []string{"/", "/app.js", "/satmap.js", "/sat.js", "/style.css", "/map/basemap.js", "/map/geo.js"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP", path)
		}
	}
	if resp, _ := http.Get(ts.URL + "/map/geo.test.mjs"); resp.StatusCode != 404 {
		t.Errorf("tests are served: %d", resp.StatusCode)
	}
}

func TestGuardRefusesOtherSites(t *testing.T) {
	ts, _ := server(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/location", strings.NewReader(`{"lat":1,"lon":2,"altM":0,"source":"map"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin location write: %d", resp.StatusCode)
	}
}

func TestLocationAndTracks(t *testing.T) {
	ts, app := server(t)
	if code, _ := post(t, ts.URL+"/api/location", `{"lat":95,"lon":0,"altM":0,"source":"map"}`); code != 400 {
		t.Errorf("latitude 95: %d", code)
	}
	if code, _ := post(t, ts.URL+"/api/location", `{"lat":53.35,"lon":-6.26,"altM":0,"source":"browser","extra":1}`); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	code, cfg := post(t, ts.URL+"/api/location", `{"lat":53.35,"lon":-6.26,"altM":20,"source":"browser"}`)
	if code != 200 || cfg["location"] == nil {
		t.Fatalf("location: %d %v", code, cfg)
	}
	if app.Config().Location.Source != "browser" {
		t.Error("location not set")
	}
	resp, _ := http.Get(ts.URL + "/api/track?id=99901")
	var tr satapp.Track
	json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	if len(tr.Future) < 100 {
		t.Errorf("track: %d points", len(tr.Future))
	}
	if resp, _ := http.Get(ts.URL + "/api/track?id=nope"); resp.StatusCode != 404 {
		t.Errorf("unknown satellite: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/api/pass?id=99901")
	var pass struct {
		Pass   satapp.PassInfo
		Points []satapp.SkyPoint
	}
	json.NewDecoder(resp.Body).Decode(&pass)
	resp.Body.Close()
	if pass.Pass.ID != "99901" || len(pass.Points) < 10 {
		t.Errorf("pass: %+v, %d points", pass.Pass, len(pass.Points))
	}
	if code, _ := post(t, ts.URL+"/api/alert", `{"id":"99901","on":true}`); code != 200 || !app.Config().Alerts["99901"] {
		t.Errorf("alert: %d", code)
	}
	if code, _ := post(t, ts.URL+"/api/alert-timing", `{"minutes":0,"minEl":10}`); code != 400 {
		t.Errorf("alert 0 minutes before: %d", code)
	}
}

func TestEventStream(t *testing.T) {
	ts, _ := server(t)
	post(t, ts.URL+"/api/location", `{"lat":53.35,"lon":-6.26,"altM":20,"source":"browser"}`)
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	seen := map[string]bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		var typ string
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				typ = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				seen[typ] = true
				if typ == "hello" {
					var h struct {
						Info     Info
						Snapshot satapp.Snapshot
					}
					if err := json.Unmarshal([]byte(v), &h); err != nil || h.Info.Protocol != ProtocolVersion || len(h.Snapshot.Sats) != 5 {
						t.Errorf("hello: %v %+v", err, h.Info)
					}
				}
				if seen["hello"] && seen["tick"] {
					return
				}
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("events seen: %v", seen)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 0))
}
