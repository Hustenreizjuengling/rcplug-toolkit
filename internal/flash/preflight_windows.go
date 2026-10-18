//go:build windows

package flash

import (
	"fmt"
	"math/bits"
	"strings"
	"syscall"
	"unsafe"
)

// Supported reports whether this system can flash: on Windows it can, with
// the native port (native.go).
func Supported() error { return nil }

// hostChecks checks the HID interface opens for reading and writing, that
// no RØDE app is running, and the drive letter for the update disk.
func (r *Report) hostChecks(o Options) {
	if r.Node != "" {
		r.add(accessCheck(r.Node))
	}
	r.add(appCheck())
	r.add(driveCheck(o.Drive))
}

func accessCheck(node string) Check {
	if err := loadWinAPI(); err != nil {
		return fail("access", err)
	}
	h, err := openPath(node, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0)
	if err != nil {
		return fail("access", fmt.Errorf("cannot open the device for reading and writing (%v): close the RØDECaster App and other programs that use it, then check again", err))
	}
	defer syscall.CloseHandle(h)
	caps, err := hidCaps(h)
	if err != nil {
		return fail("access", err)
	}
	if err := checkReportLengths(caps); err != nil {
		return fail("access", err)
	}
	return pass("access", fmt.Sprintf("opens for reading and writing (reports: in %d, out %d bytes)",
		caps.InputReportByteLength, caps.OutputReportByteLength))
}

// appCheck refuses to go on while RØDE's own app runs: it talks to the same
// HID interface and reacts to the device's update mode.
func appCheck() Check {
	names, err := runningRodeApps()
	if err != nil {
		return Check{ID: "app", OK: true, Warn: true,
			Detail: "could not list running programs (" + err.Error() + "); make sure the RØDECaster App is closed"}
	}
	if len(names) > 0 {
		return fail("app", fmt.Errorf("%s is running: close it first (also in the notification area), as it uses the same USB interface and could interfere with the update mode", strings.Join(names, ", ")))
	}
	return pass("app", "no RØDE app running")
}

func runningRodeApps() ([]string, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snap)
	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var names []string
	seen := map[string]bool{}
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		name := syscall.UTF16ToString(pe.ExeFile[:])
		if isRodeApp(name) && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, nil
}

// driveCheck says how the update disk will be found. Windows mounts it by
// itself once the device exposes it; it needs a free drive letter, unless
// the device's empty drive already has one.
func driveCheck(drive string) Check {
	if drive != "" {
		root, ok := normDrive(drive)
		if !ok {
			return fail("mount", fmt.Errorf("%q is not a drive letter", drive))
		}
		return Check{ID: "mount", OK: true, Warn: true,
			Detail: "update disk given as " + root + "; rcplug will not check that it is on the RØDECaster"}
	}
	if err := loadWinAPI(); err != nil {
		return fail("mount", err)
	}
	mask, _, _ := procGetLogicalDrives.Call()
	free := 24 - bits.OnesCount32(uint32(mask)>>2) // C: to Z:
	return pass("mount", fmt.Sprintf("Windows mounts it after 'M'; rcplug uses the new FAT drive on the Duo in update mode (%d drive letters free)", free))
}
