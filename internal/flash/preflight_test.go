package flash

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"
)

// stage writes archive.tar.gz (a tiny image of the given files) and a
// matching archive.md5 into a fresh directory.
func stage(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + name, Mode: 0o644, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	dir := t.TempDir()
	writeStaging(t, dir, buf.Bytes(), digestOf(buf.Bytes())+"\n")
	return dir
}

func duoImage(keys string) map[string]string {
	return map[string]string{
		"etc/hostname": "rodecaster-pro-ii-mini\n",
		"etc/passwd":   "root:x:0:0:root:/home/root:/bin/sh\n",
		"boot/meson-axg-rodecaster-pro-ii-mini.dtb": "dtb",
		"usr/bin/rcp-os.version":                    "1.7.6\n",
		"home/root/.ssh/authorized_keys":            keys,
	}
}

func check(r *Report, id string) (Check, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

func TestPreflightArchiveWithKey(t *testing.T) {
	r := &Report{}
	r.checkArchive(stage(t, duoImage("ssh-ed25519 AAAA me\n")))
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
	img, ok := check(r, "image")
	if !ok || !img.OK || img.Warn || !strings.Contains(img.Detail, "RØDECaster Duo, firmware 1.7.6, 1 SSH key") {
		t.Fatalf("image check = %+v", img)
	}
	if r.Image == nil || r.Image.Keys != 1 || r.Staging == nil || !r.Staging.MD5OK {
		t.Fatalf("report = %+v", r)
	}
}

// A stock image may be flashed (to go back), but it gives no SSH: warn.
func TestPreflightStockImageWarns(t *testing.T) {
	r := &Report{}
	r.checkArchive(stage(t, duoImage("")))
	img, _ := check(r, "image")
	if !img.OK || !img.Warn || !strings.Contains(img.Detail, "no SSH key") {
		t.Fatalf("image check = %+v", img)
	}
	if r.Err() != nil {
		t.Fatalf("a warning must not block: %v", r.Err())
	}
}

func TestPreflightRefusesOtherModel(t *testing.T) {
	files := duoImage("ssh-ed25519 AAAA me\n")
	delete(files, "boot/meson-axg-rodecaster-pro-ii-mini.dtb")
	files["etc/hostname"] = "rodecaster-pro-ii\n"
	files["boot/meson-axg-rodecaster-pro-ii.dtb"] = "dtb"
	r := &Report{}
	r.checkArchive(stage(t, files))
	img, _ := check(r, "image")
	if img.OK || !strings.Contains(img.Detail, "not the RØDECaster Duo") {
		t.Fatalf("image check = %+v", img)
	}
	if r.Err() == nil {
		t.Fatal("an image for another model must block the run")
	}
}

func TestPreflightRefusesUnknownImage(t *testing.T) {
	r := &Report{}
	r.checkArchive(stage(t, map[string]string{"etc/hostname": "something\n"}))
	if img, _ := check(r, "image"); img.OK {
		t.Fatalf("image check = %+v", img)
	}
}

func TestPreflightChecksumMismatch(t *testing.T) {
	dir := stage(t, duoImage("ssh-ed25519 AAAA me\n"))
	writeStaging(t, dir, []byte("changed"), strings.Repeat("0", 32)+"\n")
	r := &Report{}
	r.checkArchive(dir)
	a, _ := check(r, "archive")
	if a.OK || !strings.Contains(a.Detail, "would reject it") {
		t.Fatalf("archive check = %+v", a)
	}
	if _, ok := check(r, "image"); ok {
		t.Fatal("a damaged archive must not be inspected")
	}
}

func TestReportErrKeepsTheCause(t *testing.T) {
	r := &Report{}
	r.add(pass("archive", "fine"))
	r.add(fail("device", ErrNoDevice))
	r.add(fail("mount", errors.New("later")))
	if err := r.Err(); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("Err = %v, want the first failure (ErrNoDevice)", err)
	}
}

func fakeDetect(t *testing.T, devs ...Device) {
	old := detect
	detect = func() ([]Device, error) { return devs, nil }
	t.Cleanup(func() { detect = old })
}

// The image is the Duo's, so is the procedure: another RØDECaster model on
// USB is refused, and so is an unknown one or a second Duo; next to other
// RØDE devices the Duo is chosen, with a warning.
func TestCheckDeviceModel(t *testing.T) {
	pro2 := Device{Node: "/dev/hidraw1", Vendor: DuoVendorID, Product: 0x0037}
	duo := Device{Node: "/dev/hidraw2", Vendor: DuoVendorID, Product: 0x0095}
	other := Device{Node: "/dev/hidraw3", Vendor: DuoVendorID, Product: 0x0038}

	fakeDetect(t, pro2)
	r := &Report{}
	r.checkDevice("")
	if c, _ := check(r, "device"); c.OK || !strings.Contains(c.Detail, "RØDECaster Pro II") || r.Node != "" {
		t.Fatalf("Pro II: %+v, node %q", c, r.Node)
	}

	fakeDetect(t, other)
	r = &Report{}
	r.checkDevice("")
	if c, _ := check(r, "device"); c.OK || !strings.Contains(c.Detail, "product id rcplug does not know") || r.Node != "" {
		t.Fatalf("unknown: %+v", c)
	}

	fakeDetect(t, pro2, other, duo)
	r = &Report{}
	r.checkDevice("")
	if c, _ := check(r, "device"); !c.OK || !c.Warn || r.Node != duo.Node || !strings.Contains(c.Detail, "2 other RØDE HID interface(s)") {
		t.Fatalf("Pro II + other + Duo: %+v, node %q", c, r.Node)
	}

	fakeDetect(t, duo, Device{Node: "/dev/hidraw5", Vendor: DuoVendorID, Product: 0x0050})
	r = &Report{}
	r.checkDevice("")
	if c, _ := check(r, "device"); c.OK || !strings.Contains(c.Detail, "2 RØDECaster Duos found") {
		t.Fatalf("two Duos: %+v", c)
	}

	// A Duo left in update mode by an earlier run must be restarted first.
	fakeDetect(t, Device{Node: "/dev/hidraw4", Vendor: DuoVendorID, Product: 0x004f})
	r = &Report{}
	r.checkDevice("")
	if c, _ := check(r, "device"); c.OK || !strings.Contains(c.Detail, "update mode already") {
		t.Fatalf("update mode: %+v", c)
	}

	r = &Report{}
	r.checkDevice("/dev/hidraw9")
	if c, _ := check(r, "device"); !c.OK || r.Node != "/dev/hidraw9" {
		t.Fatalf("given: %+v", c)
	}
}
