package demo

import (
	"context"
	"math"
	"net/http"
	"slices"
	"testing"
	"time"

	"d75scope/aprs"
	"d75scope/aprsapp"
	"d75scope/aprsfi"
)

func TestMicEEncoderRoundTrips(t *testing.T) {
	for _, c := range []struct {
		lat, lon      float64
		speed, course int
		msg           int
	}{
		{53.3418, -6.26, 36, 251, 6},
		{-33.8533, 151.2067, 0, 90, 0},
		{0.5, 3.25, 5, 1, 7},
		{41.71, -72.73, 120, 360, 1},
		{12.0, 105.5, 9, 45, 3},
		{-1.0, -179.99, 199, 180, 4},
	} {
		dst, info := micE(c.lat, c.lon, c.speed, c.course, '/', '>', c.msg, ">", 61, true)
		d, err := aprs.Decode(aprs.Packet{Src: "Q5MOB", Dst: dst, Info: info + "comment"})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		p := d.Position
		if math.Abs(p.Lat-c.lat) > 1e-4 || math.Abs(p.Lon-c.lon) > 1e-4 || int(p.Speed) != c.speed || p.Course != c.course || p.Altitude != 61 || d.Comment != "comment" {
			t.Errorf("%+v: decoded %+v, comment %q", c, *p, d.Comment)
		}
		names := []string{"Emergency", "Priority", "Special", "Committed", "Returning", "In Service", "En Route", "Off Duty"}
		if d.MicE != names[c.msg] {
			t.Errorf("%+v: message %q", c, d.MicE)
		}
	}
}

func TestCompressedEncoderRoundTrips(t *testing.T) {
	info := compressed(53.871, -6.857, '/', 'k', 88, 40)
	d, err := aprs.Decode(aprs.Packet{Src: "Q9FAR", Dst: "APRS", Info: info})
	if err != nil {
		t.Fatal(err)
	}
	p := d.Position
	if math.Abs(p.Lat-53.871) > 1e-5 || math.Abs(p.Lon+6.857) > 1e-5 || p.Course != 88 || math.Abs(p.Speed-40) > 2 {
		t.Errorf("decoded %+v", *p)
	}
}

// TestScenarioCoversEveryCase runs the demo for 15 simulated minutes and
// checks that the page will see every kind of source tag.
func TestScenarioCoversEveryCase(t *testing.T) {
	s := New("EI2ABC-7", 53.35, -6.26)
	store := aprsapp.NewStore("EI2ABC-7")
	start := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	for i := range 900 {
		for _, a := range s.Step(start.Add(time.Duration(i) * time.Second)) {
			store.Ingest(a)
		}
	}
	snap := store.Snapshot()
	has := func(name string, ok func(p aprsapp.PacketRecord) bool) {
		t.Helper()
		if !slices.ContainsFunc(snap.Packets, ok) {
			t.Errorf("no packet: %s", name)
		}
	}
	both := []aprsapp.Source{aprsapp.SourceRF, aprsapp.SourceIS}
	has("RF heard direct, igated (RF+IP)", func(p aprsapp.PacketRecord) bool {
		return slices.Equal(p.Sources, both) && p.RF.Direct && p.IS.FromRF
	})
	has("RF via a digipeater", func(p aprsapp.PacketRecord) bool { return p.RF != nil && len(p.RF.Digis) > 0 })
	has("RF only", func(p aprsapp.PacketRecord) bool { return slices.Equal(p.Sources, []aprsapp.Source{aprsapp.SourceRF}) })
	has("IP from an internet client", func(p aprsapp.PacketRecord) bool {
		return slices.Equal(p.Sources, []aprsapp.Source{aprsapp.SourceIS}) && p.IS.Q == "qAC"
	})
	has("IP igated far away", func(p aprsapp.PacketRecord) bool {
		return slices.Equal(p.Sources, []aprsapp.Source{aprsapp.SourceIS}) && p.IS.FromRF
	})
	has("third party on RF, merged with IP", func(p aprsapp.PacketRecord) bool { return p.RF != nil && p.RF.Gateway != "" && p.IS != nil })
	has("Mic-E", func(p aprsapp.PacketRecord) bool { return p.Src == "Q5MOB-9" && p.Kind == "position" })
	has("object", func(p aprsapp.PacketRecord) bool { return p.Kind == "object" })
	has("item", func(p aprsapp.PacketRecord) bool { return p.Kind == "item" })
	has("telemetry", func(p aprsapp.PacketRecord) bool { return p.Summary == "Telemetry" })
	has("malformed", func(p aprsapp.PacketRecord) bool { return p.Kind == "malformed" })

	var rfMsg, isMsg, bulletin bool
	for _, m := range snap.Messages {
		rfMsg = rfMsg || (m.Kind == "message" && slices.Contains(m.Sources, aprsapp.SourceRF))
		isMsg = isMsg || (m.Kind == "message" && slices.Equal(m.Sources, []aprsapp.Source{aprsapp.SourceIS}))
		bulletin = bulletin || m.Bulletin
	}
	if !rfMsg || !isMsg || !bulletin {
		t.Errorf("messages: RF %v, IP only %v, bulletin %v", rfMsg, isMsg, bulletin)
	}
	for _, st := range snap.Stations {
		if st.Name[0] != 'Q' && st.Kind == "station" {
			t.Errorf("station %q does not start with Q", st.Name)
		}
	}
}

func TestFakeAPRSFI(t *testing.T) {
	s := New("EI2ABC-7", 53.35, -6.26)
	s.Step(time.Now()) // places Q1HOME
	c := &aprsfi.Client{Key: "demo", UserAgent: "test", HTTP: &http.Client{Transport: s.APRSFI()}}
	entries, err := c.Locate(context.Background(), "Q9REMOTE", "Q1HOME", "NOBODY")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "Q9REMOTE" || entries[1].Name != "Q1HOME" {
		t.Fatalf("entries %+v", entries)
	}
	if age := time.Since(entries[0].LastTime); age < 119*time.Minute || age > 121*time.Minute {
		t.Errorf("Q9REMOTE age %v", age)
	}
}
