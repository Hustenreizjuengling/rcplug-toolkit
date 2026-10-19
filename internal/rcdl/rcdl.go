// Package rcdl fetches rcplug's device package from its GitHub releases the
// way rcplugd updates itself: the release's manifest (release.json) must be
// signed by one of rcplug's release keys (rcplug's sdk/release), and the
// package must have exactly the size and SHA-256 the manifest names. So a
// package from anywhere else, or changed on the way, is never installed.
package rcdl

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Hustenreizjuengling/rcplug/sdk/release"
)

// Repo is rcplug's repository on GitHub.
const Repo = "Hustenreizjuengling/rcplug"

// BaseURL is GitHub; tests point it at a local server.
var BaseURL = "https://github.com/"

// TrustedKeys are the keys a release must be signed with: rcplug's own
// (tests sign with keys of their own).
var TrustedKeys = release.BuiltinKeys

// Package is the device package of rcplug's latest release.
type Package struct {
	Version string
	Asset   release.Asset
	URL     string
}

// Client is an HTTP client for the downloads: HTTPS only (also after
// redirects to GitHub's file hosts), with timeouts.
func Client() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if r.URL.Scheme != "https" {
				return fmt.Errorf("refusing a redirect to %s", r.URL.Redacted())
			}
			return nil
		},
	}
}

// Latest reads and verifies the manifest of rcplug's latest release.
func Latest(ctx context.Context, c *http.Client) (*Package, error) {
	base := BaseURL + Repo + "/releases/latest/download/"
	manifest, err := fetch(ctx, c, base+release.ManifestName, release.MaxManifestSize)
	if err != nil {
		return nil, fmt.Errorf("reading rcplug's latest release: %w", err)
	}
	sig, err := fetch(ctx, c, base+release.SignatureName, release.MaxManifestSize)
	if err != nil {
		return nil, fmt.Errorf("reading the release's signature: %w", err)
	}
	var keys []ed25519.PublicKey
	for _, k := range TrustedKeys {
		pub, err := release.ParsePublicKey(k)
		if err != nil {
			return nil, err
		}
		keys = append(keys, pub)
	}
	if _, err := release.Verify(manifest, sig, keys); err != nil {
		return nil, err
	}
	m, err := release.Parse(manifest)
	if err != nil {
		return nil, err
	}
	if m.Project != "rcplug" {
		return nil, fmt.Errorf("the release is of %q, not of rcplug", m.Project)
	}
	a, ok := m.Platform()
	if !ok {
		return nil, fmt.Errorf("rcplug %s has no device package", m.Version)
	}
	return &Package{Version: m.Version, Asset: a,
		URL: BaseURL + Repo + "/releases/download/v" + m.Version + "/" + a.Name}, nil
}

func fetch(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := get(ctx, c, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is too large", url)
	}
	return b, nil
}

func get(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

// Download saves the package in dir under its own name and returns the
// file. A file there already that matches the manifest is kept; a download
// that does not match it is removed again.
func Download(ctx context.Context, c *http.Client, p *Package, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, p.Asset.Name)
	if matches(final, p.Asset) == nil {
		return final, nil
	}
	resp, err := get(ctx, c, p.URL)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", p.Asset.Name, err)
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(dir, p.Asset.Name+".*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // gone once renamed
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, p.Asset.Size+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", p.Asset.Name, err)
	}
	if n != p.Asset.Size || hex.EncodeToString(h.Sum(nil)) != p.Asset.SHA256 {
		return "", fmt.Errorf("%s does not match the signed release (size or SHA-256)", p.Asset.Name)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

// matches checks a file against the manifest's size and SHA-256.
func matches(file string, a release.Asset) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("does not match")
	}
	return nil
}
