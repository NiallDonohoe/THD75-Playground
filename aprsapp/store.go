// Package aprsapp is d75aprs's engine: the store of packets, stations and
// messages, with the source tags (RF, IP, aprs.fi) the page shows, and the
// event stream the page follows.
package aprsapp

import (
	"slices"
	"strings"
	"sync"
	"time"

	"d75scope/aprs"
)

// Source is how this program received a packet or a position.
type Source string

const (
	SourceRF     Source = "rf"     // KISS frames from the radio's TNC
	SourceIS     Source = "is"     // APRS-IS
	SourceAPRSFI Source = "aprsfi" // an aprs.fi lookup
)

// Arrival is one packet as a source delivered it.
type Arrival struct {
	Source Source
	Packet aprs.Packet
	At     time.Time
}

// DupWindow is how long a packet counts as the same packet when it arrives
// again, by another source or another digipeater. APRS-IS uses 30 s too.
const DupWindow = 30 * time.Second

// Limits on what the store keeps in memory.
const (
	maxPackets  = 2000
	maxMessages = 500
	maxTrack    = 100
)

// Store holds what the program has heard. It is safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	myCall   string
	nextID   int64
	packets  []*PacketRecord // oldest first
	recent   map[string]*PacketRecord
	stations map[string]*Station
	messages []*MessageRecord
	status   map[string]SourceStatus
	subs     map[chan Event]struct{}
}

// NewStore returns an empty store for the station myCall, whose messages it
// collects.
func NewStore(myCall string) *Store {
	return &Store{
		myCall:   strings.ToUpper(myCall),
		recent:   map[string]*PacketRecord{},
		stations: map[string]*Station{},
		status:   map[string]SourceStatus{},
		subs:     map[chan Event]struct{}{},
	}
}

// PacketRecord is one packet, however many sources delivered it.
type PacketRecord struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"` // first arrival
	Sources []Source  `json:"sources"`
	Raw     string    `json:"raw"`     // TNC2 text as first received
	Src     string    `json:"src"`     // the station that sent it (inside a third-party packet, the original sender)
	Station string    `json:"station"` // key of the station, object or item it is about
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	RF      *RFOrigin `json:"rf,omitempty"`
	IS      *ISOrigin `json:"is,omitempty"`
	Dupes   int       `json:"dupes,omitempty"` // further copies within DupWindow, from a source already counted

	key string
}

// RFOrigin describes how a packet reached us on RF.
type RFOrigin struct {
	Direct bool     `json:"direct"`          // no digipeater had repeated it
	Digis  []string `json:"digis,omitempty"` // digipeaters used, in order
	// Gateway is set for a third-party packet: the station that relayed it
	// from APRS-IS to RF.
	Gateway string `json:"gateway,omitempty"`
}

// ISOrigin describes how a packet reached APRS-IS.
type ISOrigin struct {
	Q      string `json:"q,omitempty"`      // the q construct code
	Via    string `json:"via,omitempty"`    // the IGate or server named after it
	FromRF bool   `json:"fromRF,omitempty"` // an IGate heard it on RF
}

// Station is a station, object or item.
type Station struct {
	Key     string       `json:"key"`
	Name    string       `json:"name"`
	Kind    string       `json:"kind"` // "station", "object" or "item"
	Owner   string       `json:"owner,omitempty"`
	Symbol  string       `json:"symbol,omitempty"` // table and code
	Pos     *StationPos  `json:"pos,omitempty"`
	Track   [][2]float64 `json:"track,omitempty"` // recent positions, oldest first
	Comment string       `json:"comment,omitempty"`
	Status  string       `json:"status,omitempty"`
	MicE    string       `json:"micE,omitempty"`
	Killed  bool         `json:"killed,omitempty"`

	LastHeard  time.Time  `json:"lastHeard"`
	LastRF     *time.Time `json:"lastRF,omitempty"`
	LastIS     *time.Time `json:"lastIS,omitempty"`
	LastAPRSFI *time.Time `json:"lastAPRSFI,omitempty"`
	Packets    int        `json:"packets"`

	posPacket int64 // the packet the position came from
}

// StationPos is a station's latest position and where it came from.
type StationPos struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Ambiguity int       `json:"ambiguity,omitempty"`
	HasCourse bool      `json:"hasCourse,omitempty"`
	Course    int       `json:"course,omitempty"`
	Speed     float64   `json:"speed,omitempty"` // knots
	HasAlt    bool      `json:"hasAlt,omitempty"`
	Alt       float64   `json:"alt,omitempty"` // metres
	At        time.Time `json:"at"`            // when it was received, or reported (aprs.fi)
	Sources   []Source  `json:"sources"`
}

// MessageRecord is a message, ack or reject to our callsign, or a bulletin.
// Its ID is the ID of the packet that carried it.
type MessageRecord struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Sources  []Source  `json:"sources"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Kind     string    `json:"kind"` // "message", "ack" or "rej"
	Text     string    `json:"text,omitempty"`
	MsgID    string    `json:"msgId,omitempty"`
	Bulletin bool      `json:"bulletin,omitempty"`
}

// SourceStatus is the state of one source, shown in the page's status bar.
type SourceStatus struct {
	State  string `json:"state"`  // "off", "demo", "connected", "disconnected", "on request"
	Detail string `json:"detail"` // free text, such as the server name
}

// Ingest adds an arrival. A packet already seen within DupWindow (same
// sender, destination and information field) is merged into the first
// record, which then carries both sources.
func (s *Store) Ingest(a Arrival) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := a.Packet
	d, err := aprs.Decode(p)
	var rf *RFOrigin
	var is *ISOrigin
	switch a.Source {
	case SourceRF:
		rf = &RFOrigin{Digis: p.Digipeaters()}
		rf.Direct = len(rf.Digis) == 0
	case SourceIS:
		is = &ISOrigin{}
		if q, ok := p.Q(); ok {
			is.Q, is.Via, is.FromRF = q.Code, q.Via, q.FromRF()
		}
	}
	// A third-party packet is about the station inside it. The gateway that
	// relayed it was heard too.
	if err == nil && d.Kind == aprs.KindThirdParty {
		if rf != nil {
			rf.Gateway = p.Src
		}
		s.heard(p.Src, a.Source, a.At, true)
		p = *d.Inner
		d, err = aprs.Decode(p)
	}

	s.expire(a.At)
	key := dupKey(p)
	if rec, ok := s.recent[key]; ok {
		if slices.Contains(rec.Sources, a.Source) {
			rec.Dupes++
			s.publish("packet", rec)
			return
		}
		rec.Sources = append(rec.Sources, a.Source)
		if rec.RF == nil {
			rec.RF = rf
		}
		if rec.IS == nil {
			rec.IS = is
		}
		s.publish("packet", rec)
		for _, st := range s.stations {
			if st.posPacket == rec.ID {
				st.Pos.Sources = slices.Clone(rec.Sources)
			}
		}
		s.heard(rec.Src, a.Source, a.At, false)
		if st, ok := s.stations[rec.Station]; ok && rec.Station != strings.ToUpper(rec.Src) {
			s.touch(st, a.Source, a.At)
			s.publish("station", st)
		}
		for _, m := range s.messages {
			if m.ID == rec.ID {
				m.Sources = slices.Clone(rec.Sources)
				s.publish("message", m)
			}
		}
		return
	}

	s.nextID++
	rec := &PacketRecord{
		ID: s.nextID, At: a.At, Sources: []Source{a.Source}, Raw: a.Packet.String(),
		Src: p.Src, Station: p.Src, Kind: d.Kind.String(), Summary: summary(d, err),
		RF: rf, IS: is, key: key,
	}
	if err != nil {
		rec.Kind = "malformed"
	}
	s.recent[key] = rec
	s.packets = append(s.packets, rec)
	if len(s.packets) > maxPackets {
		s.packets = s.packets[len(s.packets)-maxPackets:]
	}

	sender := s.heard(p.Src, a.Source, a.At, true)
	if err == nil {
		s.apply(rec, sender, d, a.At)
	}
	s.publish("packet", rec)
}

// apply updates the stations and messages from a decoded packet.
func (s *Store) apply(rec *PacketRecord, sender *Station, d aprs.Data, at time.Time) {
	switch d.Kind {
	case aprs.KindPosition, aprs.KindObject, aprs.KindItem:
		st := sender
		if d.Kind != aprs.KindPosition {
			kind := "object"
			if d.Kind == aprs.KindItem {
				kind = "item"
			}
			st = s.station(objectKey(d.Name), d.Name, kind)
			st.Owner = rec.Src
			st.Killed = d.Killed
			s.touch(st, rec.Sources[0], at)
			rec.Station = st.Key
		}
		s.setPos(st, d.Position, at, rec.Sources, rec.ID)
		st.Comment = d.Comment
		st.MicE = d.MicE
		s.publish("station", st)
	case aprs.KindStatus:
		sender.Status = d.Status
		s.publish("station", sender)
	case aprs.KindMessage:
		m := d.Message
		if !strings.EqualFold(m.To, s.myCall) && !m.IsBulletin() {
			return
		}
		mr := &MessageRecord{
			ID: rec.ID, At: at, Sources: slices.Clone(rec.Sources), From: rec.Src, To: m.To,
			Kind: [...]string{"message", "ack", "rej"}[m.Kind], Text: m.Text, MsgID: m.ID, Bulletin: m.IsBulletin(),
		}
		s.messages = append(s.messages, mr)
		if len(s.messages) > maxMessages {
			s.messages = s.messages[len(s.messages)-maxMessages:]
		}
		s.publish("message", mr)
	}
}

func (s *Store) setPos(st *Station, p *aprs.Position, at time.Time, sources []Source, packet int64) {
	st.Pos = &StationPos{
		Lat: p.Lat, Lon: p.Lon, Ambiguity: p.Ambiguity,
		HasCourse: p.HasCourseSpeed, Course: p.Course, Speed: p.Speed,
		HasAlt: p.HasAltitude, Alt: p.Altitude,
		At: at, Sources: slices.Clone(sources),
	}
	st.Symbol = string([]byte{p.Symbol.Table, p.Symbol.Code})
	st.posPacket = packet
	pt := [2]float64{p.Lat, p.Lon}
	if n := len(st.Track); n == 0 || st.Track[n-1] != pt {
		st.Track = append(st.Track, pt)
		if len(st.Track) > maxTrack {
			st.Track = st.Track[len(st.Track)-maxTrack:]
		}
	}
}

// heard records that a station sent a packet through a source, and
// publishes the change unless the caller will.
func (s *Store) heard(call string, src Source, at time.Time, counted bool) *Station {
	st := s.station(strings.ToUpper(call), call, "station")
	s.touch(st, src, at)
	if counted {
		st.Packets++
	}
	s.publish("station", st)
	return st
}

func (s *Store) touch(st *Station, src Source, at time.Time) {
	if at.After(st.LastHeard) {
		st.LastHeard = at
	}
	t := at
	switch src {
	case SourceRF:
		st.LastRF = &t
	case SourceIS:
		st.LastIS = &t
	case SourceAPRSFI:
		st.LastAPRSFI = &t
	}
}

func (s *Store) station(key, name, kind string) *Station {
	st, ok := s.stations[key]
	if !ok {
		st = &Station{Key: key, Name: name, Kind: kind}
		s.stations[key] = st
	}
	return st
}

// Objects and items have their own namespace: their names are not callsigns.
func objectKey(name string) string { return ";" + name }

// dupKey identifies a packet for duplicate detection: sender, destination
// (without SSID, which digipeating paths may change) and information field.
func dupKey(p aprs.Packet) string {
	dst, _, _ := strings.Cut(p.Dst, "-")
	return strings.ToUpper(p.Src) + ">" + dst + ":" + p.Info
}

func (s *Store) expire(now time.Time) {
	for k, rec := range s.recent {
		if now.Sub(rec.At) > DupWindow {
			delete(s.recent, k)
		}
	}
}

// SetStatus records the state of a source ("rf", "is" or "aprsfi").
func (s *Store) SetStatus(source string, st SourceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[source] = st
	s.publish("status", map[string]SourceStatus{source: st})
}

// Station returns a copy of the station with the given key.
func (s *Store) Station(key string) (Station, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.stations[key]
	if !ok {
		return Station{}, false
	}
	return copyStation(st), true
}

func copyStation(st *Station) Station {
	c := *st
	c.Track = slices.Clone(st.Track)
	if st.Pos != nil {
		p := *st.Pos
		p.Sources = slices.Clone(p.Sources)
		c.Pos = &p
	}
	return c
}
