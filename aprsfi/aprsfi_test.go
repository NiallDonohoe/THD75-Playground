package aprsfi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const key = "SECRET-KEY-123"

type fake struct {
	srv      *httptest.Server
	requests atomic.Int32
	reply    atomic.Value // string
	status   atomic.Int32
	lastUA   atomic.Value
	lastName atomic.Value
}

func newFake(t *testing.T) *fake {
	f := &fake{}
	f.status.Store(200)
	f.reply.Store(`{"command":"get","result":"ok","what":"loc","found":1,"entries":[
		{"class":"a","name":"OH7RDA","type":"l","time":"1267445689","lasttime":"1270580127",
		 "lat":"63.06717","lng":"27.66050","course":"90","speed":"18.5","altitude":"120.5",
		 "symbol":"/#","srccall":"OH7RDA","dstcall":"UIDIGI","comment":" UiDigi","path":"WIDE2-2,qAR,OH7AA"}]}`)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.lastUA.Store(r.UserAgent())
		f.lastName.Store(r.URL.Query().Get("name"))
		q := r.URL.Query()
		if q.Get("what") != "loc" || q.Get("apikey") != key || q.Get("format") != "json" {
			t.Errorf("query %v", q)
		}
		w.WriteHeader(int(f.status.Load()))
		w.Write([]byte(f.reply.Load().(string)))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func client(f *fake, now *time.Time) *Client {
	return &Client{Key: key, UserAgent: "d75aprs/test (+https://example.org)", BaseURL: f.srv.URL, Now: func() time.Time { return *now }}
}

func TestLocate(t *testing.T) {
	f := newFake(t)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	c := client(f, &now)
	entries, err := c.Locate(context.Background(), "OH7RDA")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries %+v", entries)
	}
	e := entries[0]
	if e.Name != "OH7RDA" || e.Type != "l" || e.Lat != 63.06717 || e.Lng != 27.6605 || !e.HasCourse || e.Course != 90 ||
		!e.HasSpeed || e.Speed != 18.5 || !e.HasAlt || e.Altitude != 120.5 || e.Symbol != "/#" || e.Path != "WIDE2-2,qAR,OH7AA" {
		t.Errorf("entry %+v", e)
	}
	if !e.Time.Equal(time.Unix(1267445689, 0)) || !e.LastTime.Equal(time.Unix(1270580127, 0)) {
		t.Errorf("times %v %v", e.Time, e.LastTime)
	}
	if ua := f.lastUA.Load(); ua != "d75aprs/test (+https://example.org)" {
		t.Errorf("User-Agent %q", ua)
	}
}

func TestCache(t *testing.T) {
	f := newFake(t)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	c := client(f, &now)
	ctx := context.Background()
	c.Locate(ctx, "OH7RDA", "NOBODY")
	if _, err := c.Locate(ctx, "oh7rda"); err != nil || f.requests.Load() != 1 {
		t.Errorf("cached name fetched again: %d requests, %v", f.requests.Load(), err)
	}
	// A name aprs.fi did not know is remembered too.
	if e, err := c.Locate(ctx, "NOBODY"); err != nil || len(e) != 0 || f.requests.Load() != 1 {
		t.Errorf("unknown name: %v %v, %d requests", e, err, f.requests.Load())
	}
	// Only the names not cached are asked for.
	c.Locate(ctx, "OH7RDA", "NEW")
	if f.requests.Load() != 2 || f.lastName.Load() != "NEW" {
		t.Errorf("asked for %q", f.lastName.Load())
	}
	now = now.Add(6 * time.Minute)
	c.Locate(ctx, "OH7RDA")
	if f.requests.Load() != 3 {
		t.Errorf("expired entry not fetched again")
	}
}

func TestFailuresBackOffAndHideTheKey(t *testing.T) {
	f := newFake(t)
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	c := client(f, &now)
	ctx := context.Background()

	f.reply.Store(`{"command":"get","result":"fail","description":"authentication failed: wrong API key ` + key + `"}`)
	_, err := c.Locate(ctx, "A")
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error %v", err)
	}
	if _, err := c.Locate(ctx, "A"); !errors.Is(err, ErrBackoff) || f.requests.Load() != 1 {
		t.Errorf("no backoff: %v, %d requests", err, f.requests.Load())
	}
	now = now.Add(6 * time.Second)
	f.status.Store(500)
	if _, err := c.Locate(ctx, "A"); err == nil || f.requests.Load() != 2 {
		t.Errorf("HTTP 500: %v", err)
	}
	// The second failure doubles the wait.
	now = now.Add(6 * time.Second)
	if _, err := c.Locate(ctx, "A"); !errors.Is(err, ErrBackoff) {
		t.Errorf("second backoff not longer: %v", err)
	}
	now = now.Add(5 * time.Second)
	f.status.Store(200)
	f.reply.Store(`{"command":"get","result":"ok","what":"loc","found":0,"entries":[]}`)
	if _, err := c.Locate(ctx, "A"); err != nil {
		t.Errorf("after the wait: %v", err)
	}

	// A connection error quotes the URL, which holds the key.
	f.srv.Close()
	c2 := client(f, &now)
	if _, err := c2.Locate(ctx, "B"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("connection error %v", err)
	}
}

func TestLocateRefuses(t *testing.T) {
	c := &Client{}
	if _, err := c.Locate(context.Background(), "A"); err == nil {
		t.Error("no key accepted")
	}
	c.Key = key
	names := make([]string, MaxNames+1)
	if _, err := c.Locate(context.Background(), names...); err == nil {
		t.Error("21 names accepted")
	}
	if e, err := c.Locate(context.Background()); e != nil || err != nil {
		t.Errorf("no names: %v %v", e, err)
	}
}
