package aprsapp

import (
	"encoding/json"
	"sort"
)

// Event is one change, already encoded, for the page's event stream.
type Event struct {
	Type string          // "packet", "station", "message" or "status"
	Data json.RawMessage // the record as JSON
}

// subQueue is how many events a subscriber may fall behind before it is
// dropped. A dropped subscriber's channel is closed; the page reconnects and
// starts again from a snapshot, so a slow page never holds up the store.
const subQueue = 512

// publish encodes v under the store's lock, so subscribers never see a
// record while it changes. The caller holds s.mu.
func (s *Store) publish(typ string, v any) {
	if len(s.subs) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := Event{Type: typ, Data: b}
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

// Snapshot is everything the page needs to draw itself from scratch.
type Snapshot struct {
	Stations []Station               `json:"stations"`
	Packets  []PacketRecord          `json:"packets"`
	Messages []MessageRecord         `json:"messages"`
	Status   map[string]SourceStatus `json:"status"`
}

// snapshotPackets is how many recent packets a snapshot carries.
const snapshotPackets = 500

// Subscribe returns a snapshot and a channel of the events after it. The
// channel is closed if the subscriber falls too far behind; cancel ends the
// subscription.
func (s *Store) Subscribe() (Snapshot, <-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.snapshot()
	ch := make(chan Event, subQueue)
	s.subs[ch] = struct{}{}
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
	return snap, ch, cancel
}

// Snapshot returns the current state.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

func (s *Store) snapshot() Snapshot {
	snap := Snapshot{
		Stations: make([]Station, 0, len(s.stations)),
		Packets:  make([]PacketRecord, 0, min(len(s.packets), snapshotPackets)),
		Messages: make([]MessageRecord, 0, len(s.messages)),
		Status:   map[string]SourceStatus{},
	}
	for _, st := range s.stations {
		snap.Stations = append(snap.Stations, copyStation(st))
	}
	sort.Slice(snap.Stations, func(i, j int) bool { return snap.Stations[i].Key < snap.Stations[j].Key })
	for _, p := range s.packets[max(0, len(s.packets)-snapshotPackets):] {
		c := *p
		c.Sources = append([]Source(nil), p.Sources...)
		snap.Packets = append(snap.Packets, c)
	}
	for _, m := range s.messages {
		c := *m
		c.Sources = append([]Source(nil), m.Sources...)
		snap.Messages = append(snap.Messages, c)
	}
	for k, v := range s.status {
		snap.Status[k] = v
	}
	return snap
}
