package satdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const ommJSON = `[
{"OBJECT_NAME":"ISS (ZARYA)","NORAD_CAT_ID":25544,"EPOCH":"2026-10-29T12:00:00.000000","MEAN_MOTION":15.5,"ECCENTRICITY":0.0005,"INCLINATION":51.64,"RA_OF_ASC_NODE":100,"ARG_OF_PERICENTER":90,"MEAN_ANOMALY":10,"BSTAR":0.0002},
{"OBJECT_NAME":"RS-44","NORAD_CAT_ID":44909,"EPOCH":"2026-10-29T06:00:00.000000","MEAN_MOTION":12.8,"ECCENTRICITY":0.02,"INCLINATION":82.5,"RA_OF_ASC_NODE":10,"ARG_OF_PERICENTER":20,"MEAN_ANOMALY":30,"BSTAR":0.0001},
{"OBJECT_NAME":"NO RADIO","NORAD_CAT_ID":1,"EPOCH":"2026-10-29T06:00:00.000000","MEAN_MOTION":15,"ECCENTRICITY":0.001,"INCLINATION":98,"BSTAR":0.0001}
]`

const satnogsJSON = `[
{"uuid":"a","description":"FM voice","alive":true,"type":"Transceiver","uplink_low":145990000,"uplink_high":null,"downlink_low":437800000,"downlink_high":null,"mode":"FM","invert":false,"baud":null,"sat_id":"XSKZ","norad_cat_id":25544,"status":"active"},
{"uuid":"b","description":"APRS","alive":true,"type":"Transceiver","uplink_low":145825000,"downlink_low":145825000,"mode":"AFSK","baud":1200,"sat_id":"XSKZ","norad_cat_id":25544,"status":"active"},
{"uuid":"c","description":"Dead beacon","alive":false,"type":"Transmitter","downlink_low":145800000,"mode":"FM","norad_cat_id":25544,"status":"active"},
{"uuid":"d","description":"S-band","alive":true,"type":"Transmitter","downlink_low":2265000000,"mode":"FM","norad_cat_id":25544,"status":"active"},
{"uuid":"e","description":"Linear transponder","alive":true,"type":"Transponder","uplink_low":145935000,"uplink_high":145995000,"downlink_low":435610000,"downlink_high":435670000,"mode":"SSB","invert":true,"sat_id":"RSXX","norad_cat_id":44909,"status":"active"},
{"uuid":"f","description":"CW beacon","alive":true,"type":"Transmitter","downlink_low":435605000,"mode":"CW","norad_cat_id":44909,"status":"active"},
{"uuid":"g","description":"BPSK telemetry","alive":true,"type":"Transmitter","downlink_low":435500000,"mode":"BPSK","norad_cat_id":44909,"status":"active"},
{"uuid":"h","description":"Inactive","alive":true,"type":"Transmitter","downlink_low":435700000,"mode":"FM","norad_cat_id":44909,"status":"inactive"},
{"uuid":"i","description":"No satellite","alive":true,"type":"Transmitter","downlink_low":435700000,"mode":"FM","norad_cat_id":null,"status":"active"}
]`

type fakeUpstream struct {
	srv        *httptest.Server
	celestrak  atomic.Int32
	satnogs    atomic.Int32
	fail       atomic.Bool
	userAgents chan string
}

func newUpstream(t *testing.T) *fakeUpstream {
	u := &fakeUpstream{userAgents: make(chan string, 100)}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.userAgents <- r.UserAgent()
		if u.fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/celestrak":
			u.celestrak.Add(1)
			w.Write([]byte(ommJSON))
		case "/satnogs":
			u.satnogs.Add(1)
			w.Write([]byte(satnogsJSON))
		case "/html":
			w.Write([]byte("<html>maintenance</html>"))
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

type clock struct{ off time.Duration }

func (c *clock) now() time.Time { return time.Now().Add(c.off) }

func fetcher(t *testing.T, u *fakeUpstream, c *clock) *Fetcher {
	f := &Fetcher{Dir: t.TempDir(), UserAgent: "d75sat/test", Now: c.now}
	f.SetURLs(u.srv.URL+"/celestrak", u.srv.URL+"/satnogs")
	return f
}

func TestCatalogJoinsAndFilters(t *testing.T) {
	u := newUpstream(t)
	sats, status, err := fetcher(t, u, &clock{}).Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sats) != 2 || sats[0].Name != "ISS (ZARYA)" || sats[1].Name != "RS-44" {
		t.Fatalf("catalogue: %+v", sats)
	}
	iss, rs := sats[0], sats[1]
	if len(iss.Transmitters) != 2 || iss.Transmitters[0].Downlink != 145825000 || iss.Transmitters[0].RadioMode != "FM" {
		t.Errorf("ISS transmitters (alive, in band B, by frequency): %+v", iss.Transmitters)
	}
	if iss.SatNOGS != "https://db.satnogs.org/satellite/XSKZ" {
		t.Errorf("SatNOGS page %q", iss.SatNOGS)
	}
	if len(rs.Transmitters) != 2 {
		t.Fatalf("RS-44 transmitters (no BPSK, no inactive): %+v", rs.Transmitters)
	}
	cw, lin := rs.Transmitters[0], rs.Transmitters[1]
	if cw.RadioMode != "CW" || lin.RadioMode != "USB" || lin.DownlinkTop != 435670000 || !lin.Invert {
		t.Errorf("RS-44: %+v %+v", cw, lin)
	}
	if status["celestrak"].Fetched.IsZero() || status["satnogs"].Error != "" {
		t.Errorf("status %+v", status)
	}
	if ua := <-u.userAgents; ua != "d75sat/test" {
		t.Errorf("User-Agent %q", ua)
	}
}

func TestCacheAndRefreshIntervals(t *testing.T) {
	u := newUpstream(t)
	c := &clock{}
	f := fetcher(t, u, c)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, err := f.Catalog(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if u.celestrak.Load() != 1 || u.satnogs.Load() != 1 {
		t.Fatalf("within the interval: %d and %d requests, want 1 each", u.celestrak.Load(), u.satnogs.Load())
	}
	c.off = ElementsEvery + time.Minute
	f.Catalog(ctx)
	if u.celestrak.Load() != 2 || u.satnogs.Load() != 1 {
		t.Errorf("after 2 h: %d and %d requests, want 2 and 1", u.celestrak.Load(), u.satnogs.Load())
	}
	c.off = TransmittersEvery + 3*time.Hour
	f.Catalog(ctx)
	if u.celestrak.Load() != 3 || u.satnogs.Load() != 2 {
		t.Errorf("after a day: %d and %d requests, want 3 and 2", u.celestrak.Load(), u.satnogs.Load())
	}
}

func TestFailureFallsBackToCacheAndBacksOff(t *testing.T) {
	u := newUpstream(t)
	c := &clock{}
	f := fetcher(t, u, c)
	ctx := context.Background()
	f.Catalog(ctx)
	u.fail.Store(true)
	c.off = ElementsEvery + time.Minute
	sats, status, err := f.Catalog(ctx)
	if err != nil || len(sats) != 2 {
		t.Fatalf("stale cache not used: %v", err)
	}
	if !strings.Contains(status["celestrak"].Error, "503") {
		t.Errorf("status %+v", status["celestrak"])
	}
	f.Catalog(ctx) // still within the backoff
	f.Catalog(ctx)
	if n := len(u.userAgents); n != 3 { // 2 at first, then 1 that failed
		t.Errorf("%d requests, want 3: retried while backing off", n)
	}
	c.off += firstBackoff + time.Second
	u.fail.Store(false)
	if _, status, _ := f.Catalog(ctx); status["celestrak"].Error != "" {
		t.Errorf("after the backoff: %+v", status["celestrak"])
	}
}

func TestNoCacheNoNetworkIsAnError(t *testing.T) {
	u := newUpstream(t)
	u.fail.Store(true)
	if _, _, err := fetcher(t, u, &clock{}).Catalog(context.Background()); err == nil {
		t.Error("no error with no cache and no network")
	}
	f := fetcher(t, u, &clock{})
	f.Offline = true
	if _, status, err := f.Catalog(context.Background()); err == nil || !status["celestrak"].Off {
		t.Errorf("offline with no cache: %v %+v", err, status)
	}
	// One request from the first fetcher (it stops at CelesTrak's
	// failure), none from the offline one.
	if n := len(u.userAgents); n != 1 {
		t.Errorf("%d requests, want 1: the offline fetcher made some", n)
	}
}

func TestNonJSONResponseNotCached(t *testing.T) {
	u := newUpstream(t)
	f := fetcher(t, u, &clock{})
	f.SetURLs(u.srv.URL+"/html", u.srv.URL+"/satnogs")
	if _, status, err := f.Catalog(context.Background()); err == nil || !strings.Contains(status["celestrak"].Error, "not a JSON list") {
		t.Errorf("HTML page accepted: %v %+v", err, status)
	}
}

func TestRadioMode(t *testing.T) {
	for mode, want := range map[string]string{
		"FM": "FM", "AFSK": "FM", "GMSK": "FM", "GFSK": "FM", "FSK": "FM", "AFSK S-Net": "FM",
		"CW": "CW", "USB": "USB", "SSB": "USB", "LSB": "LSB", "BPSK": "", "LoRa": "", "": "",
	} {
		if got := radioMode(mode); got != want {
			t.Errorf("%q: %q, want %q", mode, got, want)
		}
	}
}
