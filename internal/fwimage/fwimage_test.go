package fwimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Image writes a small gzipped image with the given files ("./"-prefixed,
// like the stock images) and returns its path.
func image(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755})
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + name, Mode: 0o644, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "os.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func duoFiles(version string) map[string]string {
	return map[string]string{
		"etc/hostname": "rodecaster-pro-ii-mini\n",
		"etc/passwd":   "root:x:0:0:root:/home/root:/bin/sh\n",
		"boot/meson-axg-rodecaster-pro-ii-mini.dtb": "dtb",
		"usr/bin/rcp-os.version":                    version + "\n",
		"Application/mcu-images/version.txt":        version + "\n",
	}
}

func TestIdentifiesDuo(t *testing.T) {
	in, err := Inspect(image(t, duoFiles("1.7.3")))
	if err != nil {
		t.Fatal(err)
	}
	duo, _ := ModelByID("duo")
	if in.ModelID != "duo" || in.Version != "1.7.3" || in.MCUVersion != "1.7.3" || !in.HasPasswd {
		t.Fatalf("info = %+v", in)
	}
	if err := in.Check(duo, "1.7.3"); err != nil {
		t.Fatal(err)
	}
	if err := in.Check(duo, ""); err != nil {
		t.Fatal(err)
	}
	if err := in.Check(duo, "1.7.6"); err == nil || !strings.Contains(err.Error(), "firmware 1.7.3, not 1.7.6") {
		t.Fatalf("wrong version: %v", err)
	}
	if got := in.Describe(); got != "RØDECaster Duo, firmware 1.7.3" {
		t.Fatalf("Describe = %q", got)
	}
}

func TestRefusesOtherModel(t *testing.T) {
	in, err := Inspect(image(t, map[string]string{
		"etc/hostname":                         "rodecaster-pro-ii\n",
		"boot/meson-axg-rodecaster-pro-ii.dtb": "dtb",
		"usr/bin/rcp-os.version":               "1.7.6\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	duo, _ := ModelByID("duo")
	err = in.Check(duo, "")
	if err == nil || errors.Is(err, ErrUnknownModel) || !strings.Contains(err.Error(), "for the RØDECaster Pro II") {
		t.Fatalf("Pro II image accepted for the Duo: %v", err)
	}
}

func TestInconsistentMarkersAreUnknown(t *testing.T) {
	// Duo hostname with the Pro II device tree: trust neither.
	files := duoFiles("1.7.6")
	delete(files, "boot/meson-axg-rodecaster-pro-ii-mini.dtb")
	files["boot/meson-axg-rodecaster-pro-ii.dtb"] = "dtb"
	in, err := Inspect(image(t, files))
	if err != nil {
		t.Fatal(err)
	}
	duo, _ := ModelByID("duo")
	if err := in.Check(duo, ""); !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("inconsistent markers: %v", err)
	}
}

func TestNotAnImage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.tar.gz")
	os.WriteFile(p, []byte("not gzip"), 0o644)
	if _, err := Inspect(p); err == nil {
		t.Fatal("accepted a non-gzip file")
	}
}

func TestModelTable(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Models {
		if seen[m.ID] {
			t.Fatalf("duplicate model %s", m.ID)
		}
		seen[m.ID] = true
		if m.Support != Unsupported && (m.Hostname == "" || m.DTB == "" || m.Manifest == "" || !strings.HasSuffix(m.Releases, "/")) {
			t.Fatalf("model %s lacks firmware data: %+v", m.ID, m)
		}
	}
	if m, ok := ModelByHostname(" rodecaster-pro-ii-mini\n"); !ok || m.ID != "duo" {
		t.Fatalf("ModelByHostname = %+v, %v", m, ok)
	}
	if _, err := ModelByID("nope"); err == nil || !strings.Contains(err.Error(), "duo") {
		t.Fatalf("ModelByID error = %v", err)
	}
}

// Keys tells a prepared image from a stock one, whose authorized_keys is empty.
func TestCountsAuthorizedKeys(t *testing.T) {
	files := duoFiles("1.7.6")
	files["home/root/.ssh/authorized_keys"] = ""
	in, err := Inspect(image(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if in.Keys != 0 {
		t.Fatalf("stock image: Keys = %d, want 0", in.Keys)
	}
	files["home/root/.ssh/authorized_keys"] = "# added by rcplug\nssh-ed25519 AAAA one\n\nssh-ed25519 AAAA two\n"
	files["home/root/.ssh/known_hosts"] = "host ssh-ed25519 AAAA\n"
	if in, err = Inspect(image(t, files)); err != nil {
		t.Fatal(err)
	}
	if in.Keys != 2 {
		t.Fatalf("prepared image: Keys = %d, want 2", in.Keys)
	}
}

func TestKeyFingerprint(t *testing.T) {
	// As ssh-keygen -l prints it for this key.
	line := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7vY+SWORihUKc4UVEZdJ4UjtRbB+ukyY79uOSbC9Si test"
	if got := keyFingerprint(line); got != "SHA256:RXs1nqf/WSw8k6h49Cdzfc/3tFeaP7GLiUNfzpPiG+s" {
		t.Fatalf("fingerprint %q", got)
	}
	if keyFingerprint("not a key") != "" {
		t.Fatal("garbage has a fingerprint")
	}
	in := Info{Keys: 0, VendorKeys: 2}
	if got := in.KeysText(); got != "no SSH key of yours (and 2 of RØDE's own)" {
		t.Fatalf("KeysText %q", got)
	}
}
