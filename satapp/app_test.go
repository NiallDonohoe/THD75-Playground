package satapp

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"d75scope/orbit"
	"d75scope/satdata"
	"d75scope/satdemo"
)

type fixed struct{ t time.Time }

func (f *fixed) Now() time.Time { return f.t }

var (
	start  = time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)
	dublin = Location{Lat: 53.35, Lon: -6.26, AltM: 20, Source: "browser"}
)

func demoApp(t *testing.T, cfgPath string) (*App, *fixed) {
	t.Helper()
	clk := &fixed{start}
	a := New(clk, Config{}, cfgPath)
	cat := satdemo.Catalog(orbit.Observer{Lat: dublin.Lat, Lon: dublin.Lon}, start)
	// And one deep-space satellite, which is listed but not predicted.
	deep := cat[0]
	deep.ID, deep.Name = "99999", "HIGH ORBIT"
	deep.Elements.Catalog, deep.Elements.Name, deep.Elements.MeanMotion = "99999", "HIGH ORBIT", 2
	a.SetCatalog(append(cat, deep), map[string]satdata.Status{"celestrak": {Off: true}})
	return a, clk
}

func TestLocationValidatedAndSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	a, _ := demoApp(t, path)
	for _, bad := range []Location{
		{Lat: 91, Source: "map"}, {Lon: 200, Source: "map"}, {Lat: math.NaN(), Source: "map"},
		{AltM: 20000, Source: "map"}, {Lat: 10, Source: "guess"},
	} {
		if err := a.SetLocation(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if err := a.SetLocation(dublin); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAlert("99901", true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAlert("nope", true); err == nil {
		t.Error("alert for an unknown satellite accepted")
	}
	if err := a.SetAlertTiming(0, 10); err == nil {
		t.Error("alert 0 minutes before accepted")
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Location == nil || cfg.Location.Lat != 53.35 || cfg.Location.Source != "browser" || !cfg.Alerts["99901"] {
		t.Errorf("saved config %+v", cfg)
	}
	if cfg.AlertMinutes != DefaultAlertMinutes || cfg.AlertMinEl != DefaultAlertMinEl {
		t.Errorf("defaults %+v", cfg)
	}
}

func TestPassesAndLive(t *testing.T) {
	a, _ := demoApp(t, "")
	snap, ch, cancel := a.Subscribe()
	defer cancel()
	if len(snap.Sats) != 6 || snap.Sats[5].Predicted || snap.Sats[5].Problem == "" {
		t.Fatalf("catalogue %+v", snap.Sats)
	}
	a.computePasses()
	if len(a.Passes()) != 0 {
		t.Error("passes without a location")
	}
	a.SetLocation(dublin)
	a.computePasses()
	ps := a.Passes()
	if len(ps) < 20 {
		t.Fatalf("%d passes in a day for five satellites", len(ps))
	}
	var first *PassInfo
	for i := range ps {
		if ps[i].ID == "99901" {
			first = &ps[i]
			break
		}
	}
	if first == nil || first.InProgress || first.AOS.Sub(start) > 3*time.Minute {
		t.Errorf("DEMOSAT-1's first pass %+v", first)
	}
	if b, _ := json.Marshal(ps[0]); len(b) > 260 {
		t.Errorf("pass JSON is long; rounding? %s", b)
	}
	for i := 1; i < len(ps); i++ {
		if ps[i].AOS.Before(ps[i-1].AOS) {
			t.Fatal("passes not in AOS order")
		}
	}
	a.tick()
	got := map[string]bool{}
	for drain := true; drain; {
		select {
		case ev := <-ch:
			got[ev.Type] = true
			if ev.Type == "tick" {
				var tk struct{ Live []Live }
				json.Unmarshal(ev.Data, &tk)
				if len(tk.Live) != 5 || tk.Live[0].Rng == 0 {
					t.Errorf("tick %+v", tk.Live)
				}
			}
		default:
			drain = false
		}
	}
	for _, typ := range []string{"config", "passes", "tick"} {
		if !got[typ] {
			t.Errorf("no %q event (got %v)", typ, got)
		}
	}
}

func TestTracks(t *testing.T) {
	a, clk := demoApp(t, "")
	a.SetLocation(dublin)
	tr, err := a.GroundTrack("99901")
	if err != nil {
		t.Fatal(err)
	}
	// About 93 minutes each way at 30 s steps.
	if len(tr.Past) < 180 || len(tr.Future) < 180 || len(tr.Past) > 200 {
		t.Errorf("track lengths %d, %d", len(tr.Past), len(tr.Future))
	}
	if _, err := a.GroundTrack("99999"); err == nil {
		t.Error("track for a deep-space satellite")
	}
	info, pts, err := a.PassTrack("99901", clk.t)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) < 10 || pts[0].El > 1 || pts[len(pts)-1].El != 0 {
		t.Errorf("sky track %d points, from %.1f° to %.1f°", len(pts), pts[0].El, pts[len(pts)-1].El)
	}
	top := 0.0
	for _, p := range pts {
		top = math.Max(top, p.El)
	}
	if math.Abs(top-info.MaxEl) > 1 {
		t.Errorf("sky track peaks at %.1f°, pass at %.1f°", top, info.MaxEl)
	}
	// Range rate goes from approaching to receding.
	if pts[0].RR >= 0 || pts[len(pts)-1].RR <= 0 {
		t.Errorf("range rate %.2f to %.2f", pts[0].RR, pts[len(pts)-1].RR)
	}
}

func TestFastClock(t *testing.T) {
	c := NewFastClock(start, 60)
	time.Sleep(50 * time.Millisecond)
	if d := c.Now().Sub(start); d < 2*time.Second || d > 30*time.Second {
		t.Errorf("60× clock moved %v in 50 ms", d)
	}
}
