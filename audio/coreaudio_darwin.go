//go:build darwin && cgo

package audio

/*
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework CoreFoundation
#include <stdlib.h>
#include "coreaudio.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"d75scope/ifsource"
)

// CoreAudio lists and opens Core Audio devices.
type CoreAudio struct{}

// New returns the Core Audio provider.
func New() *CoreAudio { return &CoreAudio{} }

// Devices lists every device with its formats.
func (*CoreAudio) Devices() ([]ifsource.Device, error) {
	n := int(C.d75_device_count())
	if n < 0 {
		return nil, errors.New("Core Audio: cannot list devices")
	}
	if n == 0 {
		return nil, nil
	}
	ids := make([]C.uint32_t, n)
	n = int(C.d75_device_ids(&ids[0], C.int(n)))
	if n < 0 {
		return nil, errors.New("Core Audio: cannot list devices")
	}
	def := C.d75_default_input()
	var devs []ifsource.Device
	for _, id := range ids[:n] {
		d := ifsource.Device{
			ID:             devString(id, 2),
			Name:           devString(id, 0),
			Manufacturer:   devString(id, 1),
			ModelUID:       devString(id, 3),
			Transport:      transportName(uint32(C.d75_transport(id))),
			InputChannels:  int(C.d75_channels(id, 1)),
			OutputChannels: int(C.d75_channels(id, 0)),
			NominalRate:    float64(C.d75_nominal_rate(id)),
			DefaultInput:   id == def,
		}
		var mins, maxs [32]C.double
		nr := int(C.d75_available_rates(id, &mins[0], &maxs[0], 32))
		for i := 0; i < nr; i++ {
			d.AvailableRates = append(d.AvailableRates, ifsource.RateRange{Min: float64(mins[i]), Max: float64(maxs[i])})
		}
		var fmts [16]C.d75_format
		nf := int(C.d75_input_formats(id, &fmts[0], 16))
		for _, f := range fmts[:nf] {
			d.InputFormats = append(d.InputFormats, ifsource.StreamFormat{
				SampleRate:     float64(f.sample_rate),
				Channels:       int(f.channels),
				BitsPerChannel: int(f.bits),
				Float:          f.is_float != 0,
				FormatID:       fourCC(uint32(f.format_id)),
			})
		}
		devs = append(devs, d)
	}
	return devs, nil
}

func devString(id C.uint32_t, which int) string {
	var buf [512]C.char
	if C.d75_device_string(id, C.int(which), &buf[0], 512) < 0 {
		return ""
	}
	return C.GoString(&buf[0])
}

func fourCC(v uint32) string {
	b := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	return string(b)
}

func transportName(t uint32) string {
	switch fourCC(t) {
	case "usb ":
		return "usb"
	case "bltn":
		return "builtin"
	case "blue", "blea":
		return "bluetooth"
	case "virt":
		return "virtual"
	case "grup":
		return "aggregate"
	case "airp":
		return "airplay"
	case "hdmi":
		return "hdmi"
	case "\x00\x00\x00\x00":
		return ""
	}
	return fourCC(t)
}

// Open starts capture from the device with this UID at its nominal rate and
// full input channel count.
func (p *CoreAudio) Open(id string) (ifsource.Source, error) {
	devs, err := p.Devices()
	if err != nil {
		return nil, err
	}
	var dev *ifsource.Device
	for i := range devs {
		if devs[i].ID == id {
			dev = &devs[i]
		}
	}
	if dev == nil {
		return nil, fmt.Errorf("no audio device with ID %q", id)
	}
	if dev.InputChannels == 0 {
		return nil, fmt.Errorf("audio device %q has no inputs", dev.Name)
	}
	cuid := C.CString(id)
	defer C.free(unsafe.Pointer(cuid))
	var status C.int32_t
	// Two seconds of ring buffer.
	c := C.d75_capture_start(cuid, C.double(dev.NominalRate), C.uint32_t(dev.InputChannels), C.uint32_t(2*dev.NominalRate), &status)
	if c == nil {
		return nil, fmt.Errorf("start capture on %q: OSStatus %d (%s)", dev.Name, int32(status), fourCC(uint32(status)))
	}
	return &capture{c: c, format: ifsource.Format{SampleRate: dev.NominalRate, Channels: dev.InputChannels}}, nil
}

type capture struct {
	mu     sync.Mutex
	c      *C.d75_capture
	format ifsource.Format
}

// ErrStalled means the device stopped delivering audio (unplugged, or the
// queue stopped).
var ErrStalled = errors.New("audio device stopped delivering samples")

func (s *capture) Format() ifsource.Format { return s.format }

func (s *capture) Read(buf []float32) (int, error) {
	got := 0
	last := time.Now()
	for got < len(buf) {
		s.mu.Lock()
		if s.c == nil {
			s.mu.Unlock()
			return got, errors.New("capture closed")
		}
		n := int(C.d75_capture_read(s.c, (*C.float)(unsafe.Pointer(&buf[got])), C.size_t(len(buf)-got)))
		s.mu.Unlock()
		if n > 0 {
			got += n
			last = time.Now()
			continue
		}
		if time.Since(last) > 2*time.Second {
			return got, ErrStalled
		}
		time.Sleep(3 * time.Millisecond)
	}
	return got, nil
}

func (s *capture) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		C.d75_capture_flush(s.c)
	}
}

func (s *capture) Overruns() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c == nil {
		return 0
	}
	return uint64(C.d75_capture_overruns(s.c))
}

func (s *capture) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		C.d75_capture_stop(s.c)
		s.c = nil
	}
	return nil
}
