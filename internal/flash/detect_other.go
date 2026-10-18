//go:build !linux && !windows

package flash

// FindDevices is unsupported here: rcplug enumerates HID interfaces only on
// Linux (/sys/class/hidraw) and Windows. See ErrUnsupportedOS.
func FindDevices() ([]Device, error) {
	return nil, ErrUnsupportedOS
}
