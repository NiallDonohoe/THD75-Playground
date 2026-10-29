package aprsapp

import (
	"strings"

	"d75scope/aprsfi"
)

// AddLookup records aprs.fi lookup results. A position from aprs.fi is
// kept only if the station has none, or an older one, so a fresh RF or IP
// position is never replaced by an older report from the web. It returns
// the keys of the stations the entries belong to.
func (s *Store) AddLookup(entries []aprsfi.Entry) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{}
	for _, e := range entries {
		var st *Station
		switch e.Type {
		case "o", "i":
			kind := "object"
			if e.Type == "i" {
				kind = "item"
			}
			st = s.station(objectKey(e.Name), e.Name, kind)
			st.Owner = e.SrcCall
		default:
			st = s.station(strings.ToUpper(e.Name), e.Name, "station")
		}
		at := e.LastTime
		t := at
		st.LastAPRSFI = &t
		if st.Pos == nil || st.Pos.At.Before(at) {
			st.Pos = &StationPos{
				Lat: e.Lat, Lon: e.Lng,
				HasCourse: e.HasCourse && e.HasSpeed, Course: int(e.Course), Speed: e.Speed / 1.852,
				HasAlt: e.HasAlt, Alt: e.Altitude,
				At: at, Sources: []Source{SourceAPRSFI},
			}
			st.posPacket = 0
			if len(e.Symbol) == 2 {
				st.Symbol = e.Symbol
			}
			pt := [2]float64{e.Lat, e.Lng}
			if n := len(st.Track); n == 0 || st.Track[n-1] != pt {
				st.Track = append(st.Track, pt)
			}
		}
		if st.Comment == "" {
			st.Comment = e.Comment
		}
		if st.Status == "" {
			st.Status = e.Status
		}
		s.publish("station", st)
		keys = append(keys, st.Key)
	}
	return keys
}
