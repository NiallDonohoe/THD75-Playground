package aprsapp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"d75scope/aprs"
	"d75scope/aprsfi"
)

var t0 = time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)

func arrival(t *testing.T, src Source, line string, at time.Time) Arrival {
	t.Helper()
	p, err := aprs.ParseTNC2(line)
	if err != nil {
		t.Fatal(err)
	}
	return Arrival{Source: src, Packet: p, At: at}
}

func TestMergeRFAndIS(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005,WIDE1-1:=5321.00N/00616.00W-Home", t0))
	s.Ingest(arrival(t, SourceIS, "Q1HOME>APK005,WIDE1-1,qAR,Q3GATE:=5321.00N/00616.00W-Home", t0.Add(2*time.Second)))

	snap := s.Snapshot()
	if len(snap.Packets) != 1 {
		t.Fatalf("%d packets, want 1 merged", len(snap.Packets))
	}
	p := snap.Packets[0]
	if !slices.Equal(p.Sources, []Source{SourceRF, SourceIS}) {
		t.Errorf("sources %v", p.Sources)
	}
	if p.RF == nil || !p.RF.Direct || p.IS == nil || p.IS.Q != "qAR" || p.IS.Via != "Q3GATE" || !p.IS.FromRF {
		t.Errorf("origins rf=%+v is=%+v", p.RF, p.IS)
	}
	st, _ := s.Station("Q1HOME")
	if st.Pos == nil || !slices.Equal(st.Pos.Sources, []Source{SourceRF, SourceIS}) || st.LastRF == nil || st.LastIS == nil {
		t.Errorf("station %+v pos %+v", st, st.Pos)
	}
	if st.Packets != 1 || st.Comment != "Home" || st.Symbol != "/-" {
		t.Errorf("station %+v", st)
	}
}

func TestDuplicatesAndWindow(t *testing.T) {
	s := NewStore("EI2ABC-7")
	line := "Q5MOB-9>APK005,Q2DIGI*,WIDE2-1:>On the road"
	s.Ingest(arrival(t, SourceRF, line, t0))
	// The same packet through another digipeater: a duplicate, not new.
	s.Ingest(arrival(t, SourceRF, "Q5MOB-9>APK005,Q4DIGI*,WIDE2-1:>On the road", t0.Add(3*time.Second)))
	// After the window it is a new packet.
	s.Ingest(arrival(t, SourceRF, line, t0.Add(DupWindow+time.Second)))
	snap := s.Snapshot()
	if len(snap.Packets) != 2 || snap.Packets[0].Dupes != 1 {
		t.Fatalf("packets %+v", snap.Packets)
	}
	if got := snap.Packets[0].RF.Digis; !slices.Equal(got, []string{"Q2DIGI"}) {
		t.Errorf("digis %v", got)
	}
	st, _ := s.Station("Q5MOB-9")
	if st.Status != "On the road" || st.Packets != 2 {
		t.Errorf("station %+v", st)
	}
}

func TestThirdPartyMergesWithIS(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceIS, "Q8FAR>APRS,TCPIP*,qAC,T2DEMO:=5340.00N/00557.00W-Net", t0))
	s.Ingest(arrival(t, SourceRF, "Q3GATE>APDW16,WIDE1-1:}Q8FAR>APRS,TCPIP,Q3GATE*:=5340.00N/00557.00W-Net", t0.Add(2*time.Second)))
	snap := s.Snapshot()
	if len(snap.Packets) != 1 {
		t.Fatalf("%d packets, want 1", len(snap.Packets))
	}
	p := snap.Packets[0]
	if p.Src != "Q8FAR" || !slices.Equal(p.Sources, []Source{SourceIS, SourceRF}) || p.RF == nil || p.RF.Gateway != "Q3GATE" || p.IS.FromRF {
		t.Errorf("packet %+v rf=%+v is=%+v", p, p.RF, p.IS)
	}
	// The gateway was heard on RF too.
	if gw, ok := s.Station("Q3GATE"); !ok || gw.LastRF == nil {
		t.Errorf("gateway %+v", gw)
	}
	if st, _ := s.Station("Q8FAR"); st.LastRF == nil || st.LastIS == nil {
		t.Errorf("Q8FAR heard %+v", st)
	}
}

func TestObjectsAndItems(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005:;NETCTRL  *291500z5321.00N\\00615.00Wn Club net", t0))
	s.Ingest(arrival(t, SourceRF, "Q7HIKE>APK005:)WATER!5321.40N/00616.60Ww", t0))
	snap := s.Snapshot()
	if snap.Packets[0].Station != ";NETCTRL" || snap.Packets[1].Station != ";WATER" {
		t.Errorf("packet stations %q %q", snap.Packets[0].Station, snap.Packets[1].Station)
	}
	obj, ok := s.Station(";NETCTRL")
	if !ok || obj.Kind != "object" || obj.Owner != "Q1HOME" || obj.Name != "NETCTRL" || obj.Pos == nil || obj.Comment != " Club net" || obj.LastRF == nil {
		t.Errorf("object %+v", obj)
	}
	if item, _ := s.Station(";WATER"); item.Kind != "item" || item.Owner != "Q7HIKE" {
		t.Errorf("item %+v", item)
	}
	if owner, _ := s.Station("Q1HOME"); owner.Pos != nil || owner.Packets != 1 {
		t.Errorf("owner %+v", owner)
	}
}

func TestMessages(t *testing.T) {
	s := NewStore("ei2abc-7")
	s.Ingest(arrival(t, SourceRF, "Q5MOB-9>APK005::EI2ABC-7 :Hello{12", t0))
	s.Ingest(arrival(t, SourceIS, "Q5MOB-9>APK005,qAR,Q3GATE::EI2ABC-7 :Hello{12", t0.Add(time.Second)))
	s.Ingest(arrival(t, SourceIS, "Q8FAR>APRS,TCPIP*,qAC,T2DEMO::EI2ABC-7 :ack3", t0))
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005::BLN1     :Net tonight", t0))
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005::Q5MOB-9  :Not for us{4", t0))
	msgs := s.Snapshot().Messages
	if len(msgs) != 3 {
		t.Fatalf("%d messages: %+v", len(msgs), msgs)
	}
	m := msgs[0]
	if m.Kind != "message" || m.From != "Q5MOB-9" || m.Text != "Hello" || m.MsgID != "12" || !slices.Equal(m.Sources, []Source{SourceRF, SourceIS}) {
		t.Errorf("message %+v", m)
	}
	if msgs[1].Kind != "ack" || msgs[1].MsgID != "3" || msgs[2].Bulletin != true {
		t.Errorf("ack and bulletin %+v %+v", msgs[1], msgs[2])
	}
}

func TestMalformed(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q2DIGI>APRS:!4903.5", t0))
	p := s.Snapshot().Packets[0]
	if p.Kind != "malformed" || !strings.HasPrefix(p.Summary, "Malformed: position") {
		t.Errorf("packet %+v", p)
	}
	if st, ok := s.Station("Q2DIGI"); !ok || st.Pos != nil || st.Packets != 1 {
		t.Errorf("station %+v", st)
	}
}

func TestSubscribe(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005:>first", t0))
	snap, ch, cancel := s.Subscribe()
	defer cancel()
	if len(snap.Packets) != 1 || len(snap.Stations) != 1 {
		t.Fatalf("snapshot %+v", snap)
	}
	s.Ingest(arrival(t, SourceIS, "Q8FAR>APRS,TCPIP*,qAC,T2DEMO:>second", t0))
	var types []string
	for len(ch) > 0 {
		ev := <-ch
		types = append(types, ev.Type)
		if !json.Valid(ev.Data) {
			t.Errorf("event %s is not JSON: %s", ev.Type, ev.Data)
		}
	}
	if !slices.Contains(types, "packet") || !slices.Contains(types, "station") {
		t.Errorf("events %v", types)
	}

	// A subscriber that stops reading is dropped, never waited for.
	for i := range subQueue + 10 {
		s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005:>status "+strings.Repeat("x", i%40)+string(rune('a'+i%26)), t0.Add(time.Duration(i)*time.Minute)))
	}
	n := 0
	for range ch {
		n++
	}
	if n > subQueue {
		t.Errorf("read %d events after the queue filled", n)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005:=5321.00N/00616.00W-", t0))
	snap := s.Snapshot()
	snap.Stations[0].Pos.Sources[0] = SourceAPRSFI
	snap.Packets[0].Sources[0] = SourceAPRSFI
	if st, _ := s.Station("Q1HOME"); st.Pos.Sources[0] != SourceRF {
		t.Errorf("snapshot shares the station's sources")
	}
	if s.Snapshot().Packets[0].Sources[0] != SourceRF {
		t.Errorf("snapshot shares the packet's sources")
	}
}

func TestAddLookup(t *testing.T) {
	s := NewStore("EI2ABC-7")
	s.Ingest(arrival(t, SourceRF, "Q1HOME>APK005:=5321.00N/00616.00W-Home", t0))
	keys := s.AddLookup([]aprsfi.Entry{
		// Older than what RF gave: kept as "seen on aprs.fi", not as the position.
		{Name: "Q1HOME", Type: "l", LastTime: t0.Add(-time.Hour), Lat: 1, Lng: 1, Symbol: "/-"},
		{Name: "Q9REMOTE", Type: "l", LastTime: t0.Add(-2 * time.Hour), Lat: 54.7, Lng: -4.9, Symbol: "/>", HasCourse: true, Course: 90, HasSpeed: true, Speed: 18.52, Comment: "far"},
		{Name: "NET", Type: "o", SrcCall: "Q1HOME", LastTime: t0, Lat: 53, Lng: -6},
	})
	if !slices.Equal(keys, []string{"Q1HOME", "Q9REMOTE", ";NET"}) {
		t.Errorf("keys %v", keys)
	}
	home, _ := s.Station("Q1HOME")
	if home.Pos.Lat == 1 || home.LastAPRSFI == nil || !slices.Equal(home.Pos.Sources, []Source{SourceRF}) {
		t.Errorf("an older aprs.fi report replaced the RF position: %+v", home.Pos)
	}
	far, _ := s.Station("Q9REMOTE")
	if far.Pos == nil || !slices.Equal(far.Pos.Sources, []Source{SourceAPRSFI}) || far.Pos.Speed != 10 || far.LastRF != nil || far.Comment != "far" {
		t.Errorf("remote %+v pos %+v", far, far.Pos)
	}
	if obj, _ := s.Station(";NET"); obj.Kind != "object" || obj.Owner != "Q1HOME" {
		t.Errorf("object %+v", obj)
	}
	if keys := s.AddLookup(nil); keys == nil || len(keys) != 0 {
		t.Errorf("no entries: %v", keys)
	}
}

func TestSummaries(t *testing.T) {
	for line, want := range map[string]string{
		"A>APRS:=5321.00N/00616.00W>090/030/A=000328":  "Position 53.3500, -6.2667 · 30 kn at 90° · 100 m",
		"A>APRS::EI2ABC-7 :ack12":                      "Ack to EI2ABC-7 for #12",
		"A>APRS::BLN1     :Net":                        "Bulletin to BLN1: Net",
		"A>APRS:T#001,1,2,3":                           "Telemetry",
		"A>APRS:_10090556c220s004g005t077":             "Weather report",
		"A>APRS:;OBJ      _291500z5321.00N/00616.00W-": "Object OBJ deleted",
	} {
		p, _ := aprs.ParseTNC2(line)
		d, err := aprs.Decode(p)
		if got := summary(d, err); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
}
