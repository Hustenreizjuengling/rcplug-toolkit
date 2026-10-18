//go:build windows

package flash

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// The RØDECaster's control interface is one vendor-defined HID collection
// (usage page 0xFF00, usage 1) with reports 1/2 (63 bytes) and 3/4 (255
// bytes), per the report descriptor the device installs
// (/etc/init.d/rc-hid-descriptor.bin). Windows gives each collection its own
// device path; on Linux the whole interface is one hidraw node.
const (
	controlUsagePage = 0xFF00
	controlUsage     = 0x0001
)

// hidInfo is what an HID interface tells about itself without I/O.
type hidInfo struct {
	vendor, product  uint16
	usagePage, usage uint16
	inLen, outLen    int
	name             string
}

// FindDevices returns every RØDECaster HID control interface present. It
// only queries the interfaces (opened without read or write access): it
// sends nothing. Matched by vendor (see DuoVendorID) and the control
// collection's usage.
func FindDevices() ([]Device, error) {
	if err := loadWinAPI(); err != nil {
		return nil, err
	}
	var guid syscall.GUID
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	ifs, err := interfaces(&guid)
	if err != nil {
		return nil, err
	}
	var devs []Device
	for _, it := range ifs {
		info, err := queryHID(it.path)
		if err != nil || info.vendor != DuoVendorID || info.usagePage != controlUsagePage || info.usage != controlUsage {
			continue
		}
		var bus uint16
		if strings.HasPrefix(strings.ToLower(it.path), `\\?\hid#vid_`) {
			bus = 0x0003 // enumerated from USB
		}
		d := Device{Node: it.path, Bus: bus, Vendor: info.vendor, Product: info.product, Raw: info.name}
		if ids, err := ancestorIDs(it.devInst); err == nil {
			d.USB = usbDevice(ids)
		}
		devs = append(devs, d)
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].Node < devs[j].Node })
	return devs, nil
}

func queryHID(path string) (hidInfo, error) {
	h, err := openPath(path, 0, 0)
	if err != nil {
		return hidInfo{}, err
	}
	defer syscall.CloseHandle(h)
	attr := hiddAttributes{}
	attr.Size = uint32(unsafe.Sizeof(attr))
	// HidD_* return BOOLEAN: only the low byte is defined.
	if r, _, e := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr))); byte(r) == 0 {
		return hidInfo{}, fmt.Errorf("HidD_GetAttributes: %w", lastErr(e))
	}
	info := hidInfo{vendor: attr.VendorID, product: attr.ProductID}
	if info.vendor != DuoVendorID {
		return info, nil
	}
	caps, err := hidCaps(h)
	if err != nil {
		return info, err
	}
	info.usagePage, info.usage = caps.UsagePage, caps.Usage
	info.inLen, info.outLen = int(caps.InputReportByteLength), int(caps.OutputReportByteLength)
	var name [128]uint16
	if r, _, _ := procHidDGetProductString.Call(uintptr(h), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2)); byte(r) != 0 {
		info.name = syscall.UTF16ToString(name[:])
	}
	return info, nil
}

func hidCaps(h syscall.Handle) (hidpCaps, error) {
	var pp uintptr
	if r, _, e := procHidDGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); byte(r) == 0 {
		return hidpCaps{}, fmt.Errorf("HidD_GetPreparsedData: %w", lastErr(e))
	}
	defer procHidDFreePreparsedData.Call(pp)
	var caps hidpCaps
	if st, _, _ := procHidPGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps))); uint32(st) != hidpStatusSuccess {
		return hidpCaps{}, fmt.Errorf("HidP_GetCaps: status %#x", uint32(st))
	}
	return caps, nil
}

// checkReportLengths checks an interface can carry report 1 and report 2.
func checkReportLengths(caps hidpCaps) error {
	if int(caps.OutputReportByteLength) < modeReportLen || int(caps.InputReportByteLength) < 2 {
		return fmt.Errorf("unexpected HID report sizes (in %d, out %d bytes): not the RØDECaster's control interface",
			caps.InputReportByteLength, caps.OutputReportByteLength)
	}
	return nil
}

var errIOTimeout = errors.New("timed out")

// command is rode_hid.send_command for Windows: open the interface, write
// output report 1 carrying cmd (padded to the interface's output report
// length, which Windows requires), and read input reports until report 2
// arrives or timeout passes; acked is whether it carries 'A'.
func command(node string, cmd byte, timeout time.Duration) (bool, error) {
	if err := loadWinAPI(); err != nil {
		return false, err
	}
	h, err := openPath(node, syscall.GENERIC_READ|syscall.GENERIC_WRITE, fileFlagOverlapped)
	if err != nil {
		return false, fmt.Errorf("opening the HID interface: %w", err)
	}
	defer syscall.CloseHandle(h)
	caps, err := hidCaps(h)
	if err != nil {
		return false, err
	}
	if err := checkReportLengths(caps); err != nil {
		return false, err
	}
	ev, err := createEvent()
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(ev)

	report := modeReport(cmd, int(caps.OutputReportByteLength))
	n, err := overlapped(h, ev, report, true, timeout)
	if err != nil {
		return false, fmt.Errorf("write: %w", err)
	}
	// The HID class driver fails a write it cannot send. The count it
	// returns varies (the buffer length, or under Wine the report without
	// its id), so only "nothing written" is an error.
	if n == 0 {
		return false, errors.New("nothing was written")
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, caps.InputReportByteLength)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return false, nil
		}
		n, err := overlapped(h, ev, buf, false, left)
		if errors.Is(err, errIOTimeout) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read: %w", err)
		}
		if isAck, acked := ackOf(buf[:n]); isAck {
			return acked, nil
		}
	}
}

// overlapped runs one ReadFile or WriteFile on h and waits up to timeout for
// it. On a timeout the request is cancelled, and waited for, before
// returning, so buf and the OVERLAPPED are no longer in use.
func overlapped(h, ev syscall.Handle, buf []byte, write bool, timeout time.Duration) (int, error) {
	var ov syscall.Overlapped
	ov.HEvent = ev
	var n uint32
	var err error
	if write {
		err = syscall.WriteFile(h, buf, &n, &ov)
	} else {
		err = syscall.ReadFile(h, buf, &n, &ov)
	}
	if err != nil && err != syscall.ERROR_IO_PENDING {
		return 0, err
	}
	if err == syscall.ERROR_IO_PENDING {
		ms := uint32(timeout / time.Millisecond)
		if ms == 0 {
			ms = 1
		}
		ret, werr := syscall.WaitForSingleObject(ev, ms)
		if ret != syscall.WAIT_OBJECT_0 {
			syscall.CancelIoEx(h, &ov)
			getOverlappedResult(h, &ov, &n, true)
			if ret == syscall.WAIT_TIMEOUT {
				return 0, errIOTimeout
			}
			return 0, fmt.Errorf("WaitForSingleObject: %v", werr)
		}
	}
	if err := getOverlappedResult(h, &ov, &n, false); err != nil {
		return 0, err
	}
	return int(n), nil
}
