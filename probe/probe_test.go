package probe

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"d75scope/radio"
)

type scripted struct{ answers map[string]string }

func (s scripted) Ask(q string, _ time.Duration) (string, bool) {
	for k, v := range s.answers {
		if strings.Contains(q, k) {
			return v, true
		}
	}
	return "", true
}

func testOptions(t *testing.T, m *Mock, writes bool) Options {
	t.Helper()
	dir := t.TempDir()
	o := Options{
		OutDir:        filepath.Join(dir, "out"),
		StorePath:     filepath.Join(dir, "radio-snapshot.json"),
		AllowWrites:   writes,
		RecordSeconds: 1.5,
		ShortSeconds:  1,
		SettleSeconds: 0.05,
		RefCarrierHz:  MockRefCarrierHz,
	}
	m.Apply(&o)
	return o
}

func assertRadioRestored(t *testing.T, m *Mock, want map[radio.Field]string) {
	t.Helper()
	got := m.Fake.Settings()
	for _, f := range radio.WritableFields {
		if got[f] != want[f] {
			t.Errorf("radio %s = %q after the probe, want %q", f, got[f], want[f])
		}
	}
	for _, raw := range m.Fake.Received() {
		if _, err := radio.CheckRaw(raw); err != nil {
			t.Errorf("radio received a refused line %q", raw)
		}
	}
}

func TestProbeMockFullRun(t *testing.T) {
	for _, transport := range []string{"pipe", "pty"} {
		t.Run(transport, func(t *testing.T) {
			m := NewMock(2, false)
			if transport == "pty" {
				if err := m.UsePTY(); err != nil {
					t.Skipf("no pty: %v", err)
				}
			}
			orig := m.Fake.Settings()
			o := testOptions(t, m, true)
			o.Prompt = scripted{map[string]string{"turn the band B dial": "", "speaker": "yes", "Menu 170": "700"}}
			// Turn the knob when the AI test asks.
			o.Prompt = knobTurner{scripted: o.Prompt.(scripted), m: m}
			rep, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("Run: %v\nerrors: %v", err, rep.Errors)
			}
			if !rep.CAT.Responding || rep.CAT.Identity.Model != "TH-D75" {
				t.Errorf("CAT = %+v", rep.CAT.Identity)
			}
			if rep.Audio.Radio == nil || rep.Audio.Radio.Name != "TH-D75" {
				t.Errorf("audio device = %+v", rep.Audio.Radio)
			}
			wantS := map[string]int{"USB": 1, "LSB": -1, "CW": 1, "AM": 1}
			if len(rep.Inversion) != 4 {
				t.Fatalf("inversion results: %+v", rep.Inversion)
			}
			for _, inv := range rep.Inversion {
				if inv.S != wantS[inv.Mode] {
					t.Errorf("%s: s = %d, want %d (%s, shift %.1f, score %.2f, energy %.0f)", inv.Mode, inv.S, wantS[inv.Mode], inv.Verdict, inv.MeasuredShiftHz, inv.Score, inv.Energy)
				}
			}
			if len(rep.Offsets) != 4 {
				t.Fatalf("offsets: %+v", rep.Offsets)
			}
			for _, off := range rep.Offsets {
				if want := MockScene.DeltaHz[off.Mode]; math.Abs(off.DeltaHz-want) > 10 {
					t.Errorf("%s: Δ = %.1f Hz, want %.0f ±10", off.Mode, off.DeltaHz, want)
				}
			}
			if rep.Restore == nil || !rep.Restore.Clean {
				t.Errorf("restore = %+v", rep.Restore)
			}
			// The AI test captured the knob turn; restore put the dial back
			// to where it was found, before the turn.
			if rep.AITest == nil || len(rep.AITest.Lines) == 0 {
				t.Errorf("AI test = %+v", rep.AITest)
			}
			assertRadioRestored(t, m, orig)
			for _, rec := range rep.Recordings {
				if _, err := os.Stat(filepath.Join(o.OutDir, rec.File)); err != nil {
					t.Errorf("missing %s", rec.File)
				}
			}
			names := map[string]bool{}
			for _, rec := range rep.Recordings {
				names[rec.Name] = true
			}
			for _, n := range []string{"as-found", "if-usb", "if-usb-up", "if-lsb", "if-cw", "if-am-up", "mode-fm", "mode-nfm", "detect-usb"} {
				if !names[n] {
					t.Errorf("no %s recording", n)
				}
			}
			ans := map[string]Answer{}
			for _, a := range rep.Answers {
				ans[a.ID] = a
			}
			if len(ans) != len(Questions) {
				t.Errorf("%d answers for %d questions", len(ans), len(Questions))
			}
			for id, want := range map[string]string{
				"Q02": "identical", "Q11": "FM: silent", "Q13": "yes", "Q18": "no", "Q20": "no foreign",
			} {
				if !strings.Contains(ans[id].Answer, want) {
					t.Errorf("%s = %q, want it to mention %q", id, ans[id].Answer, want)
				}
			}
			if pb := ans["Q07"].Answer; !strings.Contains(pb, "centre 12") && !strings.Contains(pb, "centre 11") {
				t.Errorf("Q07 = %q", pb)
			}
			if s := Summary(rep); !strings.Contains(s, "MOCK RUN") || !strings.Contains(s, "restored and verified") {
				t.Errorf("summary:\n%s", s)
			}
			if err := rep.WriteJSON(filepath.Join(o.OutDir, "probe-report.json")); err != nil {
				t.Fatal(err)
			}
			checkSchema(t, filepath.Join(o.OutDir, "probe-report.json"))
		})
	}
}

// knobTurner answers the AI test by turning the fake radio's dial.
type knobTurner struct {
	scripted
	m *Mock
}

func (k knobTurner) Ask(q string, d time.Duration) (string, bool) {
	if strings.Contains(q, "turn the band B dial") {
		st := k.m.Fake.State()
		k.m.Fake.TurnKnob(st.FrequencyHz + 5000)
		k.m.Fake.TurnKnob(st.FrequencyHz + 10000)
		return "", true
	}
	return k.scripted.Ask(q, d)
}

func TestProbeReadOnlySendsNoWrites(t *testing.T) {
	m := NewMock(1, false)
	o := testOptions(t, m, false)
	rep, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, raw := range m.Fake.Received() {
		l, _ := radio.ParseLine(raw)
		if radio.IsWrite(l) {
			t.Errorf("read-only probe sent a write: %q", raw)
		}
	}
	want := []string{"ID", "FV", "FO 1", "FQ 1", "MD 1", "IO", "FT", "FS", "AI", "SH 0", "SH 1", "SH 2", "BY 1"}
	var got []string
	for _, r := range rep.CAT.Reads {
		got = append(got, r.Command)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("reads = %v", got)
	}
	if _, err := os.Stat(o.StorePath); err == nil {
		t.Error("read-only probe wrote a snapshot")
	}
	// IO was 0 and the synthetic AF output is silent.
	if len(rep.Recordings) != 1 || !rep.Recordings[0].Analysis.Silent {
		t.Errorf("as-found recording: %+v", rep.Recordings)
	}
	if rep.Serial.ExclusiveLock != "verified" {
		t.Errorf("exclusive lock = %q", rep.Serial.ExclusiveLock)
	}
}

func TestProbeRefusesWritesOverDirtySnapshot(t *testing.T) {
	m := NewMock(1, false)
	o := testOptions(t, m, true)
	// A previous session crashed after changing IO and MD.
	prev := &radio.Snapshot{Version: 1, Dirty: true, Radio: radio.Identity{Model: "TH-D75"},
		Original: map[radio.Field]string{radio.FieldIO: "0", radio.FieldMD: "4"},
		Written:  map[radio.Field]string{radio.FieldIO: "1", radio.FieldMD: "5"},
		Order:    []radio.Field{radio.FieldIO, radio.FieldMD}}
	store := radio.Store{Path: o.StorePath}
	if err := store.Save(prev); err != nil {
		t.Fatal(err)
	}
	m.Fake.SetByHand(radio.FieldIO, "1")
	m.Fake.SetByHand(radio.FieldMD, "5")

	rep, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "--restore-previous") {
		t.Fatalf("err = %v", err)
	}
	if rep.Snapshot != nil || len(rep.Inversion) > 0 {
		t.Error("probe went ahead over a dirty snapshot")
	}

	m2 := NewMock(1, false)
	m2.Fake.SetByHand(radio.FieldIO, "1")
	m2.Fake.SetByHand(radio.FieldMD, "5")
	o2 := testOptions(t, m2, true)
	o2.StorePath = o.StorePath
	o2.RestorePrevious = true
	rep, err = Run(context.Background(), o2)
	if err != nil {
		t.Fatalf("with --restore-previous: %v", err)
	}
	if rep.PreviousSession == nil || !rep.PreviousSession.Restored {
		t.Errorf("previous session = %+v", rep.PreviousSession)
	}
	if s := m2.Fake.Settings(); s[radio.FieldIO] != "0" || s[radio.FieldMD] != "4" {
		t.Errorf("radio = %v, want the previous session's originals", s)
	}
}

func TestProbeInterruptedStillRestores(t *testing.T) {
	m := NewMock(1, false)
	orig := m.Fake.Settings()
	o := testOptions(t, m, true)
	o.RecordSeconds = 3
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel once the probe has started writing.
		for {
			time.Sleep(5 * time.Millisecond)
			if m.Fake.Settings()[radio.FieldIO] == "1" {
				cancel()
				return
			}
		}
	}()
	rep, _ := Run(ctx, o)
	if rep.Restore == nil || !rep.Restore.Clean {
		t.Fatalf("restore after interrupt = %+v", rep.Restore)
	}
	assertRadioRestored(t, m, orig)
}

func TestProbeKISSRadio(t *testing.T) {
	m := NewMock(1, false)
	m.Fake.Silent = true
	o := testOptions(t, m, true)
	o.Session.Timeout = 50 * time.Millisecond
	go func() {
		time.Sleep(20 * time.Millisecond)
		m.Fake.Inject([]byte{0xC0, 0x00, 'x', 0xC0})
	}()
	rep, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("no error")
	}
	if !strings.Contains(strings.Join(rep.Errors, " "), "KISS") {
		t.Errorf("errors = %v", rep.Errors)
	}
	if len(m.Fake.Received()) > 2 { // ID, retry
		t.Errorf("sent %v to a radio in KISS mode", m.Fake.Received())
	}
}

func TestProbeNMEAHoldsWrites(t *testing.T) {
	m := NewMock(1, false)
	orig := m.Fake.Settings()
	o := testOptions(t, m, true)
	o.Session.CleanFor = time.Hour
	// NMEA arrives as soon as the port opens.
	go func() {
		for i := 0; i < 50; i++ {
			time.Sleep(2 * time.Millisecond)
			m.Fake.Inject([]byte("$GPGGA,1,2,3\r\n"))
		}
	}()
	rep, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("writes went ahead with NMEA on the port")
	}
	for _, raw := range m.Fake.Received() {
		if l, _ := radio.ParseLine(raw); radio.IsWrite(l) {
			t.Errorf("write %q sent while NMEA was on the port", raw)
		}
	}
	assertRadioRestored(t, m, orig)
	found := false
	for _, a := range rep.Answers {
		if a.ID == "Q20" && strings.HasPrefix(a.Answer, "YES") {
			found = true
		}
	}
	if !found {
		t.Errorf("Q20 not flagged: %+v", rep.Answers)
	}
}
