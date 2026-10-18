//go:build windows

package flash

import (
	"testing"
	"unsafe"
)

// The structures passed to the Windows APIs must have their C sizes.
func TestWinStructSizes(t *testing.T) {
	ptr64 := unsafe.Sizeof(uintptr(0)) == 8
	pick := func(on64, on32 uintptr) uintptr {
		if ptr64 {
			return on64
		}
		return on32
	}
	for name, c := range map[string]struct{ got, want uintptr }{
		"SP_DEVICE_INTERFACE_DATA": {unsafe.Sizeof(spDeviceInterfaceData{}), pick(32, 28)},
		"SP_DEVINFO_DATA":          {unsafe.Sizeof(spDevinfoData{}), pick(32, 28)},
		"HIDD_ATTRIBUTES":          {unsafe.Sizeof(hiddAttributes{}), 12},
		"HIDP_CAPS":                {unsafe.Sizeof(hidpCaps{}), 64},
		"STORAGE_DEVICE_NUMBER":    {unsafe.Sizeof(storageDeviceNumber{}), 12},
	} {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d, want %d", name, c.got, c.want)
		}
	}
	if got := detailCbSize(); got != uint32(pick(8, 6)) {
		t.Errorf("detailCbSize = %d", got)
	}
}

// The APIs load and enumerate here without a device: this reads only.
func TestWinEnumerate(t *testing.T) {
	if _, err := FindDevices(); err != nil {
		t.Fatalf("FindDevices: %v", err)
	}
	vols, err := winSys{}.volumes()
	if err != nil {
		t.Fatalf("volumes: %v", err)
	}
	if len(vols) == 0 {
		t.Fatal("no volumes, not even the system drive")
	}
	for _, v := range vols {
		t.Logf("%s", v)
	}
}
