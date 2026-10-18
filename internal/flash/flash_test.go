package flash

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHIDID(t *testing.T) {
	cases := []struct {
		line              string
		bus, vendor, prod uint16
		ok                bool
	}{
		{"HID_ID=0003:000019F7:00000095", 0x0003, 0x19F7, 0x0095, true},
		{"HID_ID=0003:000019f7:00000078", 0x0003, 0x19F7, 0x0078, true},   // lower-case hex
		{" HID_ID=0003:000019F7:00000001 ", 0x0003, 0x19F7, 0x0001, true}, // surrounding space
		{"HID_ID=0003:00000AAA:00000001", 0x0003, 0x0AAA, 0x0001, true},   // other vendor still parses
		{"HID_NAME=RODE RODECaster", 0, 0, 0, false},
		{"HID_ID=0003:000019F7", 0, 0, 0, false},        // too few fields
		{"HID_ID=zz:000019F7:00000001", 0, 0, 0, false}, // bad hex
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		bus, v, p, ok := parseHIDID(c.line)
		if ok != c.ok || (ok && (bus != c.bus || v != c.vendor || p != c.prod)) {
			t.Errorf("parseHIDID(%q) = (%#x,%#x,%#x,%v), want (%#x,%#x,%#x,%v)",
				c.line, bus, v, p, ok, c.bus, c.vendor, c.prod, c.ok)
		}
	}
}

func TestMatchVendor(t *testing.T) {
	uevent := strings.Join([]string{
		"DRIVER=hid-generic",
		"HID_ID=0003:000019F7:00000095",
		"HID_NAME=RODE RODECaster Duo",
		"MODALIAS=hid:b0003g0001v000019F7p00000095",
	}, "\n")
	_, prod, raw, ok := matchVendor(strings.NewReader(uevent), DuoVendorID)
	if !ok || prod != 0x0095 {
		t.Fatalf("matchVendor = (prod %#x, ok %v), want (0x0095, true)", prod, ok)
	}
	if !strings.Contains(raw, "19F7") {
		t.Errorf("raw %q should contain the HID_ID", raw)
	}

	// A device from another vendor must not match.
	other := "HID_ID=0003:0000046D:0000C52B\nHID_NAME=Logitech\n"
	if _, _, _, ok := matchVendor(strings.NewReader(other), DuoVendorID); ok {
		t.Errorf("matchVendor matched a non-RØDE device")
	}
}

func writeStaging(t *testing.T, dir string, body []byte, md5Line string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stagedName), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if md5Line != "" {
		if err := os.WriteFile(filepath.Join(dir, md5Name), []byte(md5Line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func digestOf(body []byte) string {
	sum := md5.Sum(body)
	return hex.EncodeToString(sum[:])
}

func TestCheckStagingGood(t *testing.T) {
	dir := t.TempDir()
	body := []byte("not really a tarball, but enough to hash")
	want := digestOf(body)
	writeStaging(t, dir, body, want+"\n") // bare digest, RØDE's os.md5 style

	st, err := CheckStaging(dir)
	if err != nil {
		t.Fatalf("CheckStaging: %v", err)
	}
	if st.Digest != want {
		t.Errorf("Digest = %s, want %s", st.Digest, want)
	}
	if !st.MD5OK {
		t.Errorf("MD5OK = false, want true (declared %q)", st.Declared)
	}
	if st.Size != int64(len(body)) {
		t.Errorf("Size = %d, want %d", st.Size, len(body))
	}
}

func TestCheckStagingTwoFieldMD5(t *testing.T) {
	dir := t.TempDir()
	body := []byte("payload")
	writeStaging(t, dir, body, digestOf(body)+"  "+stagedName+"\n") // "digest  name"
	st, err := CheckStaging(dir)
	if err != nil {
		t.Fatalf("CheckStaging: %v", err)
	}
	if !st.MD5OK {
		t.Errorf("MD5OK = false for a valid 'digest  name' sidecar")
	}
}

func TestCheckStagingMismatch(t *testing.T) {
	dir := t.TempDir()
	writeStaging(t, dir, []byte("payload"), strings.Repeat("0", 32)+"\n")
	st, err := CheckStaging(dir)
	if err != nil {
		t.Fatalf("CheckStaging: %v", err)
	}
	if st.MD5OK {
		t.Errorf("MD5OK = true for a mismatched checksum")
	}
}

func TestCheckStagingMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := CheckStaging(dir); err == nil {
		t.Fatalf("CheckStaging on empty dir: want error, got nil")
	}
	// archive present but no md5 sidecar
	writeStaging(t, dir, []byte("x"), "")
	if _, err := CheckStaging(dir); err == nil {
		t.Fatalf("CheckStaging without md5: want error, got nil")
	}
}

func TestReadMD5Sidecar(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.md5")
	os.WriteFile(p, []byte("D41D8CD98F00B204E9800998ECF8427E\n"), 0o644) // upper-case
	got, err := readMD5Sidecar(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("readMD5Sidecar = %q, want lower-cased digest", got)
	}
	os.WriteFile(p, []byte("nonsense\n"), 0o644)
	if _, err := readMD5Sidecar(p); err == nil {
		t.Errorf("readMD5Sidecar on nonsense: want error")
	}
}

func TestFindPython(t *testing.T) {
	// An interpreter that cannot exist must be reported, not silently accepted.
	if _, err := FindPython("definitely-not-a-real-interpreter-xyz"); err == nil {
		t.Errorf("FindPython(bogus): want error")
	}
}
