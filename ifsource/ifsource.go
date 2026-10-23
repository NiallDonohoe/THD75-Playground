// Package ifsource defines where the IF comes from: a USB audio device (the
// audio package, cgo), a WAV file, or the synthetic generator. Nothing here
// needs cgo, so tests run anywhere.
package ifsource

import (
	"errors"
	"strings"
)

// Format is the sample stream a Source delivers: interleaved float32.
type Format struct {
	SampleRate float64 `json:"sample_rate"`
	Channels   int     `json:"channels"`
}

// Source is a running IF stream.
type Source interface {
	Format() Format
	// Read blocks until buf is full of interleaved frames (len(buf) must be
	// a multiple of Channels) or the source fails.
	Read(buf []float32) (int, error)
	Close() error
}

// Flusher is implemented by sources that buffer: Flush discards whatever
// has been captured but not read, so the next Read starts from now.
type Flusher interface {
	Flush()
}

// Overruns is implemented by sources that can drop samples when the reader
// falls behind.
type Overruns interface {
	Overruns() uint64
}

// StreamFormat is one physical format of a device stream, as the OS reports it.
type StreamFormat struct {
	SampleRate     float64 `json:"sample_rate"`
	Channels       int     `json:"channels"`
	BitsPerChannel int     `json:"bits_per_channel"`
	Float          bool    `json:"float"`
	FormatID       string  `json:"format_id"` // e.g. "lpcm"
}

// RateRange is a supported nominal sample-rate range.
type RateRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Device is an audio device as the OS describes it.
type Device struct {
	ID             string         `json:"id"` // stable UID used to open it
	Name           string         `json:"name"`
	Manufacturer   string         `json:"manufacturer"`
	ModelUID       string         `json:"model_uid,omitempty"`
	Transport      string         `json:"transport,omitempty"` // e.g. "usb", "builtin"
	InputChannels  int            `json:"input_channels"`
	OutputChannels int            `json:"output_channels"`
	NominalRate    float64        `json:"nominal_rate"`
	AvailableRates []RateRange    `json:"available_rates,omitempty"`
	InputFormats   []StreamFormat `json:"input_formats,omitempty"`
	DefaultInput   bool           `json:"default_input,omitempty"`
}

// Provider lists and opens devices.
type Provider interface {
	Devices() ([]Device, error)
	// Open starts capture from the device with this ID at its nominal rate
	// and full input channel count. It never falls back to the default input.
	Open(id string) (Source, error)
}

// ErrNoRadioDevice means no audio input looks like the radio.
var ErrNoRadioDevice = errors.New("no TH-D75 audio input found (check Menu 980 = COM+AF/IF Output, and the cable)")

// PickRadioDevice finds the radio's USB audio input by its name, model UID
// or manufacturer, never by position or by being the default.
func PickRadioDevice(devs []Device) (Device, string, error) {
	type cand struct {
		d     Device
		score int
		why   string
	}
	var best cand
	for _, d := range devs {
		if d.InputChannels == 0 {
			continue
		}
		c := cand{d: d}
		switch {
		case containsFold(d.Name, "TH-D75") || containsFold(d.ModelUID, "TH-D75"):
			c.score, c.why = 3, "device name or model contains TH-D75"
		case containsFold(d.Manufacturer, "KENWOOD") || containsFold(d.ModelUID, "2166"):
			c.score, c.why = 2, "manufacturer is JVC KENWOOD"
		case containsFold(d.Name, "TH-D7"):
			c.score, c.why = 1, "device name contains TH-D7"
		}
		if c.score > best.score {
			best = c
		}
	}
	if best.score == 0 {
		return Device{}, "", ErrNoRadioDevice
	}
	return best.d, best.why, nil
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToUpper(s), strings.ToUpper(sub))
}
