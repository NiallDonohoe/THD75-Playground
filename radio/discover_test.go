package radio

import (
	"strings"
	"testing"
)

// A trimmed `ioreg -r -c IOUSBHostDevice -l -a` with a radio-like device
// (structure as macOS prints it; identity values are the expected ones, not
// captured from a TH-D75) and an unrelated hub.
const ioregFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
	<dict>
		<key>IOObjectClass</key><string>IOUSBHostDevice</string>
		<key>USB Product Name</key><string>TH-D75</string>
		<key>USB Vendor Name</key><string>JVC KENWOOD</string>
		<key>USB Serial Number</key><string>C1234567</string>
		<key>idVendor</key><integer>8550</integer>
		<key>idProduct</key><integer>24591</integer>
		<key>sessionID</key><integer>18446744073709551615</integer>
		<key>Built-In</key><false/>
		<key>IORegistryEntryChildren</key>
		<array>
			<dict>
				<key>IOObjectClass</key><string>AppleUSBACMData</string>
				<key>IORegistryEntryChildren</key>
				<array>
					<dict>
						<key>IOObjectClass</key><string>IOSerialBSDClient</string>
						<key>IOCalloutDevice</key><string>/dev/cu.usbmodem1101</string>
						<key>IODialinDevice</key><string>/dev/tty.usbmodem1101</string>
					</dict>
				</array>
			</dict>
			<dict>
				<key>IOObjectClass</key><string>AppleUSBAudioDevice</string>
				<key>Data</key><data>AAEC</data>
			</dict>
		</array>
	</dict>
	<dict>
		<key>USB Product Name</key><string>USB2.0 Hub</string>
		<key>idVendor</key><integer>1452</integer>
		<key>idProduct</key><integer>4097</integer>
	</dict>
</array>
</plist>
`

func TestIORegParse(t *testing.T) {
	devs, err := usbDevicesFromIOReg(strings.NewReader(ioregFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("got %d devices", len(devs))
	}
	d := devs[0]
	if d.VendorID != 0x2166 || d.ProductID != 0x600F || d.Product != "TH-D75" || d.Vendor != "JVC KENWOOD" || d.Serial != "C1234567" {
		t.Errorf("device = %+v", d)
	}
	if len(d.Ports) != 1 || d.Ports[0] != "/dev/cu.usbmodem1101" {
		t.Errorf("ports = %v", d.Ports)
	}
	if devs, err := usbDevicesFromIOReg(strings.NewReader("")); err != nil || len(devs) != 0 {
		t.Errorf("empty ioreg: %v, %v", devs, err)
	}
}

func TestPickRadioPort(t *testing.T) {
	devs, _ := usbDevicesFromIOReg(strings.NewReader(ioregFixture))
	ports := joinPorts([]string{"/dev/cu.usbmodem9901", "/dev/cu.usbmodem1101"}, devs)
	p, m, err := PickRadioPort(ports)
	if err != nil || p.Path != "/dev/cu.usbmodem1101" || m != MatchProduct {
		t.Errorf("pick = %+v, %v, %v", p, m, err)
	}

	// Same vendor, different product string (a D74, say): vendor match only.
	d74 := []USBDevice{{VendorID: 0x2166, Product: "TH-D74", Ports: []string{"/dev/cu.usbmodem1"}}}
	if _, m, _ := PickRadioPort(joinPorts([]string{"/dev/cu.usbmodem1"}, d74)); m != MatchVendor {
		t.Errorf("D74 match = %v", m)
	}
	// One unidentified port: a guess, verified later by ID.
	if p, m, err := PickRadioPort([]Port{{Path: "/dev/cu.usbmodemX"}}); err != nil || m != MatchGuess || p.Path != "/dev/cu.usbmodemX" {
		t.Errorf("single port: %+v %v %v", p, m, err)
	}
	if _, _, err := PickRadioPort(nil); err != ErrRadioNotFound {
		t.Errorf("no ports: %v", err)
	}
	if _, _, err := PickRadioPort([]Port{{Path: "/dev/a"}, {Path: "/dev/b"}}); err == nil {
		t.Error("two unidentified ports picked one")
	}
}
