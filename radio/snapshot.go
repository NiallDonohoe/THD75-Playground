package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Snapshot records the radio's settings before the program changes them,
// and what the program last wrote, so the radio can be put back as it was
// found: on quit, or on the next connection after an unplug or a crash.
type Snapshot struct {
	Version int       `json:"version"`
	Taken   time.Time `json:"taken"`
	Updated time.Time `json:"updated"`
	// Dirty is true from just before the first write until every restored
	// value has read back correctly.
	Dirty bool     `json:"dirty"`
	Radio Identity `json:"radio"`
	Port  string   `json:"port,omitempty"`

	// Original holds each writable field as first read, as raw parameter text.
	Original map[Field]string `json:"original"`
	// Written holds the value the program last wrote to each field.
	Written map[Field]string `json:"written"`
	// Order lists fields in the order the program first changed them.
	Order []Field `json:"order"`

	// Read-only context, kept for diagnosis; never written back.
	VFO     []string          `json:"vfo,omitempty"`     // FO 1 fields
	Filters map[string]string `json:"filters,omitempty"` // SH 0/1/2 index
}

const snapshotVersion = 1

// Store saves the snapshot as JSON, atomically.
type Store struct {
	Path string
}

// DefaultStorePath is radio-snapshot.json under the user config directory
// (Application Support on macOS).
func DefaultStorePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "d75scope", "radio-snapshot.json"), nil
}

// Load reads the stored snapshot. It returns an error wrapping
// os.ErrNotExist if there is none.
func (s Store) Load() (*Snapshot, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("read snapshot %s: %w", s.Path, err)
	}
	return &snap, nil
}

// Save writes the snapshot to a temporary file in the same directory,
// syncs it and renames it over the old one.
func (s Store) Save(snap *Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".radio-snapshot-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Guard sits between the program and the radio for every write. Begin takes
// the snapshot; Write records each value in the snapshot (saved, dirty)
// before sending it; Restore puts back what the program changed.
type Guard struct {
	r     FieldAccess
	store Store
	now   func() time.Time

	mu   sync.Mutex
	snap *Snapshot
}

// NewGuard returns a guard for r that saves to store.
func NewGuard(r FieldAccess, store Store) *Guard {
	return &Guard{r: r, store: store, now: time.Now}
}

// ErrNoSnapshot means a write was attempted before Begin.
var ErrNoSnapshot = errors.New("no snapshot taken; refusing to write")

// ErrPreviousDirty means an earlier session left the radio modified and its
// snapshot has not been restored. Begin refuses to overwrite it.
var ErrPreviousDirty = errors.New("a previous session left the radio modified; restore it first")

// Begin reads every writable field (plus FO and SH for the record) and
// saves the snapshot, clean, before any write is possible. It refuses to
// replace a dirty snapshot from an earlier session.
func (g *Guard) Begin(ctx context.Context, id Identity, port string, vfo []string, filters map[string]string) (*Snapshot, error) {
	if prev, err := g.store.Load(); err == nil && prev.Dirty {
		return nil, ErrPreviousDirty
	}
	snap := &Snapshot{
		Version:  snapshotVersion,
		Taken:    g.now().UTC(),
		Radio:    id,
		Port:     port,
		Original: map[Field]string{},
		Written:  map[Field]string{},
		VFO:      vfo,
		Filters:  filters,
	}
	for _, f := range WritableFields {
		v, err := g.r.ReadField(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", f, err)
		}
		snap.Original[f] = v
	}
	snap.Updated = snap.Taken
	if err := g.store.Save(snap); err != nil {
		return nil, fmt.Errorf("save snapshot: %w", err)
	}
	g.mu.Lock()
	g.snap = snap
	g.mu.Unlock()
	return snap, nil
}

// Snapshot returns a copy of the current snapshot, or nil before Begin.
func (g *Guard) Snapshot() *Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snap == nil {
		return nil
	}
	c := *g.snap
	c.Original = maps(g.snap.Original)
	c.Written = maps(g.snap.Written)
	c.Order = slices.Clone(g.snap.Order)
	return &c
}

func maps(m map[Field]string) map[Field]string {
	c := make(map[Field]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Write records the intended value, marks the snapshot dirty and saves it,
// then writes the field and returns the read-back.
func (g *Guard) Write(ctx context.Context, f Field, v string) (string, error) {
	g.mu.Lock()
	if g.snap == nil {
		g.mu.Unlock()
		return "", ErrNoSnapshot
	}
	if !slices.Contains(WritableFields, f) {
		g.mu.Unlock()
		return "", fmt.Errorf("%w: %s is not writable", ErrNotAllowed, f)
	}
	if !slices.Contains(g.snap.Order, f) {
		g.snap.Order = append(g.snap.Order, f)
	}
	g.snap.Written[f] = v
	g.snap.Dirty = true
	g.snap.Updated = g.now().UTC()
	err := g.store.Save(g.snap)
	g.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("save snapshot before write: %w", err)
	}
	got, err := g.r.WriteField(ctx, f, v)
	if err != nil {
		return "", err
	}
	if got != v {
		// The radio rounded or refused part of it. Remember what it holds,
		// because restore only touches fields still holding our value.
		g.mu.Lock()
		g.snap.Written[f] = got
		g.store.Save(g.snap)
		g.mu.Unlock()
	}
	return got, nil
}

// FieldOutcome says what restore did with one field.
type FieldOutcome string

const (
	OutcomeRestored     FieldOutcome = "restored"      // put back and read back correctly
	OutcomeUnchanged    FieldOutcome = "unchanged"     // already held its original value
	OutcomeOwnerChanged FieldOutcome = "owner-changed" // changed by hand since; left alone
	OutcomeFailed       FieldOutcome = "failed"        // could not be put back
)

// FieldRestore reports one field's restore.
type FieldRestore struct {
	Field    Field        `json:"field"`
	Original string       `json:"original"`
	Written  string       `json:"written"`
	Found    string       `json:"found"`
	Final    string       `json:"final"`
	Outcome  FieldOutcome `json:"outcome"`
	Error    string       `json:"error,omitempty"`
}

// RestoreReport is the result of a restore.
type RestoreReport struct {
	Fields []FieldRestore `json:"fields"`
	Clean  bool           `json:"clean"` // dirty flag cleared
}

// Restore puts back every field the program changed, in reverse order of
// first change, using only the allowed writes. A field is restored only if it
// still holds the value the program last wrote; if the owner has changed it
// by hand since, it is left alone. After the pass every restored field is
// read again (a later restore can disturb an earlier one, for example a mode
// change moving the frequency) and rewritten once if needed. The dirty flag
// is cleared only if every restored value reads back correctly.
func (g *Guard) Restore(ctx context.Context) (RestoreReport, error) {
	g.mu.Lock()
	snap := g.snap
	g.mu.Unlock()
	if snap == nil {
		return RestoreReport{Clean: true}, nil
	}
	rep := restore(ctx, g.r, snap)
	if !rep.Clean {
		return rep, errors.New("restore incomplete; the snapshot stays dirty")
	}
	g.mu.Lock()
	snap.Dirty = false
	snap.Written = map[Field]string{}
	snap.Order = nil
	snap.Updated = g.now().UTC()
	err := g.store.Save(snap)
	g.mu.Unlock()
	if err != nil {
		rep.Clean = false
		return rep, fmt.Errorf("restored, but could not clear the dirty flag: %w", err)
	}
	return rep, nil
}

// RestorePrevious restores a dirty snapshot left by an earlier session that
// ended in an unplug or a crash, then clears it.
func RestorePrevious(ctx context.Context, r FieldAccess, store Store, snap *Snapshot) (RestoreReport, error) {
	g := NewGuard(r, store)
	g.snap = snap
	return g.Restore(ctx)
}

func restore(ctx context.Context, r FieldAccess, snap *Snapshot) RestoreReport {
	var rep RestoreReport
	rep.Clean = true
	idx := map[Field]int{}
	for i := len(snap.Order) - 1; i >= 0; i-- {
		f := snap.Order[i]
		fr := FieldRestore{Field: f, Original: snap.Original[f], Written: snap.Written[f]}
		found, err := r.ReadField(ctx, f)
		if err != nil {
			fr.Outcome, fr.Error, rep.Clean = OutcomeFailed, err.Error(), false
			rep.Fields = append(rep.Fields, fr)
			continue
		}
		fr.Found, fr.Final = found, found
		switch {
		case found == fr.Original:
			fr.Outcome = OutcomeUnchanged
		case found != fr.Written:
			fr.Outcome = OutcomeOwnerChanged
		default:
			got, err := r.WriteField(ctx, f, fr.Original)
			fr.Final = got
			switch {
			case err != nil:
				fr.Outcome, fr.Error, rep.Clean = OutcomeFailed, err.Error(), false
			case got != fr.Original:
				fr.Outcome, fr.Error, rep.Clean = OutcomeFailed, fmt.Sprintf("read back %q", got), false
			default:
				fr.Outcome = OutcomeRestored
			}
		}
		idx[f] = len(rep.Fields)
		rep.Fields = append(rep.Fields, fr)
	}
	// Verification pass, in the same order.
	for i := len(snap.Order) - 1; i >= 0; i-- {
		f := snap.Order[i]
		j, ok := idx[f]
		if !ok || rep.Fields[j].Outcome != OutcomeRestored && rep.Fields[j].Outcome != OutcomeUnchanged {
			continue
		}
		fr := &rep.Fields[j]
		got, err := r.ReadField(ctx, f)
		if err == nil && got == fr.Original {
			fr.Final = got
			continue
		}
		got, err = r.WriteField(ctx, f, fr.Original)
		fr.Final = got
		if err != nil || got != fr.Original {
			fr.Outcome, rep.Clean = OutcomeFailed, false
			if err != nil {
				fr.Error = err.Error()
			} else {
				fr.Error = fmt.Sprintf("disturbed by a later restore; read back %q", got)
			}
		} else {
			fr.Outcome = OutcomeRestored
		}
	}
	return rep
}
