// Package satweb serves d75sat's page: the embedded assets, a stream of
// server-sent events from satapp, ground and sky tracks on request, the
// station's location and alert settings, and map tiles. It listens on
// localhost only and refuses requests from other sites. The wire format
// is in docs/SAT_PROTOCOL.md.
package satweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"time"

	"d75scope/localweb"
	"d75scope/satapp"
)

// ProtocolVersion is sent in the hello event.
const ProtocolVersion = 1

// Info describes the program to the page.
type Info struct {
	Protocol int     `json:"protocol"`
	Version  string  `json:"version"`
	Demo     bool    `json:"demo"`
	Speed    float64 `json:"speed,omitempty"` // demo clock rate
	Tiles    bool    `json:"tiles"`
	Network  bool    `json:"network"` // fetching from CelesTrak and SatNOGS
}

// Server is the page's HTTP server.
type Server struct {
	App       *satapp.App
	Info      Info
	Assets    fs.FS
	Tiles     http.Handler // nil turns the map's tiles off
	Heartbeat time.Duration
}

// Listen opens a TCP listener, refusing any address that is not loopback.
func Listen(addr string) (net.Listener, error) { return localweb.Listen("d75sat", addr) }

// Handler returns the server's routes behind the localhost guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(s.Assets))
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /api/track", s.track)
	mux.HandleFunc("GET /api/pass", s.pass)
	mux.HandleFunc("POST /api/location", s.location)
	mux.HandleFunc("POST /api/alert", s.alert)
	mux.HandleFunc("POST /api/alert-timing", s.alertTiming)
	if s.Tiles != nil {
		mux.Handle("GET /tiles/{z}/{x}/{y}", s.Tiles)
	}
	return localweb.Guard(mux)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	snap, ch, cancel := s.App.Subscribe()
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	info := s.Info
	info.Protocol = ProtocolVersion
	hello, err := json.Marshal(struct {
		Info     Info            `json:"info"`
		Snapshot satapp.Snapshot `json:"snapshot"`
	}{info, snap})
	if err != nil {
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	localweb.WriteEvent(w, "hello", hello)
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
				return // fell behind; the page reconnects for a fresh snapshot
			}
			localweb.WriteEvent(w, ev.Type, ev.Data)
			fl.Flush()
		case <-t.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) track(w http.ResponseWriter, r *http.Request) {
	tr, err := s.App.GroundTrack(r.URL.Query().Get("id"))
	if err != nil {
		localweb.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	localweb.WriteJSON(w, http.StatusOK, tr)
}

func (s *Server) pass(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	at := s.App.Now()
	if v := q.Get("at"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			localweb.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "at must be an RFC 3339 time"})
			return
		}
		at = t
	}
	info, pts, err := s.App.PassTrack(q.Get("id"), at)
	if err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, satapp.ErrUnknown) {
			status = http.StatusConflict
		}
		localweb.WriteJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	localweb.WriteJSON(w, http.StatusOK, map[string]any{"pass": info, "points": pts})
}

// readJSON decodes a small JSON body, answering the error itself.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		localweb.WriteJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "send JSON"})
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		localweb.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request: " + err.Error()})
		return false
	}
	return true
}

func (s *Server) location(w http.ResponseWriter, r *http.Request) {
	var l satapp.Location
	if !readJSON(w, r, &l) {
		return
	}
	if err := s.App.SetLocation(l); err != nil {
		localweb.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	localweb.WriteJSON(w, http.StatusOK, s.App.Config())
}

func (s *Server) alert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
		On bool   `json:"on"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.App.SetAlert(req.ID, req.On); err != nil {
		localweb.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	localweb.WriteJSON(w, http.StatusOK, s.App.Config())
}

func (s *Server) alertTiming(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes int     `json:"minutes"`
		MinEl   float64 `json:"minEl"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.App.SetAlertTiming(req.Minutes, req.MinEl); err != nil {
		localweb.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	localweb.WriteJSON(w, http.StatusOK, s.App.Config())
}
