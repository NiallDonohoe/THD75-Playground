// Package winlinkweb serves d75winlink's page: the embedded assets, a
// stream of server-sent events from winlinkapp, and the mailbox, compose
// and session endpoints. It listens on localhost only and refuses requests
// from other sites. The wire format is in docs/WINLINK_PROTOCOL.md.
package winlinkweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"d75scope/localweb"
	"d75scope/winlink"
	"d75scope/winlinkapp"
)

// ProtocolVersion is sent in the hello event.
const ProtocolVersion = 1

// Info describes the program to the page.
type Info struct {
	Protocol       int    `json:"protocol"`
	Version        string `json:"version"`
	MyCall         string `json:"myCall"`
	Demo           bool   `json:"demo"`
	RFTransmit     bool   `json:"rfTransmit"`
	MaxAttachments int    `json:"maxAttachments"`
	MaxMessage     int    `json:"maxMessage"`
}

// Server is the page's HTTP server.
type Server struct {
	App       *winlinkapp.App
	Info      Info
	Assets    fs.FS
	Heartbeat time.Duration
}

// Listen opens a TCP listener, refusing any address that is not loopback.
func Listen(addr string) (net.Listener, error) { return localweb.Listen("d75winlink", addr) }

// Handler returns the server's routes behind the localhost guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(s.Assets))
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /api/message/{mid}", s.message)
	mux.HandleFunc("GET /api/message/{mid}/file/{n}", s.file)
	mux.HandleFunc("POST /api/estimate", s.estimate)
	mux.HandleFunc("POST /api/queue", s.queue)
	mux.HandleFunc("POST /api/move", s.move)
	mux.HandleFunc("POST /api/delete", s.delete)
	mux.HandleFunc("POST /api/connect", s.connect)
	mux.HandleFunc("POST /api/stop", s.stop)
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
		Info     Info                `json:"info"`
		Snapshot winlinkapp.Snapshot `json:"snapshot"`
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
				return // fell behind: the page reconnects for a fresh snapshot
			}
			localweb.WriteEvent(w, ev.Type, ev.Data)
			for more := true; more; {
				select {
				case ev, ok := <-ch:
					if !ok {
						fl.Flush()
						return
					}
					localweb.WriteEvent(w, ev.Type, ev.Data)
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

type errorBody struct {
	Error string `json:"error"`
}

func fail(w http.ResponseWriter, status int, err error) {
	localweb.WriteJSON(w, status, errorBody{err.Error()})
}

// readJSON decodes a JSON request body of at most limit bytes.
func readJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, errors.New("send JSON"))
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, errors.New("the message is too large"))
		} else {
			fail(w, http.StatusBadRequest, fmt.Errorf("bad request: %v", err))
		}
		return false
	}
	return true
}

// Message is a message as the reading pane shows it.
type Message struct {
	winlinkapp.Summary
	Body  string `json:"body"`
	Mbo   string `json:"mbo,omitempty"`
	Type  string `json:"type,omitempty"`
	Files []File `json:"files"`
	Raw   string `json:"raw,omitempty"` // only when the message would not parse
}

// File is an attachment's name and size; its bytes come from .../file/{n}.
type File struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	mid := r.PathValue("mid")
	mb := s.App.Mailbox()
	sum, m, raw, err := mb.Get(mid)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	if !sum.Read {
		mb.MarkRead(mid)
		sum.Read = true
	}
	out := Message{Summary: sum, Files: []File{}}
	if m == nil {
		out.Raw = string(raw)
	} else {
		out.Body, out.Mbo, out.Type = m.Body, m.Mbo, m.Type
		for _, f := range m.Files {
			out.Files = append(out.Files, File{Name: f.Name, Size: len(f.Data)})
		}
	}
	localweb.WriteJSON(w, http.StatusOK, out)
}

// file sends an attachment as a download, never to be shown in the page.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	_, m, _, err := s.App.Mailbox().Get(r.PathValue("mid"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if m == nil || err != nil || n < 0 || n >= len(m.Files) {
		fail(w, http.StatusNotFound, errors.New("no such attachment"))
		return
	}
	f := m.Files[n]
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeName(f.Name)}))
	h.Set("Content-Length", strconv.Itoa(len(f.Data)))
	w.Write(f.Data)
}

// safeName keeps an attachment's name to its last path element, without
// control characters, for the download.
func safeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return "attachment"
	}
	return name
}

// draftLimit is the largest compose request: the message limit, as base64,
// with room for the JSON around it.
const draftLimit = winlinkapp.MaxMessage*4/3 + 64<<10

func (s *Server) estimate(w http.ResponseWriter, r *http.Request) {
	var d winlinkapp.Draft
	if !readJSON(w, r, draftLimit, &d) {
		return
	}
	e, err := s.App.Estimate(d)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	localweb.WriteJSON(w, http.StatusOK, e)
}

func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	var d winlinkapp.Draft
	if !readJSON(w, r, draftLimit, &d) {
		return
	}
	sum, err := s.App.Queue(d)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, winlink.ErrInvalid) {
			status = http.StatusUnprocessableEntity
		}
		fail(w, status, err)
		return
	}
	localweb.WriteJSON(w, http.StatusOK, sum)
}

type midRequest struct {
	MID string `json:"mid"`
	To  string `json:"to,omitempty"`
}

func mailboxStatus(err error) int {
	switch {
	case errors.Is(err, winlinkapp.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, winlinkapp.ErrNotAllowed):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func (s *Server) move(w http.ResponseWriter, r *http.Request) {
	var req midRequest
	if !readJSON(w, r, 1024, &req) {
		return
	}
	if err := s.App.Mailbox().Move(req.MID, req.To); err != nil {
		fail(w, mailboxStatus(err), err)
		return
	}
	localweb.WriteJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	var req midRequest
	if !readJSON(w, r, 1024, &req) {
		return
	}
	if s.App.Session().State == "connecting" || s.App.Session().State == "exchanging" {
		fail(w, http.StatusConflict, errors.New("wait for the session to end"))
		return
	}
	if err := s.App.Mailbox().Delete(req.MID); err != nil {
		fail(w, mailboxStatus(err), err)
		return
	}
	localweb.WriteJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Route string `json:"route"`
	}
	if !readJSON(w, r, 1024, &req) {
		return
	}
	if err := s.App.Connect(req.Route); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, winlinkapp.ErrBusy) {
			status = http.StatusConflict
		}
		fail(w, status, err)
		return
	}
	localweb.WriteJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	go s.App.Stop()
	localweb.WriteJSON(w, http.StatusOK, struct{}{})
}
