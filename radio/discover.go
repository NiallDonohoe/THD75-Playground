package radio

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// KenwoodVendorID is JVC KENWOOD's USB vendor ID, as the TH-D74 enumerated
// (CHIRP #4129). Expected, not verified, for the TH-D75.
const KenwoodVendorID = 0x2166

// USBDevice is one USB device and the serial device nodes under it.
type USBDevice struct {
	VendorID  uint16   `json:"vendor_id"`
	ProductID uint16   `json:"product_id"`
	Vendor    string   `json:"vendor"`
	Product   string   `json:"product"`
	Serial    string   `json:"serial"`
	Ports     []string `json:"ports"` // callout devices, e.g. /dev/cu.usbmodem1101
}

// Port is a candidate serial port.
type Port struct {
	Path string     `json:"path"`
	USB  *USBDevice `json:"usb,omitempty"`
}

// Match grades how sure we are that a port is the radio.
type Match int

const (
	MatchNone    Match = iota
	MatchGuess         // the only modem port, identity unknown
	MatchVendor        // JVC KENWOOD vendor ID
	MatchProduct       // JVC KENWOOD vendor ID and a TH-D75 product string
)

func (m Match) String() string {
	return [...]string{"none", "only modem port (identity unknown)", "JVC KENWOOD vendor ID", "JVC KENWOOD vendor ID and TH-D75 product string"}[m]
}

// ErrRadioNotFound means no candidate serial port was found.
var ErrRadioNotFound = errors.New("radio not found: no USB modem serial port (check the cable and Menu 980 = COM+AF/IF Output)")

// PickRadioPort chooses the most likely radio port. It never assumes a
// product ID; it matches the vendor ID and product string.
func PickRadioPort(ports []Port) (Port, Match, error) {
	best, bestMatch := Port{}, MatchNone
	for _, p := range ports {
		m := MatchNone
		if p.USB != nil && p.USB.VendorID == KenwoodVendorID {
			m = MatchVendor
			if strings.Contains(strings.ToUpper(p.USB.Product), "TH-D75") {
				m = MatchProduct
			}
		}
		if m > bestMatch {
			best, bestMatch = p, m
		}
	}
	if bestMatch == MatchNone && len(ports) == 1 {
		return ports[0], MatchGuess, nil
	}
	if bestMatch == MatchNone {
		if len(ports) == 0 {
			return Port{}, MatchNone, ErrRadioNotFound
		}
		paths := make([]string, len(ports))
		for i, p := range ports {
			paths[i] = p.Path
		}
		return Port{}, MatchNone, fmt.Errorf("several modem ports and none identifies as the radio: %s (choose one with --port)", strings.Join(paths, ", "))
	}
	return best, bestMatch, nil
}

// joinPorts attaches USB identities to device paths.
func joinPorts(paths []string, devs []USBDevice) []Port {
	var ports []Port
	for _, path := range paths {
		p := Port{Path: path}
		for i := range devs {
			if slices.Contains(devs[i].Ports, path) {
				d := devs[i]
				p.USB = &d
			}
		}
		ports = append(ports, p)
	}
	return ports
}

// --- macOS ioreg plist parsing (pure Go so it is testable anywhere) ---

// plistValue is a decoded property-list value: string, int64, bool,
// []plistValue or map[string]plistValue.
type plistValue any

// parsePlist decodes the XML property list that `ioreg -a` prints.
func parsePlist(r io.Reader) (plistValue, error) {
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, nil // no output: nothing matched
			}
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return decodePlistElement(dec, se)
		}
	}
}

func decodePlistElement(dec *xml.Decoder, se xml.StartElement) (plistValue, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]plistValue{}
		var key string
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := dec.DecodeElement(&k, &t); err != nil {
						return nil, err
					}
					key = k
					continue
				}
				v, err := decodePlistElement(dec, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		var a []plistValue
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := decodePlistElement(dec, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			case xml.EndElement:
				return a, nil
			}
		}
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "integer":
		var s string
		if err := dec.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 0, 64)
		if err != nil {
			// ioreg prints some unsigned 64-bit values that overflow int64.
			u, uerr := strconv.ParseUint(strings.TrimSpace(s), 0, 64)
			if uerr != nil {
				return nil, err
			}
			n = int64(u)
		}
		return n, nil
	default: // string, data, date, real: keep the text
		var s string
		if err := dec.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		return s, nil
	}
}

// usbDevicesFromIOReg walks `ioreg -r -c IOUSBHostDevice -l -a` output.
func usbDevicesFromIOReg(r io.Reader) ([]USBDevice, error) {
	root, err := parsePlist(r)
	if err != nil {
		return nil, fmt.Errorf("parse ioreg output: %w", err)
	}
	var list []plistValue
	switch v := root.(type) {
	case []plistValue:
		list = v
	case map[string]plistValue:
		list = []plistValue{v}
	}
	var devs []USBDevice
	for _, item := range list {
		m, ok := item.(map[string]plistValue)
		if !ok {
			continue
		}
		d := USBDevice{
			VendorID:  uint16(plistInt(m["idVendor"])),
			ProductID: uint16(plistInt(m["idProduct"])),
			Vendor:    plistString(m, "USB Vendor Name", "kUSBVendorString"),
			Product:   plistString(m, "USB Product Name", "kUSBProductString"),
			Serial:    plistString(m, "USB Serial Number", "kUSBSerialNumberString"),
		}
		collectCallouts(m, &d.Ports)
		slices.Sort(d.Ports)
		d.Ports = slices.Compact(d.Ports)
		devs = append(devs, d)
	}
	return devs, nil
}

func collectCallouts(v plistValue, out *[]string) {
	switch t := v.(type) {
	case map[string]plistValue:
		if s, ok := t["IOCalloutDevice"].(string); ok {
			*out = append(*out, s)
		}
		for _, child := range t {
			collectCallouts(child, out)
		}
	case []plistValue:
		for _, child := range t {
			collectCallouts(child, out)
		}
	}
}

func plistInt(v plistValue) int64 {
	n, _ := v.(int64)
	return n
}

func plistString(m map[string]plistValue, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
