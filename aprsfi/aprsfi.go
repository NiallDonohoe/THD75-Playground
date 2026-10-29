// Package aprsfi looks stations up on aprs.fi, within its API terms
// (https://aprs.fi/page/api): only when the owner asks, with the owner's own
// key, a descriptive User-Agent, a short timeout, a cache, and exponential
// backoff after failures. The key never appears in an error.
package aprsfi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the API endpoint.
const DefaultURL = "https://api.aprs.fi/api/get"

// MaxNames is the most callsigns one location query may name.
const MaxNames = 20

// ErrBackoff is returned while the client is waiting after failures.
var ErrBackoff = errors.New("aprs.fi: waiting after recent failures")

// Entry is one location record.
type Entry struct {
	Class     string // "a" APRS, "i" AIS, "w" web
	Name      string
	Type      string    // "l" station, "o" object, "i" item, "w" weather, "a" AIS
	Time      time.Time // when it first reported its current position
	LastTime  time.Time // its latest report at that position
	Lat, Lng  float64
	HasCourse bool
	Course    float64 // degrees
	HasSpeed  bool
	Speed     float64 // km/h, as aprs.fi gives it
	HasAlt    bool
	Altitude  float64 // metres
	Symbol    string
	SrcCall   string
	DstCall   string
	Comment   string
	Path      string
	Status    string
}

// Client queries aprs.fi. Set Key and UserAgent; the zero values of the
// other fields give the defaults.
type Client struct {
	Key       string
	UserAgent string        // "d75aprs/VERSION (+HOME PAGE)"
	BaseURL   string        // DefaultURL if empty
	HTTP      *http.Client  // a client with a 5 s timeout if nil
	TTL       time.Duration // cache lifetime, 5 minutes if zero
	Now       func() time.Time

	mu       sync.Mutex
	cache    map[string]cached // by upper-case name
	failures int
	retryAt  time.Time
}

type cached struct {
	entry   *Entry // nil: aprs.fi had nothing for the name
	expires time.Time
}

// Locate returns the latest known positions of up to MaxNames stations,
// objects or items. Names aprs.fi does not know are left out. Results are
// cached, so asking again within the TTL sends no request.
func (c *Client) Locate(ctx context.Context, names ...string) ([]Entry, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if len(names) > MaxNames {
		return nil, fmt.Errorf("aprs.fi: at most %d names per lookup", MaxNames)
	}
	if c.Key == "" {
		return nil, errors.New("aprs.fi: no API key")
	}
	now := c.now()
	var out []Entry
	var missing []string
	c.mu.Lock()
	for _, n := range names {
		k := strings.ToUpper(n)
		if e, ok := c.cache[k]; ok && now.Before(e.expires) {
			if e.entry != nil {
				out = append(out, *e.entry)
			}
		} else {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 && now.Before(c.retryAt) {
		c.mu.Unlock()
		return out, ErrBackoff
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}

	entries, err := c.fetch(ctx, missing)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.failures++
		c.retryAt = now.Add(min(5*time.Second<<min(c.failures-1, 10), 10*time.Minute))
		return out, err
	}
	c.failures, c.retryAt = 0, time.Time{}
	if c.cache == nil {
		c.cache = map[string]cached{}
	}
	expires := now.Add(c.ttl())
	for _, n := range missing {
		c.cache[strings.ToUpper(n)] = cached{expires: expires}
	}
	for i := range entries {
		e := entries[i]
		c.cache[strings.ToUpper(e.Name)] = cached{entry: &e, expires: expires}
		out = append(out, e)
	}
	return out, nil
}

func (c *Client) fetch(ctx context.Context, names []string) ([]Entry, error) {
	q := url.Values{"name": {strings.Join(names, ",")}, "what": {"loc"}, "apikey": {c.Key}, "format": {"json"}}
	base := c.BaseURL
	if base == "" {
		base = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, errors.New("aprs.fi: bad base URL")
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.client().Do(req)
	if err != nil {
		// A *url.Error quotes the URL, which holds the key: keep only its cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("aprs.fi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aprs.fi: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Result      string     `json:"result"`
		Description string     `json:"description"`
		Entries     []rawEntry `json:"entries"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("aprs.fi: unreadable reply: %v", err)
	}
	if r.Result != "ok" {
		return nil, fmt.Errorf("aprs.fi: %s", strings.ReplaceAll(r.Description, c.Key, "…"))
	}
	entries := make([]Entry, 0, len(r.Entries))
	for _, re := range r.Entries {
		entries = append(entries, re.entry())
	}
	return entries, nil
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (c *Client) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 5 * time.Minute
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// rawEntry is an entry as aprs.fi sends it: numbers arrive as strings.
type rawEntry struct {
	Class    string `json:"class"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Time     num    `json:"time"`
	LastTime num    `json:"lasttime"`
	Lat      num    `json:"lat"`
	Lng      num    `json:"lng"`
	Course   num    `json:"course"`
	Speed    num    `json:"speed"`
	Altitude num    `json:"altitude"`
	Symbol   string `json:"symbol"`
	SrcCall  string `json:"srccall"`
	DstCall  string `json:"dstcall"`
	Comment  string `json:"comment"`
	Path     string `json:"path"`
	Status   string `json:"status"`
}

func (r rawEntry) entry() Entry {
	e := Entry{
		Class: r.Class, Name: r.Name, Type: r.Type,
		Lat: r.Lat.v, Lng: r.Lng.v,
		HasCourse: r.Course.ok, Course: r.Course.v,
		HasSpeed: r.Speed.ok, Speed: r.Speed.v,
		HasAlt: r.Altitude.ok, Altitude: r.Altitude.v,
		Symbol: r.Symbol, SrcCall: r.SrcCall, DstCall: r.DstCall,
		Comment: r.Comment, Path: r.Path, Status: r.Status,
	}
	if r.Time.ok {
		e.Time = time.Unix(int64(r.Time.v), 0).UTC()
	}
	if r.LastTime.ok {
		e.LastTime = time.Unix(int64(r.LastTime.v), 0).UTC()
	} else {
		e.LastTime = e.Time
	}
	return e
}

// num reads a number sent as a JSON string ("63.06717") or a JSON number.
type num struct {
	v  float64
	ok bool
}

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("not a number: %q", s)
	}
	n.v, n.ok = v, true
	return nil
}
