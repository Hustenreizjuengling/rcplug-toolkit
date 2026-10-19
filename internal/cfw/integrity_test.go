package cfw

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A damaged image (here only its gzip checksum) never becomes a flashable
// archive, and no output folder is left behind.
func TestRefusesADamagedImage(t *testing.T) {
	img := buildImage(t)
	raw, _ := os.ReadFile(img)
	raw[len(raw)-8] ^= 0xff // the CRC-32 in the gzip trailer: the tar inside reads fine
	os.WriteFile(img, raw, 0o644)
	out := filepath.Join(t.TempDir(), "out")
	if _, err := build(img, out, []string{testKey}, false, false); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged image: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("an output folder was made for a refused image")
	}
}

// The second pass must read the bytes the checks saw.
func TestRefusesAnImageThatChangesBetweenThePasses(t *testing.T) {
	good, _ := os.ReadFile(buildImage(t))
	other, _ := os.ReadFile(buildImage(t, entry{tar.Header{Typeflag: tar.TypeReg, Name: "./etc/extra", Mode: 0o644}, []byte("more")}))
	pass := 0
	src := func() (io.ReadCloser, error) {
		pass++
		if pass == 1 {
			return io.NopCloser(bytes.NewReader(good)), nil
		}
		return io.NopCloser(bytes.NewReader(other)), nil
	}
	var out bytes.Buffer
	_, err := BuildTo(StreamOptions{PublicKeys: []string{testKey}, Name: "img"}, src, &out)
	if err == nil || !strings.Contains(err.Error(), "changed while it was being read") {
		t.Fatalf("changed image: %v", err)
	}
}

// Checked sees the result of the checks before anything is written, and can
// stop the build.
func TestCheckedStopsBeforeWriting(t *testing.T) {
	img := buildImage(t)
	stop := errors.New("not this one")
	var seen *Result
	var out bytes.Buffer
	_, err := BuildTo(StreamOptions{PublicKeys: []string{testKey}, Checked: func(r *Result) error { seen = r; return stop }}, FileSource(img), &out)
	if !errors.Is(err, stop) || out.Len() != 0 || seen == nil || seen.InputDigest == "" || seen.Image.ModelID != "duo" {
		t.Fatalf("err %v, %d bytes written, seen %+v", err, out.Len(), seen)
	}
}
