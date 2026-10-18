// Package flash puts a prepared RØDECaster Duo firmware archive onto the
// device over USB.
//
// rcplug prepares the image (internal/cfw); this package checks everything
// beforehand and then runs the device's own update procedure, as published
// by nicewrld and implemented in rodecaster-duo-ssh (MIT, © Guid-Lab): send
// 'M' over HID, copy archive.tar.gz + archive.md5 onto the update disk the
// device then exposes, and — only when the caller asks — send 'U'.
//
//   - On Linux it runs the proven, vendored rodecaster-duo-ssh scripts
//     (internal/flash/scripts) unchanged; Go only detects the device (from
//     /sys/class/hidraw) and checks the inputs.
//   - On Windows those scripts cannot run (no hidraw, no /sys, no udisksctl),
//     so rcplug does the same steps itself through the Windows APIs: a direct
//     port of the scripts with the same two commands, reports and acks
//     (native.go, *_windows.go).
//
// The destructive flash command ('U', which overwrites a firmware partition
// and reboots) is never sent unless Options.Flash is set; callers (rcplug-flash,
// the setup GUI) confirm with the operator first. Preflight checks everything a
// run needs beforehand, without sending anything. On other systems FindDevices
// and Run return ErrUnsupportedOS.
//
// The device keeps two firmware partitions (A/B): a bad flash of one still
// boots from the other, which is the recovery path. See docs/enable-ssh.md.
package flash

import (
	"bufio"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// scriptFS holds the vendored flash tooling (MIT, © Guid-Lab/rodecaster-duo-ssh)
// and the udev rule. flash_cfw.py does "import rode_hid", so the two scripts
// are extracted into the same directory before running (see run_linux.go).
// Windows uses the Go port in native.go instead.
//
//go:embed scripts/rode_hid.py scripts/flash_cfw.py scripts/70-rode-hidraw.rules
var scriptFS embed.FS

// UdevRule returns the bundled udev rule that grants the logged-in user access
// to the RØDECaster HID node without root (vendor-wide, so it survives the
// product-id change on mode switches).
func UdevRule() []byte {
	b, _ := scriptFS.ReadFile("scripts/70-rode-hidraw.rules")
	return b
}

// DuoVendorID is the USB vendor id shared by RØDECaster devices. The product
// id is not matched: the RØDECaster changes it when multitrack mode is toggled,
// so detection is vendor-wide.
const DuoVendorID = 0x19F7

// File names the device expects on its update disk. They mirror what
// internal/cfw writes (cfw.StagedName / cfw.MD5Name); kept here as literals so
// this package carries no build dependency on cfw.
const (
	stagedName = "archive.tar.gz"
	md5Name    = "archive.md5"
)

var (
	// ErrUnsupportedOS is returned by device/flash operations on systems
	// other than Linux and Windows.
	ErrUnsupportedOS = errors.New("flashing is supported on Windows and Linux only (not on this system yet)")
	// ErrNoDevice is returned when no RØDECaster is found on USB.
	ErrNoDevice = errors.New("no RØDECaster found on USB (vendor 19f7); is it connected and powered on?")
)

// Device is a RØDECaster HID control interface found on the system.
type Device struct {
	Node    string `json:"node"`    // /dev/hidraw3 on Linux, a HID device path on Windows
	Bus     uint16 `json:"bus"`     // HID bus type (0003 = USB)
	Vendor  uint16 `json:"vendor"`  // 0x19f7 for RØDE
	Product uint16 `json:"product"` // varies with device/mode; informational only
	Raw     string `json:"raw"`     // the raw HID_ID line (Linux) or product name (Windows), for diagnostics
	// USB is the USB device the interface belongs to (Windows), e.g.
	// USB\VID_19F7&PID_004F\RD0000000; "" if unknown.
	USB string `json:"usb,omitempty"`
}

func (d Device) String() string {
	return fmt.Sprintf("%s (vendor %04x, product %04x)", d.Node, d.Vendor, d.Product)
}

// USB product ids of the RØDECaster models, one per USB mode (stereo,
// multitrack, update, ...), from the device's own gadget script
// /etc/init.d/uac2_gadget.sh in firmware 1.7.6. The id changes with the
// mode, but every mode of a model has its own, so it tells the model apart.
var (
	duoProductIDs  = map[uint16]bool{0x004f: true, 0x0050: true, 0x0079: true, 0x008e: true, 0x0093: true, 0x0095: true}
	pro2ProductIDs = map[uint16]bool{0x0030: true, 0x0037: true, 0x0078: true, 0x008d: true, 0x0092: true, 0x0094: true}
)

// Product ids of the "update" mode the device switches to on 'M' (same
// script): it shows only its update disk and the HID interface then.
var updateModeIDs = map[uint16]bool{0x004f: true, 0x0030: true}

// InUpdateMode reports whether d is in the update mode 'M' switches to, by
// its product id.
func (d Device) InUpdateMode() bool { return updateModeIDs[d.Product] }

// Model names the RØDECaster model d is by its product id: "duo", "pro2", or
// "" if the id is not known.
func (d Device) Model() string {
	switch {
	case duoProductIDs[d.Product]:
		return "duo"
	case pro2ProductIDs[d.Product]:
		return "pro2"
	}
	return ""
}

// parseHIDID parses a sysfs uevent HID_ID line, e.g.
//
//	HID_ID=0003:000019F7:00000095
//
// into its bus/vendor/product fields. ok is false for any other line.
func parseHIDID(line string) (bus, vendor, product uint16, ok bool) {
	line = strings.TrimSpace(line)
	const prefix = "HID_ID="
	if !strings.HasPrefix(line, prefix) {
		return 0, 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(line, prefix), ":")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	b, err1 := strconv.ParseUint(parts[0], 16, 64)
	v, err2 := strconv.ParseUint(parts[1], 16, 64)
	p, err3 := strconv.ParseUint(parts[2], 16, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return uint16(b), uint16(v), uint16(p), true
}

// Staging describes the prepared archive in a directory (the output of
// rcplug-cfw): the archive, its declared checksum file, the archive's actual
// MD5, and whether the two agree.
type Staging struct {
	Dir      string `json:"dir"`
	Archive  string `json:"archive"`  // path to archive.tar.gz
	MD5Path  string `json:"md5Path"`  // path to archive.md5
	Size     int64  `json:"size"`     // archive size in bytes
	Digest   string `json:"digest"`   // actual MD5 of the archive (hex)
	Declared string `json:"declared"` // MD5 named in archive.md5 (hex, lower-case), "" if unreadable
	MD5OK    bool   `json:"md5ok"`    // Declared == Digest
}

// CheckStaging verifies dir holds the files the device expects (archive.tar.gz
// and archive.md5), computes the archive's MD5 and compares it with the
// declared one. A missing file is an error; a checksum mismatch is reported in
// Staging.MD5OK (the device rejects an archive whose md5 does not match, so the
// caller should refuse to flash it).
func CheckStaging(dir string) (*Staging, error) {
	s := &Staging{
		Dir:     dir,
		Archive: filepath.Join(dir, stagedName),
		MD5Path: filepath.Join(dir, md5Name),
	}
	info, err := os.Stat(s.Archive)
	if err != nil {
		return nil, fmt.Errorf("missing %s: %w (prepare it first with rcplug-cfw --out %s)", stagedName, err, dir)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not the firmware archive", s.Archive)
	}
	s.Size = info.Size()
	if _, err := os.Stat(s.MD5Path); err != nil {
		return nil, fmt.Errorf("missing %s: %w", md5Name, err)
	}

	digest, err := md5File(s.Archive)
	if err != nil {
		return nil, err
	}
	s.Digest = digest
	if declared, err := readMD5Sidecar(s.MD5Path); err == nil {
		s.Declared = declared
		s.MD5OK = declared == digest
	}
	return s, nil
}

func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readMD5Sidecar returns the first 32-hex-digit field from an md5 file. The
// file may be a bare digest (RØDE's os.md5 style) or "digest  name".
func readMD5Sidecar(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != 32 {
		return "", fmt.Errorf("%s does not contain an md5 digest", path)
	}
	digest := strings.ToLower(fields[0])
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("%s: not a hex digest: %w", path, err)
	}
	return digest, nil
}

// FindPython returns a Python 3 interpreter to run the vendored flash scripts.
// override, if non-empty, is looked up and used as-is.
func FindPython(override string) (string, error) {
	candidates := []string{"python3", "python"}
	if override != "" {
		candidates = []string{override}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	if override != "" {
		return "", fmt.Errorf("python interpreter %q not found in PATH", override)
	}
	return "", errors.New("python3 not found in PATH (the vendored flash scripts need Python 3.8+)")
}

// matchVendor reads a uevent reader and returns the first HID_ID match for
// vendor. It is split out for testing.
func matchVendor(uevent io.Reader, vendor uint16) (bus, prod uint16, raw string, ok bool) {
	sc := bufio.NewScanner(uevent)
	for sc.Scan() {
		line := sc.Text()
		if b, v, p, parsed := parseHIDID(line); parsed && v == vendor {
			return b, p, strings.TrimSpace(line), true
		}
	}
	return 0, 0, "", false
}

// Options controls one stage/flash run.
type Options struct {
	StageDir string    // directory holding archive.tar.gz + archive.md5 (rcplug-cfw --out)
	Node     string    // HID interface to use (hidraw node, or device path on Windows); "" detects it
	Drive    string    // Windows: the update disk's drive letter; "" uses the new drive on the RØDECaster
	Python   string    // Linux: Python interpreter; "" auto-detects python3
	Flash    bool      // after staging, send 'U' to flash and reboot (destructive)
	NoPrompt bool      // no terminal to answer a sudo password prompt (the setup GUI)
	Stdout   io.Writer // script stdout (nil = os.Stdout)
	Stderr   io.Writer // script stderr (nil = os.Stderr)
}

// Plan is the resolved, checked set of inputs for a run: the staged archive
// and what it is, the device that will be flashed (zero when Options.Node
// overrides detection), the HID node used, the interpreter (Linux only), and
// the pre-flight warnings worth showing. Prepare fills it without sending any
// command to the device.
type Plan struct {
	Staging  *Staging
	Image    *fwimage.Info
	Device   Device
	Python   string
	Node     string
	Warnings []string
}

func orStdout(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}

func orStderr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}
