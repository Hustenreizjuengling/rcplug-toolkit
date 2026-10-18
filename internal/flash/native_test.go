package flash

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSys plays the device and Windows for the native flow: 'M' makes the
// update disk appear a little later (and, like the real Duo, the HID
// interface come back under another product id), 'U' is recorded. Time is
// fake, so the flow's timeouts pass instantly.
type fakeSys struct {
	t      *testing.T
	clock  time.Time
	devs   []Device
	vols   map[string]volume   // mounted now
	dirs   map[string]string   // root -> directory backing it
	owners map[string][]string // root -> device instance ids up the tree

	nack        map[byte]bool // commands the device does not ack
	afterM      func(f *fakeSys)
	pending     []pendingVol
	sent        []string // node + command, in order
	events      []string // command and release/unlock order
	releaseErr  error
	copyErr     error
	onProgress  func()
	volumesSeen int
	ancestorErr int // usbAncestors fails this many times first
}

type pendingVol struct {
	at  time.Time
	v   volume
	ids []string
}

const (
	duoNode    = `\\?\hid#vid_19f7&pid_0095&mi_09#8&1&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	updateNode = `\\?\hid#vid_19f7&pid_004f&mi_01#8&2&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
)

var duoIDs = []string{
	`USBSTOR\DISK&VEN_LINUX&PROD_FILE-STOR_GADGET&REV_0510\RD0000000&0`,
	`USB\VID_19F7&PID_004F&MI_00\8&2&0&0000`,
	`USB\VID_19F7&PID_004F\RD0000000`,
	`USB\ROOT_HUB30\4&1&0&0`,
}

func newFakeSys(t *testing.T) *fakeSys {
	return &fakeSys{
		t:      t,
		clock:  time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		devs:   []Device{{Node: duoNode, Bus: 3, Vendor: DuoVendorID, Product: 0x0095}},
		vols:   map[string]volume{`C:\`: {Root: `C:\`, FS: "NTFS", Size: 500 << 30, Free: 100 << 30}},
		dirs:   map[string]string{},
		owners: map[string][]string{`C:\`: {`SCSI\DISK&VEN_NVME\1`, `PCI\VEN_144D&DEV_A808\3`}},
		nack:   map[byte]bool{},
	}
}

// updateDisk returns the Duo's update disk as Windows shows it: a FAT image
// of 560000 sectors without a label.
func (f *fakeSys) updateDisk(root string) volume {
	return volume{Root: root, FS: "FAT", Size: 560000 * 512, Free: 280 << 20}
}

// appear makes v mount after d, backed by a fresh directory.
func (f *fakeSys) appear(d time.Duration, v volume, ids []string) {
	f.pending = append(f.pending, pendingVol{at: f.clock.Add(d), v: v, ids: ids})
}

// duoBehaviour is the real device's answer to 'M': its HID interface comes
// back as the update-mode device and the update disk mounts as E: 3 s later.
func duoBehaviour(f *fakeSys) {
	f.devs = []Device{{Node: updateNode, Bus: 3, Vendor: DuoVendorID, Product: 0x004f, USB: duoIDs[2]}}
	f.appear(3*time.Second, f.updateDisk(`E:\`), duoIDs)
}

func (f *fakeSys) findDevices() ([]Device, error) { return f.devs, nil }

func (f *fakeSys) command(node string, cmd byte, timeout time.Duration) (bool, error) {
	f.sent = append(f.sent, node+" "+string(cmd))
	f.events = append(f.events, string(cmd))
	if cmd == cmdEnterUpdate && f.afterM != nil {
		f.afterM(f)
	}
	return !f.nack[cmd], nil
}

func (f *fakeSys) volumes() (map[string]volume, error) {
	f.volumesSeen++
	var rest []pendingVol
	for _, p := range f.pending {
		if f.clock.Before(p.at) {
			rest = append(rest, p)
			continue
		}
		f.vols[p.v.Root] = p.v
		f.owners[p.v.Root] = p.ids
		f.dirs[p.v.Root] = f.t.TempDir()
	}
	f.pending = rest
	out := make(map[string]volume, len(f.vols))
	for r, v := range f.vols {
		out[r] = v
	}
	return out, nil
}

func (f *fakeSys) usbAncestors(v volume) ([]string, error) {
	if f.ancestorErr > 0 {
		f.ancestorErr--
		return nil, errors.New("CM_Get_Device_ID: CONFIGRET 13")
	}
	ids, ok := f.owners[v.Root]
	if !ok {
		return nil, errors.New("no such disk")
	}
	return ids, nil
}

func (f *fakeSys) copyFile(ctx context.Context, dst, src string, progress func(int64)) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	root, name := dst[:3], dst[3:]
	dir, ok := f.dirs[root]
	if !ok {
		f.t.Fatalf("copy to %s, which is not a mounted fake volume", dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer out.Close()
	return copyStream(ctx, out, in, func(n int64) {
		if f.onProgress != nil {
			f.onProgress()
		}
		progress(n)
	})
}

func (f *fakeSys) release(v volume) (func(), error) {
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	f.events = append(f.events, "release "+v.Root)
	return func() { f.events = append(f.events, "unlock "+v.Root) }, nil
}

func (f *fakeSys) now() time.Time        { return f.clock }
func (f *fakeSys) sleep(d time.Duration) { f.clock = f.clock.Add(d) }

// stagedPlan prepares archive.tar.gz + archive.md5 and the plan a run gets.
func stagedPlan(t *testing.T, body []byte) *Plan {
	t.Helper()
	dir := t.TempDir()
	writeStaging(t, dir, body, digestOf(body)+"\n")
	st, err := CheckStaging(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &Plan{Staging: st, Node: duoNode}
}

func runNative(t *testing.T, f *fakeSys, o Options, p *Plan) (string, error) {
	t.Helper()
	var out bytes.Buffer
	o.Stdout = &out
	err := nativeRun(context.Background(), f, o, p)
	return out.String(), err
}

func staged(t *testing.T, f *fakeSys, root, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dirs[root], name))
	if err != nil {
		t.Fatalf("%s%s: %v", root, name, err)
	}
	return b
}

func TestNativeStageOnly(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	body := bytes.Repeat([]byte("firmware"), 1<<17) // 1 MiB
	p := stagedPlan(t, body)

	log, err := runNative(t, f, Options{}, p)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	if got := strings.Join(f.sent, ","); got != duoNode+" M" {
		t.Fatalf("sent %s, want only 'M'", got)
	}
	if !bytes.Equal(staged(t, f, `E:\`, stagedName), body) {
		t.Fatal("archive.tar.gz on the update disk differs from the staged one")
	}
	if got := string(staged(t, f, `E:\`, md5Name)); got != digestOf(body)+"\n" {
		t.Fatalf("archive.md5 = %q", got)
	}
	if got := strings.Join(f.events, ","); got != `M,release E:\,unlock E:\` {
		t.Fatalf("events = %s", got)
	}
	for _, want := range []string{"sending 'M'", `update disk: E:\ (no label, FAT, 273 MB, 280 MB free) on USB\VID_19F7&PID_004F\RD0000000`, "100%", "archive staged on device.", "staged only"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// The flash goes to the HID interface as it is in update mode, and the
// update disk stays locked until 'U' has been sent.
func TestNativeFlash(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	log, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	if got := strings.Join(f.sent, ","); got != duoNode+" M,"+updateNode+" U" {
		t.Fatalf("sent %s", got)
	}
	if got := strings.Join(f.events, ","); got != `M,release E:\,U,unlock E:\` {
		t.Fatalf("events = %s", got)
	}
	if !strings.Contains(log, "device in update mode: "+updateNode) || !strings.Contains(log, "flash command ack=yes") {
		t.Fatalf("log:\n%s", log)
	}
}

func TestNativeNoAckForM(t *testing.T) {
	f := newFakeSys(t)
	f.nack[cmdEnterUpdate] = true
	_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "no ack for 'M'") {
		t.Fatalf("err = %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %v after a missing ack", f.sent)
	}
}

func TestNativeNoAckForU(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	f.nack[cmdFlash] = true
	log, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "no ack for 'U'") || !strings.Contains(log, "ack=NO") {
		t.Fatalf("err = %v\n%s", err, log)
	}
}

// Another drive plugged in meanwhile is reported and left alone.
func TestNativeIgnoresOtherDrives(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		stick := volume{Root: `D:\`, Label: "STICK", FS: "FAT32", Size: 16 << 30, Free: 8 << 30}
		f.appear(time.Second, stick, []string{`USBSTOR\DISK&VEN_SANDISK\1`, `USB\VID_0781&PID_5581\4C53`})
		duoBehaviour(f)
	}
	log, err := runNative(t, f, Options{}, stagedPlan(t, []byte("fw")))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	if !strings.Contains(log, `ignoring new drive D:\: it belongs to USB\VID_0781&PID_5581\4C53, not to a RØDECaster Duo in update mode`) {
		t.Fatalf("log:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(f.dirs[`D:\`], stagedName)); err == nil {
		t.Fatal("the archive was copied onto the other drive")
	}
	staged(t, f, `E:\`, stagedName)
}

func TestNativeDiskNeverAppears(t *testing.T) {
	f := newFakeSys(t)
	start := f.clock
	f.afterM = func(f *fakeSys) {
		f.appear(time.Second, volume{Root: `F:\`, FS: "NTFS", Size: 1 << 40, Free: 1 << 39}, duoIDs)
	}
	_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "update disk did not appear") || !strings.Contains(err.Error(), "the update disk is FAT") {
		t.Fatalf("err = %v", err)
	}
	if waited := f.clock.Sub(start); waited < diskTimeout {
		t.Fatalf("gave up after %v, want %v", waited, diskTimeout)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %v", f.sent)
	}
}

// With --drive the operator names the update disk: the USB check is skipped
// (here it could not succeed), the FAT check is not.
func TestNativeDriveGiven(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		f.appear(2*time.Second, f.updateDisk(`G:\`), nil)
	}
	log, err := runNative(t, f, Options{Drive: "g"}, stagedPlan(t, []byte("fw")))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	staged(t, f, `G:\`, stagedName)

	if _, err := runNative(t, newFakeSys(t), Options{Drive: "nope"}, stagedPlan(t, []byte("fw"))); err == nil || !strings.Contains(err.Error(), "not a drive letter") {
		t.Fatalf("err = %v", err)
	}
}

func TestNativeNotEnoughSpace(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		v := f.updateDisk(`E:\`)
		v.Free = 1 << 20
		f.appear(time.Second, v, duoIDs)
	}
	_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, bytes.Repeat([]byte{1}, 2<<20)))
	if err == nil || !strings.Contains(err.Error(), "has 1 MB free") {
		t.Fatalf("err = %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %v", f.sent)
	}
}

func TestNativeDeviceDoesNotComeBack(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		duoBehaviour(f)
		f.devs = nil
	}
	_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "did not come back") || !strings.Contains(err.Error(), "nothing was flashed") {
		t.Fatalf("err = %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %v", f.sent)
	}
	staged(t, f, `E:\`, stagedName)
}

// A disk that is there before 'M' (the device was already in update mode)
// goes away when the device re-creates it, and its return counts.
func TestNativeDiskComesBack(t *testing.T) {
	f := newFakeSys(t)
	f.vols[`E:\`] = f.updateDisk(`E:\`)
	f.afterM = func(f *fakeSys) {
		delete(f.vols, `E:\`)
		f.appear(2*time.Second, f.updateDisk(`E:\`), duoIDs)
	}
	if log, err := runNative(t, f, Options{}, stagedPlan(t, []byte("fw"))); err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	staged(t, f, `E:\`, stagedName)
}

// Dismounting is best effort, like the script's unmount.
func TestNativeReleaseFails(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	f.releaseErr = errors.New("Access is denied.")
	log, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	if !strings.Contains(log, `note: could not dismount E:\ (Access is denied.)`) || len(f.sent) != 2 {
		t.Fatalf("sent %v, log:\n%s", f.sent, log)
	}
}

func TestNativeCopyFails(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	f.copyErr = errors.New("device not ready")
	_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "device not ready; nothing was flashed") || len(f.sent) != 1 {
		t.Fatalf("err = %v, sent %v", err, f.sent)
	}
}

// Stopping (Ctrl-C) while copying never leads to 'U'.
func TestNativeCancelled(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onProgress = cancel
	err := nativeRun(ctx, f, Options{Flash: true, Stdout: &bytes.Buffer{}}, stagedPlan(t, bytes.Repeat([]byte{1}, 3<<20)))
	if !errors.Is(err, context.Canceled) || len(f.sent) != 1 {
		t.Fatalf("err = %v, sent %v", err, f.sent)
	}
}

func TestModeReportAndAck(t *testing.T) {
	r := modeReport(cmdEnterUpdate, 256)
	if len(r) != 256 || r[0] != 0x01 || r[1] != 0x4d || !bytes.Equal(r[2:], make([]byte, 254)) {
		t.Fatalf("modeReport = % x", r[:4])
	}
	for _, c := range []struct {
		rep          []byte
		isAck, acked bool
	}{
		{[]byte{0x02, 'A', 0}, true, true},
		{[]byte{0x02, 'N'}, true, false},
		{[]byte{0x04, 'A'}, false, false}, // a property notification, not the answer
		{[]byte{0x02}, false, false},
		{nil, false, false},
	} {
		if isAck, acked := ackOf(c.rep); isAck != c.isAck || acked != c.acked {
			t.Errorf("ackOf(% x) = %v, %v", c.rep, isAck, acked)
		}
	}
}

func TestUpdateDiskOwner(t *testing.T) {
	if usb, ok, _ := updateDiskOwner(duoIDs); !ok || usb != duoIDs[2] {
		t.Fatalf("Duo update disk: %v %s", ok, usb)
	}
	for name, ids := range map[string][]string{
		"stick":               {`USBSTOR\DISK&VEN_SANDISK\1`, `USB\VID_0781&PID_5581\4C53`, `USB\ROOT_HUB30\1`},
		"Duo in normal mode":  {`USBSTOR\DISK&VEN_LINUX\1`, `USB\VID_19F7&PID_0095&MI_04\1`, `USB\VID_19F7&PID_0095\RD1`},
		"Pro II update mode":  {`USBSTOR\DISK&VEN_LINUX\1`, `USB\VID_19F7&PID_0030\RD2`},
		"longer vendor id":    {`USB\VID_19F70&PID_004F\1`},
		"interface node only": {`USB\VID_19F7&PID_004F&MI_00\1`},
		"no USB at all":       {`SCSI\DISK&VEN_NVME\1`},
		"nothing":             nil,
	} {
		if usb, ok, detail := updateDiskOwner(ids); ok || usb != "" || detail == "" {
			t.Errorf("%s: owner %q, ok %v, detail %q", name, usb, ok, detail)
		}
	}
	if v, p, ok := usbIDs(`usb\vid_19f7&pid_004f\x`); !ok || v != 0x19f7 || p != 0x004f {
		t.Fatalf("usbIDs = %#x %#x %v", v, p, ok)
	}
}

// 'U' goes only to the Duo in update mode on the update disk's USB device:
// never to another RØDE device that happens to be there.
func TestNativeFlashesOnlyTheDuoInUpdateMode(t *testing.T) {
	pro2 := Device{Node: `\\?\hid#vid_19f7&pid_0037#1`, Vendor: DuoVendorID, Product: 0x0037, USB: `USB\VID_19F7&PID_0037\RD9`}
	otherDuo := Device{Node: `\\?\hid#vid_19f7&pid_004f#2`, Vendor: DuoVendorID, Product: 0x004f, USB: `USB\VID_19F7&PID_004F\RD7`}
	for name, devs := range map[string][]Device{
		"only a Pro II":           {pro2},
		"a Duo on another device": {otherDuo},
	} {
		f := newFakeSys(t)
		f.afterM = func(f *fakeSys) {
			duoBehaviour(f)
			f.devs = devs
		}
		_, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw")))
		if err == nil || !strings.Contains(err.Error(), "did not come back in update mode") || len(f.sent) != 1 {
			t.Fatalf("%s: err = %v, sent %v", name, err, f.sent)
		}
	}
	// With the right one there too, that one is flashed.
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		duoBehaviour(f)
		f.devs = append([]Device{pro2, otherDuo}, f.devs...)
	}
	if log, err := runNative(t, f, Options{Flash: true}, stagedPlan(t, []byte("fw"))); err != nil || f.sent[1] != updateNode+" U" {
		t.Fatalf("err = %v, sent %v\n%s", err, f.sent, log)
	}
}

// exFAT and a RØDE disk in normal mode are not the update disk.
func TestNativeRefusesOtherRodeDisks(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = func(f *fakeSys) {
		f.appear(time.Second, volume{Root: `F:\`, Label: "RodeCaster", FS: "exFAT", Size: 64 << 30, Free: 60 << 30}, duoIDs)
		f.appear(time.Second, volume{Root: `G:\`, FS: "FAT32", Size: 32 << 30, Free: 30 << 30},
			[]string{`USBSTOR\DISK&VEN_LINUX\1`, `USB\VID_19F7&PID_0095\RD1`})
	}
	_, err := runNative(t, f, Options{}, stagedPlan(t, []byte("fw")))
	if err == nil || !strings.Contains(err.Error(), "exFAT, the update disk is FAT") || !strings.Contains(err.Error(), `USB\VID_19F7&PID_0095\RD1, not to a RØDECaster Duo in update mode`) {
		t.Fatalf("err = %v", err)
	}
}

// A passing failure to read the device tree does not reject the disk for good.
func TestNativeRetriesTheUSBCheck(t *testing.T) {
	f := newFakeSys(t)
	f.afterM = duoBehaviour
	f.ancestorErr = 3
	log, err := runNative(t, f, Options{}, stagedPlan(t, []byte("fw")))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log)
	}
	if n := strings.Count(log, "cannot tell which device it belongs to"); n != 1 {
		t.Fatalf("reported %d times:\n%s", n, log)
	}
	staged(t, f, `E:\`, stagedName)
}

func TestNormDrive(t *testing.T) {
	for in, want := range map[string]string{"e": `E:\`, "E:": `E:\`, `e:\`: `E:\`, " Z:/ ": `Z:\`} {
		if got, ok := normDrive(in); !ok || got != want {
			t.Errorf("normDrive(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "EE", "1:", `\\server\share`, "é", "a", "B:"} {
		if got, ok := normDrive(in); ok {
			t.Errorf("normDrive(%q) = %q, want not ok", in, got)
		}
	}
}

func TestIsRodeApp(t *testing.T) {
	for _, n := range []string{"RODECasterApp.exe", "RODECaster App.exe", "RØDECaster App.exe", "RODE Central.exe", "rodecentral.EXE"} {
		if !isRodeApp(n) {
			t.Errorf("isRodeApp(%q) = false", n)
		}
	}
	for _, n := range []string{"rcplug-setup.exe", "explorer.exe", "RODE Connect.exe", "rodecaster-tool.exe"} {
		if isRodeApp(n) {
			t.Errorf("isRodeApp(%q) = true", n)
		}
	}
}

func TestDeviceModel(t *testing.T) {
	for pid, want := range map[uint16]string{0x0095: "duo", 0x004f: "duo", 0x0050: "duo", 0x0037: "pro2", 0x0030: "pro2", 0x0038: ""} {
		if got := (Device{Product: pid}).Model(); got != want {
			t.Errorf("Model(%#04x) = %q, want %q", pid, got, want)
		}
	}
}
