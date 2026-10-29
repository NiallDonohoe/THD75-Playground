package tiles

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type upstream struct {
	srv         *httptest.Server
	requests    atomic.Int32
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	status      atomic.Int32
	contentType atomic.Value
	delay       time.Duration
	conditional atomic.Int32 // requests with If-None-Match
}

func newUpstream(t *testing.T, delay time.Duration) *upstream {
	u := &upstream{delay: delay}
	u.status.Store(200)
	u.contentType.Store("image/png")
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requests.Add(1)
		n := u.inFlight.Add(1)
		defer u.inFlight.Add(-1)
		for {
			m := u.maxInFlight.Load()
			if n <= m || u.maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		if r.UserAgent() != "d75aprs/test" {
			t.Errorf("User-Agent %q", r.UserAgent())
		}
		time.Sleep(u.delay)
		if st := int(u.status.Load()); st != 200 {
			w.WriteHeader(st)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			u.conditional.Add(1)
			w.Header().Set("Cache-Control", "max-age=60")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", u.contentType.Load().(string))
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprintf(w, "tile %s", r.URL.Path)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func setup(t *testing.T, u *upstream, now *time.Time) http.Handler {
	p := &Proxy{
		Upstream:  u.srv.URL + "/{z}/{x}/{y}.png",
		Dir:       t.TempDir(),
		UserAgent: "d75aprs/test",
		Now:       func() time.Time { return *now },
	}
	mux := http.NewServeMux()
	mux.Handle("GET /tiles/{z}/{x}/{y}", p)
	return mux
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestCachesTiles(t *testing.T) {
	u := newUpstream(t, 0)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	h := setup(t, u, &now)

	r := get(h, "/tiles/11/1012/673")
	if r.Code != 200 || r.Body.String() != "tile /11/1012/673.png" || r.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("first fetch: %d %q %v", r.Code, r.Body, r.Header())
	}
	if cc := r.Header().Get("Cache-Control"); cc != "max-age=60" {
		t.Errorf("Cache-Control %q", cc)
	}
	get(h, "/tiles/11/1012/673")
	if u.requests.Load() != 1 {
		t.Errorf("cached tile fetched again: %d requests", u.requests.Load())
	}

	// Stale: revalidated with the ETag; a 304 keeps the cached tile.
	now = now.Add(2 * time.Minute)
	r = get(h, "/tiles/11/1012/673")
	if r.Code != 200 || r.Body.String() != "tile /11/1012/673.png" || u.conditional.Load() != 1 {
		t.Errorf("revalidation: %d %q, %d conditional", r.Code, r.Body, u.conditional.Load())
	}
	get(h, "/tiles/11/1012/673")
	if u.requests.Load() != 2 {
		t.Errorf("revalidated tile not fresh again: %d requests", u.requests.Load())
	}
}

func TestUpstreamFailures(t *testing.T) {
	u := newUpstream(t, 0)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	h := setup(t, u, &now)
	get(h, "/tiles/3/1/1")

	u.status.Store(503)
	if r := get(h, "/tiles/3/2/2"); r.Code != http.StatusBadGateway {
		t.Errorf("nothing cached: %d", r.Code)
	}
	// A stale tile is better than none.
	now = now.Add(2 * time.Minute)
	if r := get(h, "/tiles/3/1/1"); r.Code != 200 || r.Body.String() != "tile /3/1/1.png" || r.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("stale fallback: %d %q %v", r.Code, r.Body, r.Header())
	}

	u2 := newUpstream(t, 0)
	u2.contentType.Store("text/html")
	h2 := setup(t, u2, &now)
	if r := get(h2, "/tiles/3/1/1"); r.Code != http.StatusBadGateway {
		t.Errorf("HTML served as a tile: %d", r.Code)
	}
}

func TestRejectsBadCoordinates(t *testing.T) {
	u := newUpstream(t, 0)
	now := time.Now()
	h := setup(t, u, &now)
	for _, path := range []string{"/tiles/20/0/0", "/tiles/2/4/0", "/tiles/2/0/-1", "/tiles/a/0/0", "/tiles/2/0/0.jpg"} {
		if r := get(h, path); r.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", path, r.Code)
		}
	}
	if r := get(h, "/tiles/2/3/3.png"); r.Code != 200 {
		t.Errorf("a .png suffix: %d", r.Code)
	}
	if u.requests.Load() != 1 {
		t.Errorf("%d upstream requests", u.requests.Load())
	}
}

func TestAtMostTwoDownloads(t *testing.T) {
	u := newUpstream(t, 30*time.Millisecond)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	h := setup(t, u, &now)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); get(h, fmt.Sprintf("/tiles/4/%d/5", i)) }()
		// The same tile twice at once is downloaded once.
		go func() { defer wg.Done(); get(h, fmt.Sprintf("/tiles/4/%d/5", i)) }()
	}
	wg.Wait()
	if m := u.maxInFlight.Load(); m > 2 {
		t.Errorf("%d downloads at once", m)
	}
	if n := u.requests.Load(); n != 8 {
		t.Errorf("%d requests for 8 tiles", n)
	}
}
