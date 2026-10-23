package radio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type guardFixture struct {
	fake  *FakeD75
	cat   *CAT
	store Store
	guard *Guard
	orig  map[Field]string
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	fake := NewFakeD75()
	cat := newTestCAT(t, fake, SessionConfig{})
	store := Store{Path: filepath.Join(t.TempDir(), "radio-snapshot.json")}
	g := NewGuard(cat, store)
	if _, err := g.Begin(context.Background(), Identity{Model: "TH-D75", Firmware: "1.03"}, "/dev/test", nil, nil); err != nil {
		t.Fatal(err)
	}
	return &guardFixture{fake: fake, cat: cat, store: store, guard: g, orig: fake.Settings()}
}

// changeEverything writes all six fields, as a session would.
func (f *guardFixture) changeEverything(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, w := range []struct {
		f Field
		v string
	}{
		{FieldAI, "1"}, {FieldIO, "1"}, {FieldMD, "5"}, {FieldFT, "1"}, {FieldFS, "0"}, {FieldFQ, "0007075000"},
	} {
		if _, err := f.guard.Write(ctx, w.f, w.v); err != nil {
			t.Fatalf("write %s %s: %v", w.f, w.v, err)
		}
	}
	if s := f.fake.Settings(); s[FieldIO] != "1" || s[FieldFQ] != "0007075000" {
		t.Fatalf("writes did not land: %v", s)
	}
}

func assertSettings(t *testing.T, got, want map[Field]string) {
	t.Helper()
	for _, f := range WritableFields {
		if got[f] != want[f] {
			t.Errorf("%s = %q, want %q", f, got[f], want[f])
		}
	}
}

func TestSnapshotSavedBeforeFirstWrite(t *testing.T) {
	f := newGuardFixture(t)
	snap, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Dirty || len(snap.Original) != len(WritableFields) {
		t.Errorf("after Begin: dirty=%v original=%v", snap.Dirty, snap.Original)
	}
	// Make the radio refuse IO so the write fails after the snapshot is saved.
	f.fake.NotAvailable = map[string]bool{"IO": true}
	if _, err := f.guard.Write(context.Background(), FieldIO, "1"); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("err = %v", err)
	}
	snap, _ = f.store.Load()
	if !snap.Dirty || snap.Written[FieldIO] != "1" {
		t.Errorf("snapshot not marked dirty before the write went out: %+v", snap)
	}
}

func TestWriteWithoutSnapshotRefused(t *testing.T) {
	fake := NewFakeD75()
	g := NewGuard(newTestCAT(t, fake, SessionConfig{}), Store{Path: filepath.Join(t.TempDir(), "s.json")})
	if _, err := g.Write(context.Background(), FieldIO, "1"); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("err = %v", err)
	}
	if len(fake.Received()) != 0 {
		t.Errorf("radio received %v", fake.Received())
	}
}

func TestRestoreOnQuit(t *testing.T) {
	f := newGuardFixture(t)
	f.changeEverything(t)
	rep, err := f.guard.Restore(context.Background())
	if err != nil {
		t.Fatalf("restore: %v (%+v)", err, rep)
	}
	assertSettings(t, f.fake.Settings(), f.orig)
	// Reverse order of first change: FQ, FS, FT, MD, IO, AI.
	want := []Field{FieldFQ, FieldFS, FieldFT, FieldMD, FieldIO, FieldAI}
	for i, fr := range rep.Fields {
		if fr.Field != want[i] || fr.Outcome != OutcomeRestored {
			t.Errorf("step %d: %s %s, want %s restored", i, fr.Field, fr.Outcome, want[i])
		}
	}
	snap, _ := f.store.Load()
	if snap.Dirty {
		t.Error("dirty flag not cleared")
	}
	// Restore must use only allowed writes, never FO.
	for _, raw := range f.fake.Received() {
		if l, _ := ParseLine(raw); l.Cmd == "FO" && len(l.Params) > 1 {
			t.Errorf("restore wrote FO: %q", raw)
		}
	}
	assertOnlyAllowed(t, f.fake)
}

func TestRestoreOnlyWhatChanged(t *testing.T) {
	f := newGuardFixture(t)
	ctx := context.Background()
	f.guard.Write(ctx, FieldIO, "1")
	before := len(f.fake.Received())
	rep, err := f.guard.Restore(ctx)
	if err != nil || len(rep.Fields) != 1 || rep.Fields[0].Field != FieldIO {
		t.Fatalf("rep = %+v, %v", rep, err)
	}
	for _, raw := range f.fake.Received()[before:] {
		if l, _ := ParseLine(raw); IsWrite(l) && l.Cmd != "IO" {
			t.Errorf("restore touched %q", raw)
		}
	}
}

func TestRestoreLeavesOwnerChanges(t *testing.T) {
	f := newGuardFixture(t)
	f.changeEverything(t)
	// The owner turns the knob and switches to LSB by hand.
	f.fake.SetByHand(FieldFQ, "7080000")
	f.fake.SetByHand(FieldMD, "3")
	rep, err := f.guard.Restore(context.Background())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	got := f.fake.Settings()
	if got[FieldFQ] != "0007080000" || got[FieldMD] != "3" {
		t.Errorf("owner's values overwritten: %v", got)
	}
	for _, fld := range []Field{FieldIO, FieldFT, FieldFS, FieldAI} {
		if got[fld] != f.orig[fld] {
			t.Errorf("%s = %q, want restored %q", fld, got[fld], f.orig[fld])
		}
	}
	outcomes := map[Field]FieldOutcome{}
	for _, fr := range rep.Fields {
		outcomes[fr.Field] = fr.Outcome
	}
	if outcomes[FieldFQ] != OutcomeOwnerChanged || outcomes[FieldMD] != OutcomeOwnerChanged {
		t.Errorf("outcomes = %v", outcomes)
	}
	if snap, _ := f.store.Load(); snap.Dirty {
		t.Error("owner changes should not keep the snapshot dirty")
	}
}

func TestRestoreUnplugThenReconnect(t *testing.T) {
	f := newGuardFixture(t)
	f.changeEverything(t)
	f.fake.Unplug()
	if _, err := f.guard.Restore(context.Background()); err == nil {
		t.Fatal("restore succeeded with the radio unplugged")
	}
	snap, err := f.store.Load()
	if err != nil || !snap.Dirty {
		t.Fatalf("snapshot after unplug: %+v, %v", snap, err)
	}
	// Reconnect: a new session finds the dirty snapshot and restores it.
	cat := newTestCAT(t, f.fake, SessionConfig{})
	if _, err := NewGuard(cat, f.store).Begin(context.Background(), Identity{}, "", nil, nil); !errors.Is(err, ErrPreviousDirty) {
		t.Errorf("Begin over a dirty snapshot: err = %v", err)
	}
	if _, err := RestorePrevious(context.Background(), cat, f.store, snap); err != nil {
		t.Fatalf("restore previous: %v", err)
	}
	assertSettings(t, f.fake.Settings(), f.orig)
	if snap, _ := f.store.Load(); snap.Dirty {
		t.Error("dirty flag not cleared after restoring the previous session")
	}
}

func TestRestoreAfterCrash(t *testing.T) {
	f := newGuardFixture(t)
	f.changeEverything(t)
	// Crash: no restore runs, the process is gone. Only the file survives.
	snap, err := f.store.Load()
	if err != nil || !snap.Dirty {
		t.Fatalf("snapshot: %+v, %v", snap, err)
	}
	f.cat.Close()
	cat := newTestCAT(t, f.fake, SessionConfig{})
	if _, err := RestorePrevious(context.Background(), cat, f.store, snap); err != nil {
		t.Fatal(err)
	}
	assertSettings(t, f.fake.Settings(), f.orig)
}

func TestRestoreReverifiesWhenModeMovesFrequency(t *testing.T) {
	// This radio snaps the frequency to 1 kHz on every mode change. The app
	// changes MD, then FQ; restore puts FQ back first, then MD, which moves
	// FQ again. The verification pass must catch that and fix FQ.
	fake := NewFakeD75()
	fake.SetByHand(FieldFQ, "7074500")
	fake.ModeChangeSnapsHz = 1000
	cat := newTestCAT(t, fake, SessionConfig{})
	store := Store{Path: filepath.Join(t.TempDir(), "s.json")}
	g := NewGuard(cat, store)
	ctx := context.Background()
	if _, err := g.Begin(ctx, Identity{}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	orig := fake.Settings()
	if _, err := g.Write(ctx, FieldMD, "5"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Write(ctx, FieldFQ, "0007075000"); err != nil {
		t.Fatal(err)
	}
	rep, err := g.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v %+v", err, rep)
	}
	assertSettings(t, fake.Settings(), orig)
}

func TestRestoreFailureKeepsDirty(t *testing.T) {
	f := newGuardFixture(t)
	f.changeEverything(t)
	f.fake.NotAvailable = map[string]bool{"FT": true}
	if _, err := f.guard.Restore(context.Background()); err == nil {
		t.Fatal("restore reported success")
	}
	if snap, _ := f.store.Load(); !snap.Dirty {
		t.Error("dirty flag cleared despite a failed restore")
	}
}

func TestStoreAtomicSave(t *testing.T) {
	dir := t.TempDir()
	s := Store{Path: filepath.Join(dir, "sub", "radio-snapshot.json")}
	if _, err := s.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("load missing: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Save(&Snapshot{Version: 1, Dirty: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(s.Path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
	snap, err := s.Load()
	if err != nil || !snap.Dirty {
		t.Errorf("load = %+v, %v", snap, err)
	}
}
