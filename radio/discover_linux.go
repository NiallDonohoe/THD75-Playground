package radio

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ListPorts returns /dev/ttyACM* ports with the USB identity from sysfs.
func ListPorts() ([]Port, error) {
	paths, err := filepath.Glob("/dev/ttyACM*")
	if err != nil {
		return nil, err
	}
	devs, _ := ListUSBDevices()
	return joinPorts(paths, devs), nil
}

// ListUSBDevices reads the USB identity of each ttyACM device from sysfs.
func ListUSBDevices() ([]USBDevice, error) {
	ttys, err := filepath.Glob("/sys/class/tty/ttyACM*")
	if err != nil {
		return nil, err
	}
	var devs []USBDevice
	for _, tty := range ttys {
		// device -> the USB interface; its parent is the USB device.
		usb, err := filepath.EvalSymlinks(filepath.Join(tty, "device", ".."))
		if err != nil {
			continue
		}
		read := func(name string) string {
			b, _ := os.ReadFile(filepath.Join(usb, name))
			return strings.TrimSpace(string(b))
		}
		vid, _ := strconv.ParseUint(read("idVendor"), 16, 16)
		pid, _ := strconv.ParseUint(read("idProduct"), 16, 16)
		devs = append(devs, USBDevice{
			VendorID: uint16(vid), ProductID: uint16(pid),
			Vendor: read("manufacturer"), Product: read("product"), Serial: read("serial"),
			Ports: []string{"/dev/" + filepath.Base(tty)},
		})
	}
	return devs, nil
}
