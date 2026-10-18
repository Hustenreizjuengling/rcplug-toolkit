//go:build linux

package flash

import (
	"os"
	"path/filepath"
	"sort"
)

// FindDevices returns every RØDECaster HID control interface present, by
// reading /sys/class/hidraw/*/device/uevent. It is read-only: it touches no
// device and sends no command. Matched vendor-wide (see DuoVendorID).
func FindDevices() ([]Device, error) {
	entries, err := filepath.Glob("/sys/class/hidraw/hidraw*")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	var devs []Device
	for _, dir := range entries {
		f, err := os.Open(filepath.Join(dir, "device", "uevent"))
		if err != nil {
			continue
		}
		bus, prod, raw, ok := matchVendor(f, DuoVendorID)
		f.Close()
		if !ok {
			continue
		}
		devs = append(devs, Device{
			Node:    filepath.Join("/dev", filepath.Base(dir)),
			Bus:     bus,
			Vendor:  DuoVendorID,
			Product: prod,
			Raw:     raw,
		})
	}
	return devs, nil
}
