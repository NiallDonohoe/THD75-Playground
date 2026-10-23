package probe

import (
	"io"
	"sync"

	"d75scope/dsp"
	"d75scope/ifsource"
	"d75scope/radio"
)

// MockScene is the hidden truth of the mock run: per-mode inversion and
// offsets the probe must recover, and a steady reference carrier.
var MockScene = ifsource.Scene{
	Carriers: []ifsource.RFCarrier{
		{FreqHz: 7_076_300, LevelDBFS: -20},                                                 // reference carrier
		{FreqHz: 7_071_800, LevelDBFS: -38},                                                 // weaker carrier
		{FreqHz: 7_073_100, LevelDBFS: -42, Kind: dsp.KindTwoTone},                          // SSB-ish
		{FreqHz: 7_078_900, LevelDBFS: -40, Kind: dsp.KindMorse, Text: "CQ TEST DE N0CALL"}, // keyed CW
	},
	NoiseDBFS: -55,
	Inverted:  map[string]bool{"LSB": true},
	DeltaHz:   map[string]float64{"USB": -1450, "LSB": 1450, "CW": -700, "AM": 0, "FM": 0, "NFM": 0},
	IFModes:   []string{"USB", "LSB", "CW", "AM"},
}

// MockRefCarrierHz is the reference carrier's frequency.
const MockRefCarrierHz = 7_076_300

// Mock is a fake TH-D75 and a synthetic IF that follows it.
type Mock struct {
	Fake     *radio.FakeD75
	Provider *ifsource.SyntheticProvider
	PortPath string // set by UsePTY

	mu    sync.Mutex
	state ifsource.TuneState
}

// NewMock wires a fake radio to a synthetic IF.
func NewMock(channels int, realtime bool) *Mock {
	m := &Mock{Fake: radio.NewFakeD75()}
	m.update(m.Fake.State())
	m.Fake.OnChange = m.update
	m.Provider = &ifsource.SyntheticProvider{
		Channels: channels,
		Scene:    MockScene,
		State:    m.State,
		Realtime: realtime,
	}
	return m
}

func (m *Mock) update(st radio.FakeState) {
	out := map[radio.IFOutput]string{radio.IOAF: "AF", radio.IOIF: "IF", radio.IODetect: "Detect"}[st.IFOutput]
	m.mu.Lock()
	m.state = ifsource.TuneState{DialHz: st.FrequencyHz, Mode: st.Mode.String(), Output: out}
	m.mu.Unlock()
}

// State is the synthetic IF's view of the radio.
func (m *Mock) State() ifsource.TuneState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// mockUSB is how the mock port describes itself.
var mockUSB = radio.USBDevice{VendorID: radio.KenwoodVendorID, ProductID: 0x0000, Vendor: "JVC KENWOOD (mock)", Product: "TH-D75 (mock)", Serial: "MOCK0001"}

// UsePTY puts the fake behind a pseudo-terminal, so the probe exercises the
// real serial code.
func (m *Mock) UsePTY() error {
	path, err := m.Fake.ServePTY()
	if err != nil {
		return err
	}
	m.PortPath = path
	return nil
}

// Apply points options at the mock. With a PTY the real serial opener is
// used; otherwise an in-memory pipe.
func (m *Mock) Apply(o *Options) {
	o.Mock = true
	o.Audio = m.Provider
	usb := mockUSB
	if m.PortPath != "" {
		usb.Ports = []string{m.PortPath}
		path := m.PortPath
		o.ListPorts = func() ([]radio.Port, error) { return []radio.Port{{Path: path, USB: &usb}}, nil }
		return
	}
	usb.Ports = []string{"mock:pipe"}
	o.ListPorts = func() ([]radio.Port, error) { return []radio.Port{{Path: "mock:pipe", USB: &usb}}, nil }
	var once sync.Once
	var conn io.ReadWriteCloser
	o.OpenPort = func(string, int) (io.ReadWriteCloser, error) {
		first := false
		once.Do(func() { conn, first = m.Fake.Pipe(), true })
		if !first {
			return nil, radio.ErrPortInUse // a second open is refused, like TIOCEXCL
		}
		return conn, nil
	}
}
