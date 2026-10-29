package demo

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APRSFI returns an http.RoundTripper that answers aprs.fi location queries
// from the scenario, so --demo exercises the real aprsfi client without the
// network. It knows the demo stations (reported five minutes ago at their
// last position) and two stations heard only on aprs.fi.
func (s *Scenario) APRSFI() http.RoundTripper { return fakeAPRSFI{s} }

type fakeAPRSFI struct{ s *Scenario }

// remote are stations only the fake aprs.fi knows: north and east of the
// centre in km, and how long ago they reported.
var remote = map[string]struct {
	north, east float64
	age         time.Duration
	symbol      string
	comment     string
}{
	"Q9REMOTE": {150, 90, 2 * time.Hour, "/-", "Heard only on aprs.fi"},
	"Q8ALPS-9": {-120, 200, 26 * time.Hour, "/>", "Last seen yesterday"},
}

func (f fakeAPRSFI) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.URL.Query()
	type entry struct {
		Class    string `json:"class"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Time     string `json:"time"`
		LastTime string `json:"lasttime"`
		Lat      string `json:"lat"`
		Lng      string `json:"lng"`
		Symbol   string `json:"symbol"`
		SrcCall  string `json:"srccall"`
		Comment  string `json:"comment,omitempty"`
	}
	reply := map[string]any{"command": "get", "result": "ok", "what": "loc"}
	if q.Get("what") != "loc" || q.Get("apikey") == "" {
		reply = map[string]any{"command": "get", "result": "fail", "description": "demo: unsupported query"}
	} else {
		var entries []entry
		now := time.Now()
		for _, name := range strings.Split(q.Get("name"), ",") {
			name = strings.ToUpper(strings.TrimSpace(name))
			var lat, lon float64
			var age time.Duration
			symbol, comment := "/-", "Demo station, as aprs.fi saw it"
			if rs, ok := remote[name]; ok {
				lat, lon = f.s.offset(rs.north, rs.east)
				age, symbol, comment = rs.age, rs.symbol, rs.comment
			} else if la, lo, ok := f.s.LastPosition(name); ok {
				lat, lon, age = la, lo, 5*time.Minute
			} else {
				continue
			}
			t := strconv.FormatInt(now.Add(-age).Unix(), 10)
			entries = append(entries, entry{
				Class: "a", Name: name, Type: "l", Time: t, LastTime: t,
				Lat: strconv.FormatFloat(lat, 'f', 5, 64), Lng: strconv.FormatFloat(lon, 'f', 5, 64),
				Symbol: symbol, SrcCall: name, Comment: comment,
			})
		}
		reply["found"] = len(entries)
		reply["entries"] = entries
	}
	b, _ := json.Marshal(reply)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Request:    r,
	}, nil
}
