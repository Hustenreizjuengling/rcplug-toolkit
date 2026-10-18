//go:build windows

package flash

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// Run stages the archive onto the device's update disk and, when o.Flash is
// set, triggers the flash. On Windows it runs the native port of the
// vendored scripts (native.go) through the Windows APIs below; the 'U' flash
// command is sent only when o.Flash is true (the caller confirms first).
// Progress goes to o.Stdout.
func Run(ctx context.Context, o Options) error {
	p, err := Prepare(o)
	if err != nil {
		return err
	}
	return nativeRun(ctx, winSys{}, o, p)
}

// winSys is nativeSys on Windows.
type winSys struct{}

func (winSys) findDevices() ([]Device, error) { return FindDevices() }

func (winSys) command(node string, cmd byte, timeout time.Duration) (bool, error) {
	return command(node, cmd, timeout)
}

func (winSys) now() time.Time        { return time.Now() }
func (winSys) sleep(d time.Duration) { time.Sleep(d) }

// volumes lists the local drives that hold a readable volume now. A drive
// without a medium (the Duo's update disk before 'M') is left out. The Go
// runtime sets SEM_FAILCRITICALERRORS, so an empty drive does not pop up a
// "no disk" dialog.
func (winSys) volumes() (map[string]volume, error) {
	if err := loadWinAPI(); err != nil {
		return nil, err
	}
	mask, _, e := procGetLogicalDrives.Call()
	if mask == 0 {
		return nil, fmt.Errorf("GetLogicalDrives: %w", lastErr(e))
	}
	vols := map[string]volume{}
	for i := 2; i < 26; i++ { // from C:, as A: and B: may be slow floppy drives
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if v, ok := volumeInfo(root); ok {
			vols[root] = v
		}
	}
	return vols, nil
}

func volumeInfo(root string) (volume, bool) {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return volume{}, false
	}
	if t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p))); t != driveRemovable && t != driveFixed {
		return volume{}, false // network, optical, RAM disk, ...
	}
	var label, fs [syscall.MAX_PATH + 1]uint16
	if r, _, _ := procGetVolumeInformationW.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)), 0, 0, 0,
		uintptr(unsafe.Pointer(&fs[0])), uintptr(len(fs))); r == 0 {
		return volume{}, false // no medium, or not ready
	}
	var free, total, totalFree uint64
	if r, _, _ := procGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree))); r == 0 {
		return volume{}, false
	}
	return volume{Root: root, Label: syscall.UTF16ToString(label[:]), FS: syscall.UTF16ToString(fs[:]),
		Size: total, Free: free}, true
}

// usbAncestors finds the disk the volume is on (by its storage device
// number) and returns the instance ids from that disk up the device tree:
// for the Duo's update disk USBSTOR\DISK&VEN_LINUX&..., then
// USB\VID_19F7&PID_004F&MI_00\..., then USB\VID_19F7&PID_004F\<serial>, ...
func (winSys) usbAncestors(v volume) ([]string, error) {
	if err := loadWinAPI(); err != nil {
		return nil, err
	}
	vh, err := openPath(`\\.\`+v.Root[:2], 0, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", v.Root[:2], err)
	}
	num, err := deviceNumber(vh)
	syscall.CloseHandle(vh)
	if err != nil {
		return nil, fmt.Errorf("device number of %s: %w", v.Root[:2], err)
	}
	disks, err := interfaces(&guidDevinterfaceDisk)
	if err != nil {
		return nil, err
	}
	for _, d := range disks {
		h, err := openPath(d.path, 0, 0)
		if err != nil {
			continue
		}
		n, err := deviceNumber(h)
		syscall.CloseHandle(h)
		if err == nil && n.DeviceType == num.DeviceType && n.DeviceNumber == num.DeviceNumber {
			return ancestorIDs(d.devInst)
		}
	}
	return nil, fmt.Errorf("no disk with device number %d found for %s", num.DeviceNumber, v.Root[:2])
}

// copyFile copies src to dst with FILE_FLAG_WRITE_THROUGH, so data and file
// system metadata go to the medium as they are written, then flushes it
// (FlushFileBuffers). This is the script's copy + sync. A partial file is
// removed.
func (winSys) copyFile(ctx context.Context, dst, src string, progress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	p, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_WRITE, 0, nil, syscall.CREATE_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagWriteThrough, 0)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	out := os.NewFile(uintptr(h), dst)
	err = copyStream(ctx, out, in, progress)
	if err == nil {
		if err = out.Sync(); err != nil {
			err = fmt.Errorf("flushing %s: %w", dst, err)
		}
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// release is the script's unmount: flush the volume, lock it and dismount
// it, so Windows lets go of it before the device takes the disk back.
// Without it Windows keeps the volume mounted until the device ejects the
// medium; the files are written through and flushed, so that is safe too. The
// lock is held until unlock is called; the native flow does that after 'U'.
// Locking needs the volume to be unused: Explorer, the search indexer or a
// virus scanner may hold it for a moment, so it is retried briefly. Opening
// a volume for writing may need administrator rights; without them this
// fails, which is fine, as the files are already written through.
func (winSys) release(v volume) (func(), error) {
	h, err := openPath(`\\.\`+v.Root[:2], syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0)
	if err != nil {
		return nil, err
	}
	syscall.FlushFileBuffers(h) // best effort: locking flushes as well
	var n uint32
	for i := 0; ; i++ {
		err = syscall.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &n, nil)
		if err == nil || i == 20 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("lock (still in use): %w", err)
	}
	if err := syscall.DeviceIoControl(h, fsctlDismountVolume, nil, 0, nil, 0, &n, nil); err != nil {
		syscall.DeviceIoControl(h, fsctlUnlockVolume, nil, 0, nil, 0, &n, nil)
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("dismount: %w", err)
	}
	return func() {
		syscall.DeviceIoControl(h, fsctlUnlockVolume, nil, 0, nil, 0, &n, nil)
		syscall.CloseHandle(h)
	}, nil
}
