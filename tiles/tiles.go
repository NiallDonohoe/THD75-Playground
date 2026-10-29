// Package tiles serves map tiles to the page from a disk cache, fetching
// from an upstream tile server only when a tile is missing or stale. It keeps
// to the OpenStreetMap tile usage policy
// (https://operations.osmfoundation.org/policies/tiles/): a descriptive
// User-Agent, caching that honours the server's headers, at most two
// downloads at a time, and nothing fetched that the page did not ask for.
package tiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OSM is OpenStreetMap's standard tile layer.
const OSM = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

// MaxZoom is the deepest zoom level served.
const MaxZoom = 19

// Proxy is an http.Handler for paths ending in "{z}/{x}/{y}" (the router
// supplies them as path values z, x and y).
type Proxy struct {
	Upstream   string        // URL template with {z}, {x} and {y}
	Dir        string        // cache directory
	UserAgent  string        // required by the tile policy
	HTTP       *http.Client  // a client with a 10 s timeout if nil
	DefaultTTL time.Duration // when the server sends no max-age; 7 days if zero
	Now        func() time.Time

	once     sync.Once
	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]*fetch
}

type fetch struct {
	done chan struct{}
	err  error
}

// meta is stored beside each tile.
type meta struct {
	Expires      time.Time `json:"expires"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	ContentType  string    `json:"contentType,omitempty"`
}

func (p *Proxy) init() {
	p.once.Do(func() {
		p.sem = make(chan struct{}, 2)
		p.inflight = map[string]*fetch{}
	})
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.init()
	z, x, y, ok := coords(r.PathValue("z"), r.PathValue("x"), strings.TrimSuffix(r.PathValue("y"), ".png"))
	if !ok {
		http.Error(w, "no such tile", http.StatusBadRequest)
		return
	}
	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	m, fresh := p.cached(key)
	if !fresh {
		if err := p.refresh(r.Context(), key, z, x, y, m); err != nil {
			if m == nil {
				http.Error(w, "tile unavailable", http.StatusBadGateway)
				return
			}
			// Serve the stale copy rather than nothing.
		}
		m, _ = p.cached(key)
		if m == nil {
			http.Error(w, "tile unavailable", http.StatusBadGateway)
			return
		}
	}
	f, err := os.Open(p.path(key) + ".png")
	if err != nil {
		http.Error(w, "tile unavailable", http.StatusBadGateway)
		return
	}
	defer f.Close()
	if m.ContentType != "" {
		w.Header().Set("Content-Type", m.ContentType)
	} else {
		w.Header().Set("Content-Type", "image/png")
	}
	if age := int(m.Expires.Sub(p.now()).Seconds()); age > 0 {
		w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(age))
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	io.Copy(w, f)
}

func coords(zs, xs, ys string) (z, x, y int, ok bool) {
	var err1, err2, err3 error
	z, err1 = strconv.Atoi(zs)
	x, err2 = strconv.Atoi(xs)
	y, err3 = strconv.Atoi(ys)
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || z > MaxZoom {
		return 0, 0, 0, false
	}
	n := 1 << z
	return z, x, y, x >= 0 && x < n && y >= 0 && y < n
}

func (p *Proxy) path(key string) string { return filepath.Join(p.Dir, filepath.FromSlash(key)) }

// cached returns the tile's metadata, if the tile is on disk, and whether it
// is still fresh.
func (p *Proxy) cached(key string) (*meta, bool) {
	b, err := os.ReadFile(p.path(key) + ".json")
	if err != nil {
		return nil, false
	}
	var m meta
	if json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	if _, err := os.Stat(p.path(key) + ".png"); err != nil {
		return nil, false
	}
	return &m, p.now().Before(m.Expires)
}

// refresh fetches one tile, joining a fetch of the same tile already under
// way.
func (p *Proxy) refresh(ctx context.Context, key string, z, x, y int, old *meta) error {
	p.mu.Lock()
	if f, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		select {
		case <-f.done:
			return f.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Another request may have finished this tile since the caller looked.
	if _, fresh := p.cached(key); fresh {
		p.mu.Unlock()
		return nil
	}
	f := &fetch{done: make(chan struct{})}
	p.inflight[key] = f
	p.mu.Unlock()

	f.err = p.download(ctx, key, z, x, y, old)
	p.mu.Lock()
	delete(p.inflight, key)
	p.mu.Unlock()
	close(f.done)
	return f.err
}

func (p *Proxy) download(ctx context.Context, key string, z, x, y int, old *meta) error {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	u := strings.NewReplacer("{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(p.Upstream)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", p.UserAgent)
	if old != nil {
		if old.ETag != "" {
			req.Header.Set("If-None-Match", old.ETag)
		}
		if old.LastModified != "" {
			req.Header.Set("If-Modified-Since", old.LastModified)
		}
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	m := meta{Expires: p.expires(resp.Header)}
	switch resp.StatusCode {
	case http.StatusNotModified:
		if old == nil {
			return fmt.Errorf("tile %s: 304 with nothing cached", key)
		}
		m.ETag, m.LastModified, m.ContentType = old.ETag, old.LastModified, old.ContentType
		return p.writeMeta(key, m)
	case http.StatusOK:
	default:
		return fmt.Errorf("tile %s: HTTP %d", key, resp.StatusCode)
	}
	m.ETag = resp.Header.Get("ETag")
	m.LastModified = resp.Header.Get("Last-Modified")
	m.ContentType = resp.Header.Get("Content-Type")
	if !strings.HasPrefix(m.ContentType, "image/") {
		return fmt.Errorf("tile %s: content type %q", key, m.ContentType)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if err := writeAtomic(p.path(key)+".png", body); err != nil {
		return err
	}
	return p.writeMeta(key, m)
}

func (p *Proxy) writeMeta(key string, m meta) error {
	b, _ := json.Marshal(m)
	return writeAtomic(p.path(key)+".json", b)
}

// expires reads max-age from Cache-Control, then Expires, then falls back to
// the default lifetime.
func (p *Proxy) expires(h http.Header) time.Time {
	now := p.now()
	for _, part := range strings.Split(h.Get("Cache-Control"), ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "max-age="); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return now.Add(time.Duration(n) * time.Second)
			}
		}
	}
	if t, err := http.ParseTime(h.Get("Expires")); err == nil {
		return t
	}
	ttl := p.DefaultTTL
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	return now.Add(ttl)
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tile-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (p *Proxy) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p *Proxy) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
