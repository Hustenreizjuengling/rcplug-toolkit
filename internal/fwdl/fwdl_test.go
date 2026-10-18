package fwdl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// fwImage is a tiny stock-like image: hostname, device tree and version.
func fwImage(hostname, dtb, version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"./etc/hostname":           hostname + "\n",
		"./etc/passwd":             "root:x:0:0:root:/home/root:/bin/sh\n",
		"./boot/" + dtb:            "dtb",
		"./usr/bin/rcp-os.version": version + "\n",
		"./usr/share/padding":      strings.Repeat("firmware ", 8192),
	} {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func duoImage(version string) []byte {
	return fwImage("rodecaster-pro-ii-mini", "meson-axg-rodecaster-pro-ii-mini.dtb", version)
}

type fake struct {
	srv      *httptest.Server
	model    Model
	files    map[string][]byte // version → os.tar.gz
	badMD5   map[string]bool
	manifest string // overrides the default manifest body if set
}

// fakeRode serves a manifest in RØDE's layout and releases under rcp2-duo/.
func fakeRode(t *testing.T) *fake {
	t.Helper()
	f := &fake{
		files: map[string][]byte{
			"1.7.6": duoImage("1.7.6"),
			"1.7.3": duoImage("1.7.3"),
			"9.9.9": duoImage("9.9.9"),
			"8.8.8": fwImage("rodecaster-pro-ii", "meson-axg-rodecaster-pro-ii.dtb", "8.8.8"), // a Pro II image in the Duo's place
			"7.7.7": duoImage("7.7.6"),                                                        // claims the wrong version
		},
		badMD5: map[string]bool{"9.9.9": true},
	}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	host := strings.TrimPrefix(f.srv.URL, "http://")
	mux.HandleFunc("/rcp2-mini-manifest.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if f.manifest != "" {
			fmt.Fprint(w, strings.ReplaceAll(f.manifest, "HOST", f.srv.URL))
			return
		}
		fmt.Fprint(w, strings.ReplaceAll(`{"name":"rcp2-update-manifest",
			"master-version":{"os-version":"1.7.6","os-URL":"HOST/rcp2-duo/1.7.6/os.tar.gz","os-md5":"HOST/rcp2-duo/1.7.6/os.md5"},
			"main-version":{"app-version":"1.7.6","os-version":"1.7.6","os-required":"1.7.6","app-URL":"","os-URL":"HOST/rcp2-duo/1.7.6/os.tar.gz","app-md5":"","os-md5":"HOST/rcp2-duo/1.7.6/os.md5"},
			"beta-version":{"os-version":"1.7.6","os-URL":"HOST/rcp2-duo/1.7.6/os.tar.gz","os-md5":"HOST/rcp2-duo/1.7.6/os.md5"},
			"testing-version":{"os-version":"2.0.0","os-URL":"HOST/rcp2-duo/testing/os.tar.gz","os-md5":"HOST/rcp2-duo/testing/os.md5"}}`, "HOST", f.srv.URL))
	})
	mux.HandleFunc("/rcp2-duo/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rcp2-duo/"), "/")
		body, ok := f.files[parts[0]]
		if !ok || len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		switch parts[1] {
		case "os.tar.gz":
			w.Write(body)
		case "os.md5":
			sum := md5.Sum(body)
			if f.badMD5[parts[0]] {
				sum = md5.Sum([]byte("something else"))
			}
			fmt.Fprintln(w, hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(f.srv.Close)

	old := allowedURL
	allowedURL = func(u *url.URL) bool { return u.Scheme == "http" && u.Host == host }
	t.Cleanup(func() { allowedURL = old })

	duo, _ := fwimage.ModelByID("duo")
	duo.Manifest = f.srv.URL + "/rcp2-mini-manifest.json"
	duo.Releases = f.srv.URL + "/rcp2-duo/"
	f.model = duo
	return f
}

func TestLatestOffersOnlyPublicChannels(t *testing.T) {
	f := fakeRode(t)
	rels, err := Latest(context.Background(), Client(), f.model)
	if err != nil {
		t.Fatal(err)
	}
	// beta equals stable and is folded in; the testing build is never offered.
	if len(rels) != 1 || rels[0].Channel != "stable" || rels[0].Version != "1.7.6" {
		t.Fatalf("releases = %+v", rels)
	}
}

func TestLatestFallsBackToMasterVersion(t *testing.T) {
	f := fakeRode(t)
	f.manifest = `{"name":"rcp2-update-manifest","master-version":{"os-version":"1.7.3","os-URL":"HOST/rcp2-duo/1.7.3/os.tar.gz","os-md5":"HOST/rcp2-duo/1.7.3/os.md5"}}`
	rels, err := Latest(context.Background(), Client(), f.model)
	if err != nil || len(rels) != 1 || rels[0].Version != "1.7.3" {
		t.Fatalf("releases = %+v, %v", rels, err)
	}
}

func TestLatestRejectsForeignManifests(t *testing.T) {
	f := fakeRode(t)
	for name, body := range map[string]string{
		"video manifest": `{"name":"rcv-update-manifest","main-version":{"os-version":"1.4.3","os-URL":"HOST/rcp2-duo/1.4.3/os.tar.gz","os-md5":"HOST/rcp2-duo/1.4.3/os.md5"}}`,
		"other path":     `{"name":"rcp2-update-manifest","main-version":{"os-version":"1.7.6","os-URL":"HOST/rcp2/1.7.6/os.tar.gz","os-md5":"HOST/rcp2/1.7.6/os.md5"}}`,
		"other version":  `{"name":"rcp2-update-manifest","main-version":{"os-version":"1.7.6","os-URL":"HOST/rcp2-duo/1.7.3/os.tar.gz","os-md5":"HOST/rcp2-duo/1.7.3/os.md5"}}`,
		"no stable":      `{"name":"rcp2-update-manifest","beta-version":{"os-version":"1.7.6","os-URL":"HOST/rcp2-duo/1.7.6/os.tar.gz","os-md5":"HOST/rcp2-duo/1.7.6/os.md5"}}`,
	} {
		f.manifest = body
		if rels, err := Latest(context.Background(), Client(), f.model); err == nil {
			t.Errorf("%s: accepted %+v", name, rels)
		}
	}
}

func TestDownloadVerifiesAndReuses(t *testing.T) {
	f := fakeRode(t)
	dir := t.TempDir()
	rels, err := Latest(context.Background(), Client(), f.model)
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	res, err := Download(context.Background(), Client(), f.model, rels[0], dir, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if res.Reused || res.Size != int64(len(f.files["1.7.6"])) || last != res.Size {
		t.Fatalf("result = %+v, progress %d", res, last)
	}
	if res.Image == nil || res.Image.ModelID != "duo" || res.Image.Version != "1.7.6" {
		t.Fatalf("image = %+v", res.Image)
	}
	if !strings.HasSuffix(res.Path, "rodecaster-duo-1.7.6-os.tar.gz") {
		t.Fatalf("path = %s", res.Path)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, f.files["1.7.6"]) {
		t.Fatal("content differs")
	}
	sidecar, _ := os.ReadFile(res.MD5Path)
	if strings.TrimSpace(string(sidecar)) != res.MD5 || !strings.HasSuffix(string(sidecar), "\n") {
		t.Fatalf("sidecar = %q", sidecar)
	}
	again, err := Download(context.Background(), Client(), f.model, rels[0], dir, nil)
	if err != nil || !again.Reused || again.Image.Version != "1.7.6" {
		t.Fatalf("second download: %+v, %v", again, err)
	}
}

func TestDownloadOlderVersion(t *testing.T) {
	f := fakeRode(t)
	rel, err := ForVersion(f.model, " 1.7.3 ")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Download(context.Background(), Client(), f.model, rel, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Image.Version != "1.7.3" {
		t.Fatalf("image = %+v", res.Image)
	}
}

func TestDownloadErrors(t *testing.T) {
	f := fakeRode(t)
	dir := t.TempDir()
	try := func(version string) error {
		rel, err := ForVersion(f.model, version)
		if err != nil {
			return err
		}
		_, err = Download(context.Background(), Client(), f.model, rel, dir, nil)
		return err
	}
	for version, want := range map[string]string{
		"1.0.0": "not on RØDE's server",
		"9.9.9": "checksum mismatch",
		"8.8.8": "for the RØDECaster Pro II",
		"7.7.7": "firmware 7.7.6, not 7.7.7",
		"1.7":   "not a firmware version",
		"../x":  "not a firmware version",
	} {
		if err := try(version); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", version, err, want)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("failed downloads left files behind: %v", ents)
	}

	evil := Release{Version: "1.7.6", URL: "http://example.com/os.tar.gz", MD5URL: "http://example.com/os.md5"}
	if _, err := Download(context.Background(), Client(), f.model, evil, dir, nil); err == nil || !strings.Contains(err.Error(), "refusing to download from") {
		t.Fatalf("foreign host: %v", err)
	}
}

func TestUnsupportedModels(t *testing.T) {
	video, _ := fwimage.ModelByID("video")
	if _, err := Latest(context.Background(), Client(), video); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("video: %v", err)
	}
	if _, err := ForVersion(video, "1.4.3"); err == nil {
		t.Fatal("video: ForVersion accepted")
	}
}

func TestProductionHostsOnly(t *testing.T) {
	for raw, want := range map[string]bool{
		"update.rode.com/rcp2-duo/1.7.6/os.tar.gz":                                 true,
		"https://update.rode.com/rcp2-mini-manifest.json":                          true,
		"update-rode-com.s3.ap-southeast-2.amazonaws.com/rcp2-duo/1.7.6/os.tar.gz": true,
		"http://update.rode.com/rcp2-duo/1.7.6/os.tar.gz":                          false,
		"https://update.rode.com.evil.example/os.tar.gz":                           false,
		"https://example.com/update.rode.com/os.tar.gz":                            false,
	} {
		_, err := normalizeURL(raw)
		if (err == nil) != want {
			t.Errorf("%s: allowed=%v, want %v", raw, err == nil, want)
		}
	}
	for id, want := range map[string]string{
		"duo":  "https://update.rode.com/rcp2-duo/1.7.3/os.tar.gz",
		"pro2": "https://update.rode.com/rcp2/1.7.3/os.tar.gz",
	} {
		m, _ := fwimage.ModelByID(id)
		rel, err := ForVersion(m, "1.7.3")
		if err != nil || rel.URL != want || rel.MD5URL != strings.TrimSuffix(want, "os.tar.gz")+"os.md5" {
			t.Fatalf("%s: ForVersion = %+v, %v", id, rel, err)
		}
	}
}
