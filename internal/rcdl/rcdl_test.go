package rcdl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hustenreizjuengling/rcplug/sdk/release"
)

// fakeGitHub serves one rcplug release: its manifest, signature and package.
type fakeGitHub struct {
	manifest, sig, pkg []byte
	served             int
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/" + Repo + "/releases/latest/download/release.json":
		w.Write(f.manifest)
	case "/" + Repo + "/releases/latest/download/release.json.sig":
		w.Write(f.sig)
	case "/" + Repo + "/releases/download/v0.9.0/rcplug-0.9.0-aarch64.tar.gz":
		f.served++
		w.Write(f.pkg)
	default:
		http.NotFound(w, r)
	}
}

func setup(t *testing.T, project string, pkg []byte, mutate func(m *release.Manifest)) (*fakeGitHub, *http.Client) {
	t.Helper()
	pub, priv, err := release.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := release.ParsePrivateKey(priv)
	sum := sha256.Sum256(pkg)
	m := release.Manifest{Format: 1, Project: project, Version: "0.9.0", Assets: []release.Asset{
		{Name: "rcplug-0.9.0-aarch64.tar.gz", Size: int64(len(pkg)), SHA256: hex.EncodeToString(sum[:]), Kind: release.KindPlatform}}}
	if mutate != nil {
		mutate(&m)
	}
	raw, _ := json.Marshal(m)
	f := &fakeGitHub{manifest: raw, sig: release.Sign(raw, key), pkg: pkg}
	ts := httptest.NewTLSServer(f)
	t.Cleanup(ts.Close)
	oldBase, oldKeys := BaseURL, TrustedKeys
	BaseURL, TrustedKeys = ts.URL+"/", []string{pub}
	t.Cleanup(func() { BaseURL, TrustedKeys = oldBase, oldKeys })
	return f, ts.Client()
}

func TestDownloadsTheSignedPackage(t *testing.T) {
	f, c := setup(t, "rcplug", []byte("the device package"), nil)
	p, err := Latest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "0.9.0" || !strings.HasSuffix(p.URL, "/releases/download/v0.9.0/rcplug-0.9.0-aarch64.tar.gz") {
		t.Fatalf("package %+v", p)
	}
	dir := t.TempDir()
	file, err := Download(context.Background(), c, p, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "the device package" {
		t.Fatalf("saved %q", b)
	}
	// Downloaded already: kept, not fetched again.
	if _, err := Download(context.Background(), c, p, dir); err != nil || f.served != 1 {
		t.Fatalf("again: %v, served %d", err, f.served)
	}
}

func TestRefusesWhatIsNotSigned(t *testing.T) {
	f, c := setup(t, "rcplug", []byte("pkg"), nil)
	f.manifest = []byte(strings.Replace(string(f.manifest), "0.9.0", "0.9.1", 1)) // changed after signing
	if _, err := Latest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("a changed manifest: %v", err)
	}
	_, c = setup(t, "rcplug", []byte("pkg"), nil)
	TrustedKeys = release.BuiltinKeys // signed, but not with rcplug's key
	if _, err := Latest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "trusted key") {
		t.Fatalf("a foreign key: %v", err)
	}
}

func TestRefusesOtherProjectsAndChangedPackages(t *testing.T) {
	_, c := setup(t, "rcplug-plugins", []byte("pkg"), nil)
	if _, err := Latest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "not of rcplug") {
		t.Fatalf("another project: %v", err)
	}
	f, c := setup(t, "rcplug", []byte("the real package"), nil)
	p, err := Latest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	f.pkg = []byte("the real packagf") // same size, other content
	dir := t.TempDir()
	if _, err := Download(context.Background(), c, p, dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a changed package: %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left behind: %v", ents)
	}
	f.pkg = []byte("the real package and more")
	if _, err := Download(context.Background(), c, p, dir); err == nil {
		t.Fatal("a longer package was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, p.Asset.Name)); !os.IsNotExist(err) {
		t.Fatal("a package that does not match was kept")
	}
}

func TestNoPackageNoRelease(t *testing.T) {
	_, c := setup(t, "rcplug", []byte("pkg"), func(m *release.Manifest) { m.Assets[0].Kind = "" })
	if _, err := Latest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "no device package") {
		t.Fatalf("no device package: %v", err)
	}
}
