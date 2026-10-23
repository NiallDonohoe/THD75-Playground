package radio

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
)

// ListPorts returns the USB modem serial ports (/dev/cu.usbmodem*) with the
// USB identity of the device each belongs to, from the I/O Registry.
func ListPorts() ([]Port, error) {
	paths, err := filepath.Glob("/dev/cu.usbmodem*")
	if err != nil {
		return nil, err
	}
	devs, err := ListUSBDevices()
	if err != nil {
		// Identity is a nicety; ports still work without it.
		return joinPorts(paths, nil), nil
	}
	return joinPorts(paths, devs), nil
}

// ListUSBDevices lists USB devices via ioreg, with their callout devices.
func ListUSBDevices() ([]USBDevice, error) {
	out, err := exec.Command("ioreg", "-r", "-c", "IOUSBHostDevice", "-l", "-a").Output()
	if err != nil {
		return nil, fmt.Errorf("ioreg: %w", err)
	}
	return usbDevicesFromIOReg(bytes.NewReader(out))
}
