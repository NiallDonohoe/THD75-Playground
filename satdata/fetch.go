package satdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"d75scope/orbit"
)

// The sources, and how often each may be asked.
const (
	CelesTrakURL = "https://celestrak.org/NORAD/elements/gp.php?GROUP=amateur&FORMAT=json"
	SatNOGSURL   = "https://db.satnogs.org/api/transmitters/?format=json"

	ElementsEvery     = 2 * time.Hour  // CelesTrak's guidance
	TransmittersEvery = 24 * time.Hour //
	firstBackoff      = 5 * time.Minute
	maxBackoff        = 2 * time.Hour
	maxBody           = 32 << 20
)

// Source is one upstream: its URL, how often it may be fetched, and the
// file it is cached in.
type source struct {
	name, url, file string
	every           time.Duration

	mu      sync.Mutex
	backoff time.Duration
	retryAt time.Time
}

// Status says how a source stands, for the page.
type Status struct {
	Fetched time.Time `json:"fetched,omitzero"` // when the cached copy was downloaded
	Error   string    `json:"error,omitempty"`  // the last failure, if the cache is being used instead
	Off     bool      `json:"off,omitempty"`
}

// Fetcher downloads the two sources into a cache directory and reads them
// back. Requests carry only a User-Agent: never the station's position.
type Fetcher struct {
	Dir       string
	UserAgent string
	HTTP      *http.Client
	Offline   bool // use the cache only
	Now       func() time.Time

	once                   sync.Once
	elements, transmitters *source
}

func (f *Fetcher) init() {
	f.once.Do(func() {
		f.elements = &source{name: "CelesTrak", url: CelesTrakURL, file: "celestrak-amateur.json", every: ElementsEvery}
		f.transmitters = &source{name: "SatNOGS", url: SatNOGSURL, file: "satnogs-transmitters.json", every: TransmittersEvery}
	})
}

// SetURLs points the fetcher somewhere else (tests).
func (f *Fetcher) SetURLs(celestrak, satnogs string) {
	f.init()
	f.elements.url, f.transmitters.url = celestrak, satnogs
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Catalog returns the joined catalogue, refreshing either source whose
// cache is older than its interval. A failed refresh falls back to the
// cache and is reported in the status; with no cache at all the error is
// returned.
func (f *Fetcher) Catalog(ctx context.Context) ([]Satellite, map[string]Status, error) {
	f.init()
	status := map[string]Status{}
	elData, st, err := f.get(ctx, f.elements)
	status["celestrak"] = st
	if err != nil {
		return nil, status, err
	}
	txData, st, err := f.get(ctx, f.transmitters)
	status["satnogs"] = st
	if err != nil {
		return nil, status, err
	}
	els, _ := orbit.ParseOMMJSON(elData)
	txs, pages, err := parseTransmitters(txData)
	if err != nil {
		return nil, status, err
	}
	return Join(els, txs, pages), status, nil
}

// get returns a source's data from the cache, refreshing it first when it
// is due and the source is not backing off.
func (f *Fetcher) get(ctx context.Context, s *source) ([]byte, Status, error) {
	path := filepath.Join(f.Dir, s.file)
	cached, cerr := os.ReadFile(path)
	var st Status
	if fi, err := os.Stat(path); err == nil && cerr == nil {
		st.Fetched = fi.ModTime()
	}
	due := cerr != nil || f.now().Sub(st.Fetched) >= s.every
	if f.Offline {
		st.Off = true
		due = false
	}
	if due {
		s.mu.Lock()
		waiting := f.now().Before(s.retryAt)
		s.mu.Unlock()
		if !waiting {
			data, err := f.download(ctx, s)
			s.mu.Lock()
			if err != nil {
				if s.backoff == 0 {
					s.backoff = firstBackoff
				} else {
					s.backoff = min(2*s.backoff, maxBackoff)
				}
				s.retryAt = f.now().Add(s.backoff)
				s.mu.Unlock()
				st.Error = err.Error()
			} else {
				s.backoff, s.retryAt = 0, time.Time{}
				s.mu.Unlock()
				if err := writeAtomic(path, data); err != nil {
					st.Error = "cache: " + err.Error()
				}
				return data, Status{Fetched: f.now()}, nil
			}
		} else {
			st.Error = fmt.Sprintf("%s: waiting to retry", s.name)
		}
	}
	if cerr != nil {
		if st.Error == "" {
			st.Error = "no cached copy"
		}
		return nil, st, fmt.Errorf("%s: %s", s.name, st.Error)
	}
	return cached, st, nil
}

func (f *Fetcher) download(ctx context.Context, s *source) ([]byte, error) {
	c := f.HTTP
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", s.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", s.name, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", s.name, err)
	}
	if len(data) > maxBody {
		return nil, fmt.Errorf("%s: response over %d MB", s.name, maxBody>>20)
	}
	// Refuse to cache something that is not the expected JSON array, such
	// as an HTML error page or CelesTrak's plain-text notices.
	if len(data) == 0 || data[0] != '[' {
		return nil, fmt.Errorf("%s: response is not a JSON list", s.name)
	}
	return data, nil
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
