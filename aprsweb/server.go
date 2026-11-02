// Package aprsweb serves d75aprs's page: the embedded assets, a stream of
// server-sent events from the store, aprs.fi lookups on request, and map
// tiles. It listens on localhost only and refuses requests from other sites.
// The wire format is in docs/APRS_PROTOCOL.md.
package aprsweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"time"

	"d75scope/aprsapp"
	"d75scope/aprsfi"
	"d75scope/localweb"
)

// ProtocolVersion is sent in the hello event.
const ProtocolVersion = 1

// Info describes the program to the page.
type Info struct {
	Protocol   int     `json:"protocol"`
	Version    string  `json:"version"`
	MyCall     string  `json:"myCall"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Demo       bool    `json:"demo"`
	RFTransmit bool    `json:"rfTransmit"`
	Tiles      bool    `json:"tiles"`
	APRSFI     bool    `json:"aprsfi"`
}

// Server is the page's HTTP server.
type Server struct {
	Store  *aprsapp.Store
	Info   Info
	Assets fs.FS // index.html, style.css and the scripts
	// Lookup asks aprs.fi; nil turns lookups off.
	Lookup func(ctx context.Context, names ...string) ([]aprsfi.Entry, error)
	// Tiles serves /tiles/{z}/{x}/{y}; nil turns the map's tiles off.
	Tiles http.Handler
	// Heartbeat is the interval of keepalive comments on the event stream.
	Heartbeat time.Duration
}

// Handler returns the server's routes behind the localhost guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(s.Assets))
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("POST /api/lookup", s.lookup)
	if s.Tiles != nil {
		mux.Handle("GET /tiles/{z}/{x}/{y}", s.Tiles)
	}
	return localweb.Guard(mux)
}

// Listen opens a TCP listener, refusing any address that is not loopback.
func Listen(addr string) (net.Listener, error) { return localweb.Listen("d75aprs", addr) }

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	snap, ch, cancel := s.Store.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	info := s.Info
	info.Protocol = ProtocolVersion
	hello, err := json.Marshal(struct {
		Info     Info             `json:"info"`
		Snapshot aprsapp.Snapshot `json:"snapshot"`
	}{info, snap})
	if err != nil {
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	writeEvent(w, "hello", hello)
	fl.Flush()

	beat := s.Heartbeat
	if beat == 0 {
		beat = 15 * time.Second
	}
	t := time.NewTicker(beat)
	defer t.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				// Fell behind: end the stream; the page reconnects and gets a
				// fresh snapshot.
				return
			}
			writeEvent(w, ev.Type, ev.Data)
			// Send whatever else is already queued before flushing.
			for more := true; more; {
				select {
				case ev, ok := <-ch:
					if !ok {
						fl.Flush()
						return
					}
					writeEvent(w, ev.Type, ev.Data)
				default:
					more = false
				}
			}
			fl.Flush()
		case <-t.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, typ string, data []byte) { localweb.WriteEvent(w, typ, data) }

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "send JSON"})
		return
	}
	var req struct {
		Call string `json:"call"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || !validCall(req.Call) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "give a callsign or object name: 1 to 9 letters, digits or hyphens"})
		return
	}
	if s.Lookup == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "aprs.fi lookups are off (no API key)"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	entries, err := s.Lookup(ctx, req.Call)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, aprsfi.ErrBackoff) {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	keys := s.Store.AddLookup(entries)
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func validCall(s string) bool {
	if len(s) < 1 || len(s) > 9 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) { localweb.WriteJSON(w, status, v) }
