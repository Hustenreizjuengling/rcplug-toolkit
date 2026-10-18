package flash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The native flow is a Go port of the vendored flash_cfw.py and rode_hid.py
// (rodecaster-duo-ssh, Copyright (c) 2026 Guid-Lab, MIT License; see
// scripts/LICENSE) for systems where those scripts cannot run. Windows has no hidraw, no
// /sys/block and no udisksctl, so there rcplug talks to the HID interface and
// the update disk through the Windows APIs itself (*_windows.go). The port
// keeps the scripts' protocol and order of steps:
//
//  1. send 'M' as output report 1 and wait for the 'A' ack on input report 2
//  2. wait for the update disk to appear (Windows gives it a drive letter)
//  3. copy archive.tar.gz + archive.md5 onto it, flushed to the medium
//  4. dismount it (best effort, like the script's unmount)
//  5. only with Options.Flash: send 'U' the same way, and the device flashes
//     and reboots
//
// It checks a few things the scripts leave out, because on Windows the update
// disk is just the next drive letter. The new drive must be FAT, have room for
// the archive and (unless the operator names it) sit on a RØDECaster Duo in
// update mode: its USB device has RØDE's vendor id and the Duo's update-mode
// product id. Before 'U' it looks up the HID interface again: in update mode
// the device comes back with that product id, which on Windows means a new
// device path; only the Duo in update mode, on the same USB device as the
// update disk where Windows tells, gets 'U'.

// Protocol, as in rode_hid.py.
const (
	modeReportID   = 0x01 // output report carrying a one-byte mode command
	ackReportID    = 0x02 // input report carrying the acknowledgement
	ackByte        = 'A'  // 0x41
	cmdEnterUpdate = 'M'  // 0x4d: expose the update disk
	cmdFlash       = 'U'  // 0x55: flash the staged archive and reboot
	// modeReportLen is report 1 with its id: 63 bytes in the Duo's HID report
	// descriptor, 64 with the id, as rode_hid.py sends it.
	modeReportLen = 64
)

const (
	ackTimeout = 5 * time.Second // rode_hid.ACK_TIMEOUT_S
	// diskTimeout is longer than the script's 20 s: the first time, Windows
	// also sets up the device under its update-mode product id.
	diskTimeout   = 90 * time.Second
	deviceTimeout = 30 * time.Second
	pollInterval  = 500 * time.Millisecond
	settleDelay   = time.Second // let the new disk settle, as the script does
)

// modeReport returns output report 1 carrying cmd, zero-padded to n bytes.
// On Windows n must be the interface's output report length: a write has to
// be exactly that long, which for the Duo is 256 (its longest output report,
// 3, has 255 bytes). n is at least modeReportLen; callers check that.
func modeReport(cmd byte, n int) []byte {
	b := make([]byte, n)
	b[0], b[1] = modeReportID, cmd
	return b
}

// ackOf looks at an input report: isAck is whether it is report 2, the
// acknowledgement report, and acked whether it carries 'A'. Other reports
// (the device's property notifications on report 4) are not an answer.
func ackOf(rep []byte) (isAck, acked bool) {
	if len(rep) < 2 || rep[0] != ackReportID {
		return false, false
	}
	return true, rep[1] == ackByte
}

// volume is a mounted volume as the native flow sees it.
type volume struct {
	Root  string // drive root, ending in a separator, e.g. `E:\`
	Label string
	FS    string // file system name, e.g. FAT, FAT32, exFAT
	Size  uint64 // total bytes
	Free  uint64 // bytes free for the caller
}

func (v volume) String() string {
	label := v.Label
	if label == "" {
		label = "no label"
	}
	return fmt.Sprintf("%s (%s, %s, %d MB, %d MB free)", v.Root, label, v.FS, v.Size>>20, v.Free>>20)
}

// isFAT reports whether v has a FAT file system (FAT, FAT32), as the Duo's
// update disk does: the device formats it with mkfs.vfat after 'M'. exFAT is
// not accepted; the device uses it for its other storage.
func (v volume) isFAT() bool {
	return strings.HasPrefix(strings.ToUpper(v.FS), "FAT")
}

// nativeSys is what the native flow needs from the operating system. The
// Windows implementation is in run_windows.go; tests use a fake.
type nativeSys interface {
	findDevices() ([]Device, error)
	// command sends cmd as output report 1 to the HID interface at node and
	// waits up to timeout for input report 2; acked is whether it said 'A'.
	command(node string, cmd byte, timeout time.Duration) (acked bool, err error)
	// volumes returns the volumes that are mounted and readable now, by root.
	volumes() (map[string]volume, error)
	// usbAncestors returns the device instance ids from the disk behind v up
	// to the root of the device tree, nearest first.
	usbAncestors(v volume) ([]string, error)
	// copyFile copies src to dst, written through and flushed to the medium.
	copyFile(ctx context.Context, dst, src string, progress func(done int64)) error
	// release flushes v and dismounts it, keeping it locked until unlock is
	// called. It is best effort: the files are already flushed.
	release(v volume) (unlock func(), err error)
	now() time.Time
	sleep(time.Duration)
}

// usbDevice returns the USB device in a device instance id chain (see
// usbAncestors): the first USB\VID_... id that is not one interface of a
// composite device (&MI_xx), e.g. USB\VID_19F7&PID_004F\RD0000000. Both the
// update disk and the HID interface of one device have it as an ancestor.
func usbDevice(ids []string) string {
	for _, id := range ids {
		up := strings.ToUpper(id)
		if strings.HasPrefix(up, `USB\VID_`) && !strings.Contains(up, "&MI_") {
			return id
		}
	}
	return ""
}

// usbIDs reads vendor and product id from a USB\VID_xxxx&PID_yyyy... id.
func usbIDs(id string) (vendor, product uint16, ok bool) {
	up := strings.ToUpper(id)
	const pre = `USB\VID_`
	if !strings.HasPrefix(up, pre) || len(up) < len(pre)+4+5+4 || up[len(pre)+4:len(pre)+9] != "&PID_" {
		return 0, 0, false
	}
	v, err1 := strconv.ParseUint(up[len(pre):len(pre)+4], 16, 16)
	p, err2 := strconv.ParseUint(up[len(pre)+9:len(pre)+13], 16, 16)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return uint16(v), uint16(p), true
}

// updateDiskOwner says whether a disk's instance id chain belongs to a
// RØDECaster Duo in update mode, and returns that USB device's id; detail
// names the USB device found instead, for messages.
func updateDiskOwner(ids []string) (usb string, ok bool, detail string) {
	dev := usbDevice(ids)
	if dev == "" {
		if len(ids) > 0 {
			return "", false, ids[0]
		}
		return "", false, "an unknown device"
	}
	v, p, parsed := usbIDs(dev)
	if d := (Device{Vendor: v, Product: p}); parsed && v == DuoVendorID && d.Model() == "duo" && d.InUpdateMode() {
		return dev, true, dev
	}
	return "", false, dev
}

// isRodeApp reports whether a process image name is one of RØDE's desktop
// apps that talk to the RØDECaster over USB (RODECasterApp.exe, RØDE Central).
func isRodeApp(exe string) bool {
	n := strings.TrimSuffix(strings.ToLower(exe), ".exe")
	n = strings.NewReplacer(" ", "", "-", "", "_", "", "ø", "o").Replace(n)
	return n == "rodecasterapp" || n == "rodecaster" || n == "rodecentral"
}

// normDrive turns "e", "E:", `e:\` or "E:/" into `E:\`. ok is false for
// anything that is not a drive letter from C: to Z: (A: and B: are floppy
// letters, which the flow does not look at).
func normDrive(s string) (root string, ok bool) {
	s = strings.TrimRight(strings.TrimSpace(s), `\/`)
	s = strings.TrimSuffix(s, ":")
	if len(s) != 1 {
		return "", false
	}
	c := s[0] &^ 0x20 // upper case
	if c < 'C' || c > 'Z' {
		return "", false
	}
	return string(c) + `:\`, true
}

// nativeRun stages the archive the plan checked onto the device and, with
// o.Flash, flashes it. The caller has confirmed o.Flash with the operator.
func nativeRun(ctx context.Context, sys nativeSys, o Options, p *Plan) error {
	out := orStdout(o.Stdout)
	node := p.Node
	fmt.Fprintf(out, "device: %s\n", node)

	before, err := sys.volumes()
	if err != nil {
		return fmt.Errorf("listing drives: %w", err)
	}
	fmt.Fprintf(out, "drives before: %s\n", rootList(before))
	if err := ctx.Err(); err != nil {
		return err
	}
	fmt.Fprintln(out, "sending 'M' (enter update mode)...")
	acked, err := sys.command(node, cmdEnterUpdate, ackTimeout)
	if err != nil {
		return fmt.Errorf("sending 'M': %w", err)
	}
	if !acked {
		return errors.New("no ack for 'M'; aborting")
	}

	disk, diskUSB, err := waitForUpdateDisk(ctx, sys, out, before, o.Drive)
	if err != nil {
		return err
	}
	if diskUSB != "" {
		fmt.Fprintf(out, "update disk: %s on %s\n", disk, diskUSB)
	} else {
		fmt.Fprintf(out, "update disk: %s (given, not checked)\n", disk)
	}
	need := p.Staging.Size + md5SidecarMax
	if disk.Free < uint64(need) {
		return fmt.Errorf("the update disk %s has %d MB free, the archive needs %d MB; nothing was copied or flashed",
			disk.Root, disk.Free>>20, need>>20)
	}

	fmt.Fprintf(out, "copying archive onto %s...\n", disk.Root)
	if err := stageFiles(ctx, sys, out, disk, p.Staging); err != nil {
		return fmt.Errorf("copying onto the update disk: %w; nothing was flashed", err)
	}
	unlock, err := sys.release(disk)
	if err != nil {
		fmt.Fprintf(out, "note: could not dismount %s (%v); the files are flushed to it, continuing\n", disk.Root, err)
		unlock = func() {}
	} else {
		fmt.Fprintf(out, "flushed and dismounted %s\n", disk.Root)
	}
	defer unlock()
	fmt.Fprintln(out, "archive staged on device.")

	if !o.Flash {
		fmt.Fprintln(out, "\nstaged only (no flash); nothing was flashed.")
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w; archive left staged, nothing was flashed", err)
	}
	node, err = findAgain(ctx, sys, out, node, diskUSB)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "sending 'U' (flash + reboot)...")
	acked, err = sys.command(node, cmdFlash, ackTimeout)
	if err != nil {
		return fmt.Errorf("sending 'U': %w", err)
	}
	fmt.Fprintf(out, "flash command ack=%s\n", yesNo(acked))
	if !acked {
		return errors.New("no ack for 'U'")
	}
	return nil
}

// md5SidecarMax is room kept for archive.md5 next to the archive.
const md5SidecarMax = 4096

// waitForUpdateDisk waits for the volume the device exposes after 'M' and
// returns it with the id of the USB device it is on ("" when the operator
// named the drive). As in flash_cfw.py it looks for a volume that was not
// there before; one that goes away and comes back counts as new too, since
// the device re-creates the disk on every 'M'. Each new volume is judged once
// (see judgeVolume), or again while that failed for a passing reason; others
// are reported and left alone.
func waitForUpdateDisk(ctx context.Context, sys nativeSys, out io.Writer, before map[string]volume, drive string) (volume, string, error) {
	known := make(map[string]bool, len(before))
	for r := range before {
		known[r] = true
	}
	want := ""
	if drive != "" {
		r, ok := normDrive(drive)
		if !ok {
			return volume{}, "", fmt.Errorf("%q is not a drive letter (C: to Z:)", drive)
		}
		want = r
	}
	var ignored []string
	noted := map[string]bool{}
	deadline := sys.now().Add(diskTimeout)
	for {
		now, err := sys.volumes()
		if err != nil {
			return volume{}, "", fmt.Errorf("listing drives: %w", err)
		}
		for r := range known {
			if _, ok := now[r]; !ok {
				delete(known, r)
			}
		}
		var found []volume
		var owners []string
		for _, r := range sortedRoots(now) {
			if known[r] {
				continue
			}
			v := now[r]
			owner, reason, retry := judgeVolume(sys, v, want)
			if !retry {
				known[r] = true
			}
			if reason != "" {
				if !noted[r+reason] {
					noted[r+reason] = true
					fmt.Fprintf(out, "ignoring new drive %s: %s\n", v.Root, reason)
					ignored = append(ignored, v.Root+" ("+reason+")")
				}
				continue
			}
			found = append(found, v)
			owners = append(owners, owner)
		}
		switch {
		case len(found) == 1:
			sys.sleep(settleDelay)
			return found[0], owners[0], nil
		case len(found) > 1:
			return volume{}, "", fmt.Errorf("several new drives look like the RØDECaster's update disk (%s); disconnect other drives and try again — nothing was copied or flashed", rootList(toMap(found)))
		}
		if !sys.now().Before(deadline) {
			msg := "update disk did not appear; aborting"
			if len(ignored) > 0 {
				msg += " (new drives seen but not used: " + strings.Join(ignored, "; ") + ")"
			}
			return volume{}, "", errors.New(msg)
		}
		if err := ctx.Err(); err != nil {
			return volume{}, "", err
		}
		sys.sleep(pollInterval)
	}
}

// judgeVolume says why a new volume is not the update disk (reason), or
// returns the USB device it is on (owner; "" when the operator named it with
// want, as then the USB check is skipped: they said which drive it is). retry
// is set when the reason may pass, so the volume is looked at again.
func judgeVolume(sys nativeSys, v volume, want string) (owner, reason string, retry bool) {
	if want != "" && !strings.EqualFold(v.Root, want) {
		return "", "not the drive given (" + want + ")", false
	}
	if !v.isFAT() {
		return "", "its file system is " + v.FS + ", the update disk is FAT", false
	}
	if want != "" {
		return "", "", false
	}
	ids, err := sys.usbAncestors(v)
	if err != nil {
		return "", "cannot tell which device it belongs to: " + err.Error(), true
	}
	usb, ok, detail := updateDiskOwner(ids)
	if !ok {
		return "", "it belongs to " + detail + ", not to a RØDECaster Duo in update mode", false
	}
	return usb, "", false
}

// stageFiles copies archive.tar.gz and then archive.md5 to the root of the
// update disk, the names the device expects, reporting progress in steps of
// 10 %.
func stageFiles(ctx context.Context, sys nativeSys, out io.Writer, disk volume, st *Staging) error {
	if err := copyLogged(ctx, sys, out, disk.Root+stagedName, st.Archive, st.Size); err != nil {
		return err
	}
	return copyLogged(ctx, sys, out, disk.Root+md5Name, st.MD5Path, 0)
}

func copyLogged(ctx context.Context, sys nativeSys, out io.Writer, dst, src string, size int64) error {
	next := int64(10)
	err := sys.copyFile(ctx, dst, src, func(done int64) {
		for size > 0 && next <= 100 && done*100 >= next*size {
			fmt.Fprintf(out, "  %3d%%  %d of %d MB\n", next, done>>20, size>>20)
			next += 10
		}
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "copied %s\n", dst)
	return nil
}

// copyStream copies r to w in 1 MiB chunks, calling progress with the bytes
// done so far and stopping when ctx ends.
func copyStream(ctx context.Context, w io.Writer, r io.Reader, progress func(int64)) error {
	buf := make([]byte, 1<<20)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// findAgain returns the HID interface to send 'U' to: the same one if it is
// still there, else the one RØDECaster Duo in update mode (on the same USB
// device as the update disk, where both are known). The device re-enumerates
// when it enters update mode (another product id); the scripts simply reopen
// the same hidraw node, which on Linux usually gets the same name again.
func findAgain(ctx context.Context, sys nativeSys, out io.Writer, node, diskUSB string) (string, error) {
	deadline := sys.now().Add(deviceTimeout)
	for {
		devs, err := sys.findDevices()
		if err == nil {
			for _, d := range devs {
				if strings.EqualFold(d.Node, node) {
					fmt.Fprintf(out, "device for 'U': %s (unchanged)\n", d)
					return d.Node, nil
				}
			}
			var cands []Device
			for _, d := range devs {
				if d.Model() != "duo" || !d.InUpdateMode() {
					continue // another RØDE device: never flash it
				}
				if diskUSB != "" && d.USB != "" && !strings.EqualFold(d.USB, diskUSB) {
					continue // a Duo, but not the one with the update disk
				}
				cands = append(cands, d)
			}
			switch len(cands) {
			case 1:
				fmt.Fprintf(out, "device in update mode: %s\n", cands[0])
				return cands[0].Node, nil
			case 0:
			default:
				return "", fmt.Errorf("%d RØDECaster Duos in update mode found, cannot tell which one to flash; archive left staged, nothing was flashed", len(cands))
			}
		}
		if !sys.now().Before(deadline) {
			msg := "the Duo's HID interface did not come back in update mode after 'M'"
			if err != nil {
				msg += " (" + err.Error() + ")"
			}
			return "", errors.New(msg + "; archive left staged, nothing was flashed")
		}
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%w; archive left staged, nothing was flashed", err)
		}
		sys.sleep(pollInterval)
	}
}

func sortedRoots(m map[string]volume) []string {
	roots := make([]string, 0, len(m))
	for r := range m {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	return roots
}

func rootList(m map[string]volume) string {
	if len(m) == 0 {
		return "none"
	}
	return strings.Join(sortedRoots(m), " ")
}

func toMap(vs []volume) map[string]volume {
	m := make(map[string]volume, len(vs))
	for _, v := range vs {
		m[v.Root] = v
	}
	return m
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "NO"
}
