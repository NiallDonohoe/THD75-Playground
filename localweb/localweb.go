// Package localweb holds what every page server in this module shares: a
// listener that only binds loopback, and a guard that refuses requests
// that did not come from the page itself.
package localweb

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// Listen opens a TCP listener, refusing any address that is not loopback.
// program names the program in the error.
func Listen(program, addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("refusing to listen on %q: %s serves localhost only", addr, program)
		}
	}
	return net.Listen("tcp", addr)
}

// Guard refuses requests that did not come from the page itself: the Host
// must be a loopback name (against DNS rebinding), and a browser's
// Sec-Fetch-Site and Origin headers, when present, must say same origin.
// It also sets a strict Content-Security-Policy: text on the pages comes
// from the air or the network, and they never run anything but their own
// scripts.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" && sfs != "none" {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "foreign origin refused", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteEvent writes one server-sent event. Compact JSON has no newlines,
// so one data line holds it.
func WriteEvent(w http.ResponseWriter, typ string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data)
}
