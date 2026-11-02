// Package satapp is d75sat's engine: the catalogue turned into
// propagators, the station's location and alerts, live positions and
// look angles every second, passes for the next day, and ground and sky
// tracks on request, published as events for the page. Doppler tuning
// joins it in milestone S2.
package satapp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"d75scope/orbit"
	"d75scope/satdata"
)

// Clock tells the time. Demo mode runs one fast.
type Clock interface{ Now() time.Time }

// RealClock is the system clock, in UTC.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FastClock runs Speed times faster than real time from Start.
type FastClock struct {
	Start time.Time // the clock's time when it was made
	Speed float64
	began time.Time
}

// NewFastClock starts a clock at start running speed times real time.
func NewFastClock(start time.Time, speed float64) *FastClock {
	return &FastClock{Start: start.UTC(), Speed: speed, began: time.Now()}
}

func (c *FastClock) Now() time.Time {
	return c.Start.Add(time.Duration(float64(time.Since(c.began)) * c.Speed))
}

// Horizons and steps.
const (
	PassHours     = 24
	recomputeSim  = 10 * time.Minute // passes are recomputed this often (clock time)
	trackStep     = 30 * time.Second
	skyStep       = 10 * time.Second
	subQueue      = 64
	maxTrackHours = 4
)

// SatInfo is a catalogue entry as the page sees it.
type SatInfo struct {
	satdata.Satellite
	Predicted bool    `json:"predicted"`         // false when SGP4 cannot use the elements
	Problem   string  `json:"problem,omitempty"` // why not
	PeriodMin float64 `json:"periodMin,omitempty"`
}

// PassInfo is one pass for the pass table.
type PassInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AOS        time.Time `json:"aos"`
	TCA        time.Time `json:"tca"`
	LOS        time.Time `json:"los"`
	AOSAz      float64   `json:"aosAz"`
	LOSAz      float64   `json:"losAz"`
	MaxEl      float64   `json:"maxEl"`
	InProgress bool      `json:"inProgress,omitempty"`
}

// Live is one satellite's position at a tick.
type Live struct {
	ID  string  `json:"id"`
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Alt float64 `json:"alt"`          // km
	Az  float64 `json:"az,omitempty"` // from the station, when it has a location
	El  float64 `json:"el,omitempty"`
	RR  float64 `json:"rr,omitempty"` // range rate, km/s, positive receding
	Rng float64 `json:"range,omitempty"`
}

// Event is one change for the page, already encoded.
type Event struct {
	Type string
	Data json.RawMessage
}

// Snapshot is everything the page needs to draw itself from scratch.
type Snapshot struct {
	Now    time.Time                 `json:"now"`
	Config Config                    `json:"config"`
	Sats   []SatInfo                 `json:"sats"`
	Passes []PassInfo                `json:"passes"`
	Status map[string]satdata.Status `json:"status"`
	Live   []Live                    `json:"live"`
}

type entry struct {
	info SatInfo
	prop *orbit.Satellite // nil when not predicted
}

// App holds the state. Its methods are safe for concurrent use.
type App struct {
	clock   Clock
	cfgPath string

	mu      sync.Mutex
	cfg     Config
	sats    []entry
	byID    map[string]*entry
	passes  []PassInfo
	passGen int
	passAt  time.Time
	status  map[string]satdata.Status
	subs    map[chan Event]struct{}
	live    []Live
	wake    chan struct{}
}

// New makes an app. cfgPath "" keeps the config in memory (demo).
func New(clock Clock, cfg Config, cfgPath string) *App {
	cfg.fill()
	return &App{clock: clock, cfgPath: cfgPath, cfg: cfg, byID: map[string]*entry{},
		status: map[string]satdata.Status{}, subs: map[chan Event]struct{}{}, wake: make(chan struct{}, 1)}
}

// Now is the app's clock.
func (a *App) Now() time.Time { return a.clock.Now() }

// SetCatalog replaces the catalogue.
func (a *App) SetCatalog(sats []satdata.Satellite, status map[string]satdata.Status) {
	entries := make([]entry, 0, len(sats))
	for _, s := range sats {
		e := entry{info: SatInfo{Satellite: s}}
		if p, err := orbit.New(s.Elements); err != nil {
			e.info.Problem = err.Error()
			if errors.Is(err, orbit.ErrDeepSpace) {
				e.info.Problem = "deep-space orbit: not predicted"
			}
		} else {
			e.prop, e.info.Predicted = p, true
			e.info.PeriodMin = math.Round(1440/s.Elements.MeanMotion*10) / 10
		}
		entries = append(entries, e)
	}
	a.mu.Lock()
	a.sats = entries
	a.byID = map[string]*entry{}
	for i := range a.sats {
		a.byID[a.sats[i].info.ID] = &a.sats[i]
	}
	for k, v := range status {
		a.status[k] = v
	}
	a.publish("catalog", map[string]any{"sats": a.infos(), "status": a.status})
	a.mu.Unlock()
	a.recompute()
}

// SetStatus updates a source's status (a failed refresh, say).
func (a *App) SetStatus(status map[string]satdata.Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range status {
		a.status[k] = v
	}
	a.publish("status", a.status)
}

// SetLocation sets and saves the station's position.
func (a *App) SetLocation(l Location) error {
	if err := checkLocation(l); err != nil {
		return err
	}
	l.Set = a.clock.Now()
	a.mu.Lock()
	a.cfg.Location = &l
	err := saveConfig(a.cfgPath, a.cfg)
	a.publish("config", a.cfg)
	a.mu.Unlock()
	a.recompute()
	return err
}

// SetAlert turns the AOS alert for one satellite on or off.
func (a *App) SetAlert(id string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.byID[id]; !ok {
		return errors.New("no such satellite")
	}
	if on {
		a.cfg.Alerts[id] = true
	} else {
		delete(a.cfg.Alerts, id)
	}
	a.publish("config", a.cfg)
	return saveConfig(a.cfgPath, a.cfg)
}

// SetAlertTiming sets how long before AOS alerts come, and the lowest
// peak elevation that earns one.
func (a *App) SetAlertTiming(minutes int, minEl float64) error {
	if minutes < 1 || minutes > 60 || minEl < 0 || minEl > 90 || math.IsNaN(minEl) {
		return errors.New("alerts come 1 to 60 minutes before AOS, for peaks of 0° to 90°")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg.AlertMinutes, a.cfg.AlertMinEl = minutes, minEl
	a.publish("config", a.cfg)
	return saveConfig(a.cfgPath, a.cfg)
}

// Config returns the current config.
func (a *App) Config() Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *App) infos() []SatInfo {
	out := make([]SatInfo, len(a.sats))
	for i, e := range a.sats {
		out[i] = e.info
	}
	return out
}

// Subscribe returns a snapshot and the events after it. The channel is
// closed if the subscriber falls behind; cancel ends the subscription.
func (a *App) Subscribe() (Snapshot, <-chan Event, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := Snapshot{Now: a.clock.Now(), Config: a.cfg, Sats: a.infos(), Passes: a.passes,
		Status: map[string]satdata.Status{}, Live: a.live}
	for k, v := range a.status {
		snap.Status[k] = v
	}
	ch := make(chan Event, subQueue)
	a.subs[ch] = struct{}{}
	return snap, ch, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if _, ok := a.subs[ch]; ok {
			delete(a.subs, ch)
			close(ch)
		}
	}
}

// publish encodes v and queues it for every subscriber. The caller holds
// a.mu.
func (a *App) publish(typ string, v any) {
	if len(a.subs) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	for ch := range a.subs {
		select {
		case ch <- Event{typ, b}:
		default:
			delete(a.subs, ch)
			close(ch)
		}
	}
}

func (a *App) recompute() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Run ticks once a second until ctx ends: live positions every tick, and
// passes when something changed or every ten minutes of clock time.
func (a *App) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	a.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
			a.computePasses()
		case <-t.C:
			a.tick()
			a.mu.Lock()
			due := a.clock.Now().Sub(a.passAt) >= recomputeSim
			a.mu.Unlock()
			if due {
				a.computePasses()
			}
		}
	}
}

// round rounds to a unit such as 0.1 or 1e-3, multiplying by the whole
// number 1/unit so the result prints as the short decimal it should.
func round(v, unit float64) float64 {
	k := math.Round(1 / unit)
	return math.Round(v*k) / k
}

// tick works out where every satellite is.
func (a *App) tick() {
	now := a.clock.Now()
	a.mu.Lock()
	sats := a.sats
	loc := a.cfg.Location
	a.mu.Unlock()
	live := make([]Live, 0, len(sats))
	for _, e := range sats {
		if e.prop == nil {
			continue
		}
		g, err := e.prop.SubPoint(now)
		if err != nil {
			continue
		}
		l := Live{ID: e.info.ID, Lat: round(g.Lat, 1e-3), Lon: round(g.Lon, 1e-3), Alt: round(g.Alt, 0.1)}
		if loc != nil {
			if lk, err := e.prop.Look(observer(loc), now); err == nil {
				l.Az, l.El, l.RR, l.Rng = round(lk.Az, 0.1), round(lk.El, 0.1), round(lk.RangeRate, 1e-4), round(lk.Range, 0.1)
			}
		}
		live = append(live, l)
	}
	a.mu.Lock()
	a.live = live
	a.publish("tick", map[string]any{"now": now, "live": live})
	a.mu.Unlock()
}

func observer(l *Location) orbit.Observer {
	return orbit.Observer{Lat: l.Lat, Lon: l.Lon, AltM: l.AltM}
}

// computePasses finds every satellite's passes for the next day. It runs
// without the lock, and a newer computation started meanwhile wins.
func (a *App) computePasses() {
	now := a.clock.Now()
	a.mu.Lock()
	a.passGen++
	gen := a.passGen
	sats := a.sats
	loc := a.cfg.Location
	a.passAt = now
	a.mu.Unlock()
	var out []PassInfo
	if loc != nil {
		obs := observer(loc)
		for _, e := range sats {
			if e.prop == nil {
				continue
			}
			ps, _ := e.prop.Passes(obs, now, now.Add(PassHours*time.Hour), 0)
			for _, p := range ps {
				out = append(out, PassInfo{ID: e.info.ID, Name: e.info.Name, AOS: p.AOS, TCA: p.TCA, LOS: p.LOS,
					AOSAz: round(p.AOSAz, 0.1), LOSAz: round(p.LOSAz, 0.1), MaxEl: round(p.MaxEl, 0.1), InProgress: p.InProgress})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AOS.Before(out[j].AOS) })
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.passGen {
		return
	}
	a.passes = out
	a.publish("passes", map[string]any{"computed": now, "passes": out})
}

// Passes returns the passes computed last.
func (a *App) Passes() []PassInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.passes
}

// ErrUnknown means no satellite has that catalogue number, or it cannot
// be predicted.
var ErrUnknown = errors.New("no predictable satellite with that number")

func (a *App) prop(id string) (*orbit.Satellite, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.byID[id]
	if !ok || e.prop == nil {
		return nil, ErrUnknown
	}
	return e.prop, nil
}

// Track is a ground track: one orbit before now and one after (at most
// four hours each way), as [lat, lon] pairs.
type Track struct {
	Now    time.Time    `json:"now"`
	Past   [][2]float64 `json:"past"`
	Future [][2]float64 `json:"future"`
}

// GroundTrack returns a satellite's track around now.
func (a *App) GroundTrack(id string) (Track, error) {
	p, err := a.prop(id)
	if err != nil {
		return Track{}, err
	}
	now := a.clock.Now()
	period := time.Duration(1440 / p.MeanMotion * float64(time.Minute))
	period = min(period, maxTrackHours*time.Hour)
	pts := func(from, to time.Time) [][2]float64 {
		gs, _ := p.GroundTrack(from, to, trackStep)
		out := make([][2]float64, len(gs))
		for i, g := range gs {
			out[i] = [2]float64{round(g.Lat, 1e-3), round(g.Lon, 1e-3)}
		}
		return out
	}
	return Track{Now: now, Past: pts(now.Add(-period), now), Future: pts(now, now.Add(period))}, nil
}

// SkyPoint is where a satellite is in the sky at a moment of a pass.
type SkyPoint struct {
	T  time.Time `json:"t"`
	Az float64   `json:"az"`
	El float64   `json:"el"`
	RR float64   `json:"rr"`
}

// PassTrack returns the sky track of the pass of id that is under way at
// or starts nearest after `at`, every ten seconds from AOS to LOS.
func (a *App) PassTrack(id string, at time.Time) (PassInfo, []SkyPoint, error) {
	p, err := a.prop(id)
	if err != nil {
		return PassInfo{}, nil, err
	}
	a.mu.Lock()
	loc := a.cfg.Location
	a.mu.Unlock()
	if loc == nil {
		return PassInfo{}, nil, errors.New("set the station's location first")
	}
	obs := observer(loc)
	ps, err := p.Passes(obs, at, at.Add(PassHours*time.Hour), 0)
	if err != nil || len(ps) == 0 {
		return PassInfo{}, nil, errors.New("no pass in the next day")
	}
	pass := ps[0]
	var pts []SkyPoint
	for t := pass.AOS; !t.After(pass.LOS); t = t.Add(skyStep) {
		if l, err := p.Look(obs, t); err == nil {
			pts = append(pts, SkyPoint{t, round(l.Az, 0.1), round(math.Max(l.El, 0), 0.1), round(l.RangeRate, 1e-4)})
		}
	}
	if l, err := p.Look(obs, pass.LOS); err == nil {
		pts = append(pts, SkyPoint{pass.LOS, round(l.Az, 0.1), 0, round(l.RangeRate, 1e-4)})
	}
	info := PassInfo{ID: id, Name: p.Name, AOS: pass.AOS, TCA: pass.TCA, LOS: pass.LOS,
		AOSAz: round(pass.AOSAz, 0.1), LOSAz: round(pass.LOSAz, 0.1), MaxEl: round(pass.MaxEl, 0.1), InProgress: pass.InProgress}
	return info, pts, nil
}
