package cfw

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// build is a thin wrapper so the tests read as before.
func build(in, out string, keys []string, pw, force bool) (*Result, error) {
	return Build(Options{Input: in, OutDir: out, PublicKeys: keys, EnablePasswordAuth: pw, Force: force})
}

type entry struct {
	hdr  tar.Header
	body []byte
}

// buildImage writes a synthetic rootfs .tar.gz with "./"-prefixed members.
func buildImage(t *testing.T, extra ...entry) string {
	t.Helper()
	base := []entry{
		{tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755}, nil},
		{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/passwd", Mode: 0o644},
			[]byte("root:x:0:0:root:/home/root:/bin/sh\nmessagebus:x:1:1::/var/lib/dbus:/bin/false\n")},
		{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/ssh/sshd_config", Mode: 0o600},
			[]byte("Port 22\n#PasswordAuthentication no\nPermitRootLogin yes\n")},
		{tar.Header{Typeflag: tar.TypeDir, Name: "./home/root/", Mode: 0o700, Uid: 0, Gid: 0}, nil},
		{tar.Header{Typeflag: tar.TypeSymlink, Name: "./bin/sh", Linkname: "/bin/busybox", Mode: 0o777}, nil},
		{tar.Header{Typeflag: tar.TypeChar, Name: "./dev/null", Mode: 0o666, Devmajor: 1, Devminor: 3}, nil},
		{tar.Header{Typeflag: tar.TypeReg, Name: "./bin/ping", Mode: 0o755, Format: tar.FormatPAX,
			PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "cap+ep"}}, []byte("ELF...ping")},
	}
	return writeTarGz(t, append(append(base, duoMarkers...), extra...))
}

// duoMarkers make an image identify as a RØDECaster Duo, like the stock one.
var duoMarkers = []entry{
	{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/hostname", Mode: 0o644}, []byte("rodecaster-pro-ii-mini\n")},
	{tar.Header{Typeflag: tar.TypeReg, Name: "./boot/meson-axg-rodecaster-pro-ii-mini.dtb", Mode: 0o644}, []byte("dtb")},
	{tar.Header{Typeflag: tar.TypeReg, Name: "./usr/bin/rcp-os.version", Mode: 0o644}, []byte("1.7.3\n")},
}

func writeTarGz(t *testing.T, entries []entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rootfs.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := e.hdr
		h.Size = int64(len(e.body))
		if h.ModTime.IsZero() {
			h.ModTime = time.Unix(1000000000, 0)
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("%s: %v", h.Name, err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTarGz(t *testing.T, path string) map[string]entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]entry{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		out[normalize(hdr.Name)] = entry{*hdr, body}
	}
	return out
}

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz0123456789ABCD test@host"

func writeKey(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key.pub")
	if err := os.WriteFile(p, []byte(s+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddsKeyAndPreservesEverything(t *testing.T) {
	img := buildImage(t)
	out := t.TempDir()
	if _, err := build(img, out, []string{testKey}, false, false); err != nil {
		t.Fatal(err)
	}
	m := readTarGz(t, filepath.Join(out, StagedName))

	ak, ok := m["home/root/.ssh/authorized_keys"]
	if !ok {
		t.Fatal("authorized_keys was not added")
	}
	if string(ak.body) != testKey+"\n" {
		t.Errorf("authorized_keys body = %q", ak.body)
	}
	if ak.hdr.Mode != 0o600 || ak.hdr.Uid != 0 || ak.hdr.Gid != 0 {
		t.Errorf("authorized_keys mode=%o uid=%d gid=%d", ak.hdr.Mode, ak.hdr.Uid, ak.hdr.Gid)
	}
	if d, ok := m["home/root/.ssh"]; !ok || d.hdr.Typeflag != tar.TypeDir || d.hdr.Mode != 0o700 {
		t.Errorf(".ssh dir missing or wrong: %+v", d.hdr)
	}
	// Faithful passthrough of the tricky entries.
	if l := m["bin/sh"]; l.hdr.Typeflag != tar.TypeSymlink || l.hdr.Linkname != "/bin/busybox" {
		t.Errorf("symlink not preserved: %+v", l.hdr)
	}
	if d := m["dev/null"]; d.hdr.Typeflag != tar.TypeChar || d.hdr.Devmajor != 1 || d.hdr.Devminor != 3 {
		t.Errorf("device node not preserved: %+v", d.hdr)
	}
	if p := m["bin/ping"]; p.hdr.PAXRecords["SCHILY.xattr.security.capability"] != "cap+ep" || string(p.body) != "ELF...ping" {
		t.Errorf("xattr/file not preserved: %+v body=%q", p.hdr.PAXRecords, p.body)
	}
	// Default: password auth untouched.
	if sc := m["etc/ssh/sshd_config"]; bytes.Contains(sc.body, []byte("PasswordAuthentication yes")) {
		t.Error("password auth should be off by default")
	}
	// md5 sidecar matches the archive.
	want, _ := os.ReadFile(filepath.Join(out, MD5Name))
	raw, _ := os.ReadFile(filepath.Join(out, StagedName))
	sum := md5.Sum(raw)
	if got := hex.EncodeToString(sum[:]) + "\n"; got != string(want) {
		t.Errorf("md5 sidecar %q != %q", want, got)
	}
}

func TestAppendsToExistingAuthorizedKeys(t *testing.T) {
	old := "ssh-rsa AAAAB3NzaC1yc2EAAAAoldkeybodybase64chars0123456789 old@host"
	img := buildImage(t,
		entry{tar.Header{Typeflag: tar.TypeDir, Name: "./home/root/.ssh/", Mode: 0o700}, nil},
		entry{tar.Header{Typeflag: tar.TypeReg, Name: "./home/root/.ssh/authorized_keys", Mode: 0o600}, []byte(old)})
	out := t.TempDir()
	if _, err := build(img, out, []string{testKey}, false, false); err != nil {
		t.Fatal(err)
	}
	m := readTarGz(t, filepath.Join(out, StagedName))
	want := old + "\n" + testKey + "\n"
	if got := string(m["home/root/.ssh/authorized_keys"].body); got != want {
		t.Errorf("authorized_keys = %q, want %q", got, want)
	}
	// The .ssh dir must not be duplicated.
	n := 0
	for name := range m {
		if name == "home/root/.ssh" {
			n++
		}
	}
	if n != 1 {
		t.Errorf(".ssh dir appears %d times", n)
	}
}

func TestEnablePasswordAuth(t *testing.T) {
	img := buildImage(t)
	out := t.TempDir()
	if _, err := build(img, out, []string{testKey}, true, false); err != nil {
		t.Fatal(err)
	}
	m := readTarGz(t, filepath.Join(out, StagedName))
	sc := string(m["etc/ssh/sshd_config"].body)
	if !bytes.Contains([]byte(sc), []byte("PasswordAuthentication yes")) || bytes.Contains([]byte(sc), []byte("#PasswordAuthentication")) {
		t.Errorf("sshd_config not patched: %q", sc)
	}
}

func TestReportsAndChecksTheImage(t *testing.T) {
	img := buildImage(t)
	res, err := build(img, t.TempDir(), []string{testKey}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Image.ModelID != "duo" || res.Image.Version != "1.7.3" || res.InputMD5 {
		t.Fatalf("image = %+v, md5 checked %v", res.Image, res.InputMD5)
	}

	// A matching sidecar is checked, a wrong one refused.
	raw, _ := os.ReadFile(img)
	sum := md5.Sum(raw)
	os.WriteFile(img+".md5", []byte(hex.EncodeToString(sum[:])+"\n"), 0o644)
	if res, err := build(img, t.TempDir(), []string{testKey}, false, false); err != nil || !res.InputMD5 {
		t.Fatalf("good sidecar: %+v, %v", res, err)
	}
	os.WriteFile(img+".md5", []byte("00000000000000000000000000000000\n"), 0o644)
	if _, err := build(img, t.TempDir(), []string{testKey}, false, true); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("bad sidecar: %v", err)
	}
}

func TestRefusesImagesForOtherModels(t *testing.T) {
	pro2 := writeTarGz(t, []entry{
		{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/passwd", Mode: 0o644}, []byte("root:x:0:0:root:/home/root:/bin/sh\n")},
		{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/hostname", Mode: 0o644}, []byte("rodecaster-pro-ii\n")},
		{tar.Header{Typeflag: tar.TypeReg, Name: "./boot/meson-axg-rodecaster-pro-ii.dtb", Mode: 0o644}, []byte("dtb")},
	})
	for _, force := range []bool{false, true} {
		if _, err := build(pro2, t.TempDir(), []string{testKey}, false, force); err == nil || !strings.Contains(err.Error(), "RØDECaster Pro II") {
			t.Fatalf("force=%v: Pro II image accepted: %v", force, err)
		}
	}
	// Keys only for fully supported models.
	if _, err := Build(Options{Input: buildImage(t), OutDir: t.TempDir(), PublicKeys: []string{testKey}, Model: "pro2"}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("pro2 model: %v", err)
	}
	if _, err := Build(Options{Input: buildImage(t), OutDir: t.TempDir(), PublicKeys: []string{testKey}, Model: "nope"}); err == nil {
		t.Fatal("unknown model accepted")
	}
}

func TestUnknownModelNeedsForce(t *testing.T) {
	img := writeTarGz(t, []entry{
		{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/passwd", Mode: 0o644}, []byte("root:x:0:0:root:/home/root:/bin/sh\n")},
	})
	if _, err := build(img, t.TempDir(), []string{testKey}, false, false); err == nil || !strings.Contains(err.Error(), "known RØDECaster model") {
		t.Fatalf("unknown model without force: %v", err)
	}
	if _, err := build(img, t.TempDir(), []string{testKey}, false, true); err != nil {
		t.Fatalf("unknown model with force: %v", err)
	}
}

func TestRefusesNonRootfs(t *testing.T) {
	img := writeTarGz(t, []entry{{tar.Header{Typeflag: tar.TypeReg, Name: "./readme.txt", Mode: 0o644}, []byte("hi")}})
	if _, err := build(img, t.TempDir(), []string{testKey}, false, false); err == nil {
		t.Fatal("expected an error without etc/passwd")
	}
	if _, err := build(img, t.TempDir(), []string{testKey}, false, true); err != nil {
		t.Fatalf("--force should proceed: %v", err)
	}
}

func TestValidPubKey(t *testing.T) {
	good := []string{testKey, "ssh-rsa AAAAB3NzaC1yc2Erestofthekey0123456789abcdef", "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHA256body0123 x"}
	for _, k := range good {
		if err := ValidatePublicKey(k); err != nil {
			t.Errorf("good key rejected: %q: %v", k, err)
		}
	}
	bad := []string{"", "ssh-ed25519", "not a key at all", "-----BEGIN OPENSSH PRIVATE KEY-----", testKey + "\nssh-rsa AAAA second", "magic-type AAAAbody0123456789"}
	for _, k := range bad {
		if err := ValidatePublicKey(k); err == nil {
			t.Errorf("bad key accepted: %q", k)
		}
	}
}
