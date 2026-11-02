// Package satdata fetches and caches what d75sat knows about satellites:
// orbits from CelesTrak and transmitters from the SatNOGS database
// (decision 0011). It joins the two into a catalogue of satellites the
// radio can hear: downlinks band B can tune (0.1–524 MHz), in modes it
// can demodulate.
package satdata

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"d75scope/orbit"
)

// Transmitter is one downlink worth listening to.
type Transmitter struct {
	ID          string  `json:"id"` // SatNOGS uuid
	Description string  `json:"description"`
	Downlink    float64 `json:"downlink"`              // Hz; the low edge of a passband
	DownlinkTop float64 `json:"downlinkTop,omitempty"` // Hz; the high edge, for transponders
	Uplink      float64 `json:"uplink,omitempty"`      // Hz, for reference: the program never sets it
	UplinkTop   float64 `json:"uplinkTop,omitempty"`   //
	Mode        string  `json:"mode"`                  // as SatNOGS gives it
	RadioMode   string  `json:"radioMode"`             // FM, USB, LSB or CW: what band B is set to
	Invert      bool    `json:"invert,omitempty"`      // an inverting transponder
	Baud        float64 `json:"baud,omitempty"`        //
	Kind        string  `json:"kind"`                  // Transmitter, Transceiver or Transponder
}

// Satellite is one entry in the catalogue.
type Satellite struct {
	ID           string         `json:"id"` // NORAD catalogue number
	Name         string         `json:"name"`
	Elements     orbit.Elements `json:"-"`
	Epoch        string         `json:"epoch"` // of the elements, RFC 3339
	Transmitters []Transmitter  `json:"transmitters"`
	SatNOGS      string         `json:"satnogs,omitempty"` // the satellite's page in the SatNOGS database
}

// Band B's receive range.
const (
	minDownlink = 0.1e6
	maxDownlink = 524e6
)

// satnogsTransmitter is the part of a SatNOGS DB transmitter record used.
type satnogsTransmitter struct {
	UUID         string   `json:"uuid"`
	Description  string   `json:"description"`
	Alive        bool     `json:"alive"`
	Type         string   `json:"type"`
	UplinkLow    *float64 `json:"uplink_low"`
	UplinkHigh   *float64 `json:"uplink_high"`
	DownlinkLow  *float64 `json:"downlink_low"`
	DownlinkHigh *float64 `json:"downlink_high"`
	Mode         string   `json:"mode"`
	Invert       bool     `json:"invert"`
	Baud         *float64 `json:"baud"`
	SatID        string   `json:"sat_id"`
	NoradCatID   *int     `json:"norad_cat_id"`
	Status       string   `json:"status"`
}

// radioMode says how band B should listen to a SatNOGS mode, or "" when
// the radio cannot demodulate it usefully (BPSK, LoRa, wideband modes).
func radioMode(mode string) string {
	m := strings.ToUpper(strings.TrimSpace(mode))
	switch {
	case m == "CW":
		return "CW"
	case m == "LSB":
		return "LSB"
	case m == "USB", m == "SSB":
		return "USB"
	case m == "FM", m == "FMN", m == "SSTV",
		strings.Contains(m, "AFSK"), strings.Contains(m, "FSK"), strings.Contains(m, "MSK"):
		// FM voice, and FM data heard as audio.
		return "FM"
	}
	return ""
}

// parseTransmitters reads SatNOGS's /api/transmitters/ JSON and keeps the
// transmitters the radio can hear, by catalogue number.
func parseTransmitters(data []byte) (map[string][]Transmitter, map[string]string, error) {
	var recs []satnogsTransmitter
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, nil, fmt.Errorf("satnogs: %v", err)
	}
	out := map[string][]Transmitter{}
	pages := map[string]string{}
	for _, r := range recs {
		if !r.Alive || r.Status != "active" || r.NoradCatID == nil || r.DownlinkLow == nil {
			continue
		}
		dl := *r.DownlinkLow
		rm := radioMode(r.Mode)
		if rm == "" || dl < minDownlink || dl > maxDownlink {
			continue
		}
		t := Transmitter{ID: r.UUID, Description: r.Description, Downlink: dl, Mode: r.Mode,
			RadioMode: rm, Invert: r.Invert, Kind: r.Type}
		if r.DownlinkHigh != nil && *r.DownlinkHigh > dl && *r.DownlinkHigh <= maxDownlink {
			t.DownlinkTop = *r.DownlinkHigh
		}
		if r.UplinkLow != nil {
			t.Uplink = *r.UplinkLow
		}
		if r.UplinkHigh != nil {
			t.UplinkTop = *r.UplinkHigh
		}
		if r.Baud != nil {
			t.Baud = *r.Baud
		}
		id := fmt.Sprint(*r.NoradCatID)
		out[id] = append(out[id], t)
		if r.SatID != "" {
			pages[id] = "https://db.satnogs.org/satellite/" + r.SatID
		}
	}
	for _, ts := range out {
		sort.Slice(ts, func(i, j int) bool { return ts[i].Downlink < ts[j].Downlink })
	}
	return out, pages, nil
}

// Join builds the catalogue: each element set that has at least one
// transmitter the radio can hear, in name order.
func Join(els []orbit.Elements, txs map[string][]Transmitter, pages map[string]string) []Satellite {
	var out []Satellite
	for _, e := range els {
		ts := txs[e.Catalog]
		if len(ts) == 0 {
			continue
		}
		out = append(out, Satellite{ID: e.Catalog, Name: e.Name, Elements: e,
			Epoch: e.Epoch.Format("2006-01-02T15:04:05Z"), Transmitters: ts, SatNOGS: pages[e.Catalog]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
