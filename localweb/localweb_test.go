package localweb

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListenLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.2:0", ":0", "example.com:0"} {
		if ln, err := Listen("test", addr); err == nil {
			ln.Close()
			t.Errorf("Listen(%q) accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:0"} {
		ln, err := Listen("test", addr)
		if err != nil {
			t.Errorf("Listen(%q): %v", addr, err)
			continue
		}
		ln.Close()
	}
}

func TestGuard(t *testing.T) {
	h := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, c := range []struct {
		host, origin, site string
		want               int
	}{
		{"127.0.0.1:8078", "", "", 200},
		{"localhost:8078", "http://localhost:8078", "same-origin", 200},
		{"evil.example:8078", "", "", 403},
		{"127.0.0.1:8078", "http://evil.example", "", 403},
		{"127.0.0.1:8078", "", "cross-site", 403},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%+v: %d, want %d", c, w.Code, c.want)
		}
		if w.Code == 200 && w.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%+v: no CSP", c)
		}
	}
}
