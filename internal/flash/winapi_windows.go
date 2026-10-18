//go:build windows

package flash

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// The Windows APIs the native flow uses beyond package syscall. rcplug uses
// only the standard library, so the few calls it needs are bound here. The
// DLLs are loaded from the system directory by absolute path, so a hid.dll
// next to rcplug-setup.exe is never picked up instead.

var (
	procCreateEventW,
	procGetOverlappedResult,
	procGetLogicalDrives,
	procGetDriveTypeW,
	procGetVolumeInformationW,
	procGetDiskFreeSpaceExW,
	procHidDGetHidGuid,
	procHidDGetAttributes,
	procHidDGetPreparsedData,
	procHidDFreePreparsedData,
	procHidDGetProductString,
	procHidPGetCaps,
	procSetupDiGetClassDevsW,
	procSetupDiEnumDeviceInterfaces,
	procSetupDiGetDeviceInterfaceDetailW,
	procSetupDiDestroyDeviceInfoList,
	procCMGetParent,
	procCMGetDeviceIDW *syscall.Proc
)

var winProcs = []struct {
	dll, name string
	p         **syscall.Proc
}{
	{"kernel32.dll", "CreateEventW", &procCreateEventW},
	{"kernel32.dll", "GetOverlappedResult", &procGetOverlappedResult},
	{"kernel32.dll", "GetLogicalDrives", &procGetLogicalDrives},
	{"kernel32.dll", "GetDriveTypeW", &procGetDriveTypeW},
	{"kernel32.dll", "GetVolumeInformationW", &procGetVolumeInformationW},
	{"kernel32.dll", "GetDiskFreeSpaceExW", &procGetDiskFreeSpaceExW},
	{"hid.dll", "HidD_GetHidGuid", &procHidDGetHidGuid},
	{"hid.dll", "HidD_GetAttributes", &procHidDGetAttributes},
	{"hid.dll", "HidD_GetPreparsedData", &procHidDGetPreparsedData},
	{"hid.dll", "HidD_FreePreparsedData", &procHidDFreePreparsedData},
	{"hid.dll", "HidD_GetProductString", &procHidDGetProductString},
	{"hid.dll", "HidP_GetCaps", &procHidPGetCaps},
	{"setupapi.dll", "SetupDiGetClassDevsW", &procSetupDiGetClassDevsW},
	{"setupapi.dll", "SetupDiEnumDeviceInterfaces", &procSetupDiEnumDeviceInterfaces},
	{"setupapi.dll", "SetupDiGetDeviceInterfaceDetailW", &procSetupDiGetDeviceInterfaceDetailW},
	{"setupapi.dll", "SetupDiDestroyDeviceInfoList", &procSetupDiDestroyDeviceInfoList},
	{"cfgmgr32.dll", "CM_Get_Parent", &procCMGetParent},
	{"cfgmgr32.dll", "CM_Get_Device_IDW", &procCMGetDeviceIDW},
}

var (
	winOnce sync.Once
	winErr  error
)

// loadWinAPI loads the procedures above once; every Windows entry point
// calls it first.
func loadWinAPI() error {
	winOnce.Do(func() { winErr = loadWinProcs() })
	return winErr
}

func loadWinProcs() error {
	dir, err := systemDirectory()
	if err != nil {
		return err
	}
	dlls := map[string]*syscall.DLL{}
	for _, w := range winProcs {
		d := dlls[w.dll]
		if d == nil {
			if d, err = syscall.LoadDLL(dir + `\` + w.dll); err != nil {
				return err
			}
			dlls[w.dll] = d
		}
		if *w.p, err = d.FindProc(w.name); err != nil {
			return err
		}
	}
	return nil
}

// systemDirectory returns e.g. C:\Windows\System32. kernel32.dll is one of
// the DLLs Go itself loads from there.
func systemDirectory() (string, error) {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW")
	buf := make([]uint16, syscall.MAX_PATH+1)
	n, _, e := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n >= uintptr(len(buf)) {
		return "", fmt.Errorf("GetSystemDirectory: %w", lastErr(e))
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// lastErr turns the error a Proc.Call returns into one worth reporting: a
// zero errno (the call failed without setting one) becomes EINVAL.
func lastErr(e error) error {
	var en syscall.Errno
	if errors.As(e, &en) && en == 0 {
		return syscall.EINVAL
	}
	return e
}

const (
	digcfPresent         = 0x02
	digcfDeviceInterface = 0x10

	errorNoMoreItems        = syscall.Errno(259)
	errorInsufficientBuffer = syscall.Errno(122)

	hidpStatusSuccess = 0x00110000
	crSuccess         = 0
	maxDeviceIDLen    = 200 // MAX_DEVICE_ID_LEN

	driveRemovable = 2
	driveFixed     = 3

	fileFlagOverlapped   = 0x40000000
	fileFlagWriteThrough = 0x80000000

	fsctlLockVolume             = 0x00090018
	fsctlUnlockVolume           = 0x0009001c
	fsctlDismountVolume         = 0x00090020
	ioctlStorageGetDeviceNumber = 0x002d1080
)

// guidDevinterfaceDisk is GUID_DEVINTERFACE_DISK.
var guidDevinterfaceDisk = syscall.GUID{Data1: 0x53f56307, Data2: 0xb6bf, Data3: 0x11d0,
	Data4: [8]byte{0x94, 0xf2, 0x00, 0xa0, 0xc9, 0x1e, 0xfb, 0x8b}}

// spDeviceInterfaceData is SP_DEVICE_INTERFACE_DATA.
type spDeviceInterfaceData struct {
	CbSize             uint32
	InterfaceClassGuid syscall.GUID
	Flags              uint32
	Reserved           uintptr
}

// spDevinfoData is SP_DEVINFO_DATA.
type spDevinfoData struct {
	CbSize    uint32
	ClassGuid syscall.GUID
	DevInst   uint32
	Reserved  uintptr
}

// detailCbSize is sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W): a DWORD and one
// WCHAR, 8 bytes with 64-bit alignment and 6 packed on 32-bit Windows.
func detailCbSize() uint32 {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return 8
	}
	return 6
}

// hiddAttributes is HIDD_ATTRIBUTES.
type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

// hidpCaps is HIDP_CAPS.
type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

// storageDeviceNumber is STORAGE_DEVICE_NUMBER.
type storageDeviceNumber struct {
	DeviceType      uint32
	DeviceNumber    uint32
	PartitionNumber uint32
}

// devInterface is one present device interface and the device it belongs to.
type devInterface struct {
	path    string
	devInst uint32
}

// interfaces lists the present device interfaces of a class.
func interfaces(class *syscall.GUID) ([]devInterface, error) {
	h, _, e := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(class)), 0, 0, digcfPresent|digcfDeviceInterface)
	if syscall.Handle(h) == syscall.InvalidHandle {
		return nil, fmt.Errorf("SetupDiGetClassDevs: %w", lastErr(e))
	}
	defer procSetupDiDestroyDeviceInfoList.Call(h)

	var out []devInterface
	for i := uint32(0); ; i++ {
		ifd := spDeviceInterfaceData{}
		ifd.CbSize = uint32(unsafe.Sizeof(ifd))
		r, _, e := procSetupDiEnumDeviceInterfaces.Call(h, 0, uintptr(unsafe.Pointer(class)), uintptr(i), uintptr(unsafe.Pointer(&ifd)))
		if r == 0 {
			if errors.Is(e, errorNoMoreItems) {
				return out, nil
			}
			return out, fmt.Errorf("SetupDiEnumDeviceInterfaces: %w", lastErr(e))
		}
		var need uint32
		r, _, e = procSetupDiGetDeviceInterfaceDetailW.Call(h, uintptr(unsafe.Pointer(&ifd)), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
		if (r == 0 && !errors.Is(e, errorInsufficientBuffer)) || need <= 4 {
			continue
		}
		buf := make([]uint64, (need+7)/8) // 8-byte aligned, as the structure needs
		*(*uint32)(unsafe.Pointer(&buf[0])) = detailCbSize()
		did := spDevinfoData{}
		did.CbSize = uint32(unsafe.Sizeof(did))
		r, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(h, uintptr(unsafe.Pointer(&ifd)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(need), 0, uintptr(unsafe.Pointer(&did)))
		if r == 0 {
			continue
		}
		// DevicePath follows the DWORD cbSize.
		path := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(&buf[0]), 4)), (need-4)/2)
		out = append(out, devInterface{path: syscall.UTF16ToString(path), devInst: did.DevInst})
	}
}

// openPath opens a device or file path, shared for reading and writing.
// access 0 opens it for queries only, which works even while another program
// has it open.
func openPath(path string, access, flags uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return syscall.CreateFile(p, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, flags, 0)
}

// deviceNumber returns the storage device number of an open disk or volume.
func deviceNumber(h syscall.Handle) (storageDeviceNumber, error) {
	var n storageDeviceNumber
	var got uint32
	err := syscall.DeviceIoControl(h, ioctlStorageGetDeviceNumber, nil, 0,
		(*byte)(unsafe.Pointer(&n)), uint32(unsafe.Sizeof(n)), &got, nil)
	return n, err
}

// deviceID returns the device instance id of a device node, e.g.
// USB\VID_19F7&PID_004F\RD0000000.
func deviceID(inst uint32) (string, error) {
	var buf [maxDeviceIDLen + 1]uint16
	r, _, _ := procCMGetDeviceIDW.Call(uintptr(inst), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if uint32(r) != crSuccess {
		return "", fmt.Errorf("CM_Get_Device_ID: CONFIGRET %d", r)
	}
	return syscall.UTF16ToString(buf[:]), nil
}

// ancestorIDs returns the instance ids of inst and its parents, nearest first.
func ancestorIDs(inst uint32) ([]string, error) {
	var ids []string
	for i := 0; i < 16; i++ {
		id, err := deviceID(inst)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
		var parent uint32
		if r, _, _ := procCMGetParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(inst), 0); uint32(r) != crSuccess {
			break // the root of the device tree
		}
		inst = parent
	}
	return ids, nil
}

func createEvent() (syscall.Handle, error) {
	h, _, e := procCreateEventW.Call(0, 1, 0, 0) // manual reset, not signalled
	if h == 0 {
		return 0, fmt.Errorf("CreateEvent: %w", lastErr(e))
	}
	return syscall.Handle(h), nil
}

func getOverlappedResult(h syscall.Handle, ov *syscall.Overlapped, n *uint32, wait bool) error {
	var w uintptr
	if wait {
		w = 1
	}
	r, _, e := procGetOverlappedResult.Call(uintptr(h), uintptr(unsafe.Pointer(ov)), uintptr(unsafe.Pointer(n)), w)
	if r == 0 {
		return lastErr(e)
	}
	return nil
}
