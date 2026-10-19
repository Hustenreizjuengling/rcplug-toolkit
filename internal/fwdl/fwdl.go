// Package fwdl finds and downloads official RØDECaster firmware the way RØDE's
// own RODECaster App does: a per-model JSON manifest on update.rode.com names
// the current release of each update channel, and each release is a plain
// gzipped root filesystem (os.tar.gz) with an MD5 sidecar (os.md5).
//
// Like the app, it checks that the manifest is an RØDECaster (rcp2) manifest
// and uses the stable channel ("main-version", else "master-version") or the
// beta channel. On top, all on the safe side: rcplug uses HTTPS (the app
// fetches the manifest over plain HTTP), accepts download URLs only on RØDE's
// own hosts and only under the chosen model's release path, verifies the MD5
// before a file gets its final name, then opens the image and checks that it
// identifies as the chosen model and version (see fwimage). It offers only the
// public channels, not RØDE's internal test builds.
package fwdl

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// Model is a RØDECaster variant; the table lives in fwimage.
type Model = fwimage.Model

// fetchable reports whether rcplug can fetch firmware for m.
func fetchable(m Model) error {
	if m.Support == fwimage.Unsupported || m.Manifest == "" || m.Releases == "" {
		why := m.Note
		if why == "" {
			why = "not supported"
		}
		return fmt.Errorf("rcplug cannot fetch firmware for the %s (%s)", m.Name, why)
	}
	return nil
}

// Release is one downloadable firmware version.
type Release struct {
	Channel string `json:"channel"` // "stable", "beta", or "" when picked by version
	Version string `json:"version"`
	URL     string `json:"url"`
	MD5URL  string `json:"md5Url"`
}

// The public channels and their manifest keys, in the app's order of
// preference. The manifest also lists "testing", "closedbeta", "privatebeta"
// and "sp" builds; those are RØDE's pre-release builds for selected testers
// and are not offered.
var channels = []struct {
	name string
	keys []string
}{
	{"stable", []string{"main-version", "master-version"}},
	{"beta", []string{"beta-version"}},
}

// manifestName is the "name" of the RØDECaster Pro II / Duo manifests; the
// Video family's say "rcv-update-manifest".
const manifestName = "rcp2-update-manifest"

type manifestEntry struct {
	OSVersion string `json:"os-version"`
	OSURL     string `json:"os-URL"`
	OSMD5     string `json:"os-md5"`
}

var versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// UserAgent is sent with every request.
var UserAgent = "rcplug"

// allowedURL reports whether u may be fetched: HTTPS on one of RØDE's update
// hosts. Tests replace it to talk to a local server.
var allowedURL = func(u *url.URL) bool {
	if u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "update.rode.com", "update-rode-com.s3.ap-southeast-2.amazonaws.com":
		return true
	}
	return false
}

// normalizeURL adds the https:// the manifest leaves out and checks the host.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !allowedURL(u) {
		if u.Scheme != "https" {
			return "", fmt.Errorf("refusing to download from %s (not HTTPS)", u.Redacted())
		}
		return "", fmt.Errorf("refusing to download from %s (not a RØDE update host)", u.Host)
	}
	return u.String(), nil
}

// Client returns an HTTP client suited for firmware downloads: no overall
// timeout (the files are ~170 MB), but bounded connection and header waits.
func Client() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

func get(ctx context.Context, c *http.Client, u string) (*http.Response, error) {
	rq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	rq.Header.Set("User-Agent", UserAgent)
	resp, err := c.Do(rq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return resp, nil
}

var errNotFound = fmt.Errorf("not found on RØDE's server")

// Latest reads the model's manifest and returns the stable release, followed
// by the beta release if it is a different version.
func Latest(ctx context.Context, c *http.Client, m Model) ([]Release, error) {
	if err := fetchable(m); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := get(ctx, c, m.Manifest)
	if err != nil {
		return nil, fmt.Errorf("reading the firmware list: %w", err)
	}
	defer resp.Body.Close()
	var man map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&man); err != nil {
		return nil, fmt.Errorf("reading the firmware list: %w", err)
	}
	var name string
	_ = json.Unmarshal(man["name"], &name)
	if name != manifestName {
		return nil, fmt.Errorf("%s is not a RØDECaster firmware list (name %q)", m.Manifest, name)
	}
	var out []Release
	for _, ch := range channels {
		rel, err := channelRelease(m, man, ch.keys)
		if err != nil {
			if ch.name == "stable" {
				return nil, err
			}
			continue // no usable beta: offer stable only
		}
		if len(out) > 0 && out[len(out)-1].Version == rel.Version {
			continue // beta is the same as stable
		}
		rel.Channel = ch.name
		out = append(out, rel)
	}
	return out, nil
}

// channelRelease reads the first of keys present in the manifest and checks
// that it points into m's own release path, at the version it names.
func channelRelease(m Model, man map[string]json.RawMessage, keys []string) (Release, error) {
	for _, key := range keys {
		raw, ok := man[key]
		if !ok {
			continue
		}
		var e manifestEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return Release{}, fmt.Errorf("the firmware list's %s is unreadable: %w", key, err)
		}
		if !versionRE.MatchString(e.OSVersion) {
			return Release{}, fmt.Errorf("the firmware list's %s has an odd version %q", key, e.OSVersion)
		}
		want, err := ForVersion(m, e.OSVersion)
		if err != nil {
			return Release{}, err
		}
		osURL, err := normalizeURL(e.OSURL)
		if err != nil {
			return Release{}, fmt.Errorf("the firmware list's %s: %w", key, err)
		}
		md5URL, err := normalizeURL(e.OSMD5)
		if err != nil {
			return Release{}, fmt.Errorf("the firmware list's %s checksum: %w", key, err)
		}
		if urlPath(osURL) != urlPath(want.URL) || urlPath(md5URL) != urlPath(want.MD5URL) {
			return Release{}, fmt.Errorf("the firmware list's %s points at %s, not at the %s's release %s", key, osURL, m.Name, e.OSVersion)
		}
		return Release{Version: e.OSVersion, URL: osURL, MD5URL: md5URL}, nil
	}
	return Release{}, fmt.Errorf("the firmware list has no %s", strings.Join(keys, " or "))
}

func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// ForVersion returns the release of a specific version, also one the manifest
// no longer lists. RØDE keeps past releases under <Releases><version>/;
// whether a version exists shows when downloading it.
func ForVersion(m Model, version string) (Release, error) {
	if err := fetchable(m); err != nil {
		return Release{}, err
	}
	version = strings.TrimSpace(version)
	if !versionRE.MatchString(version) {
		return Release{}, fmt.Errorf("%q is not a firmware version like 1.7.6", version)
	}
	base := m.Releases + version + "/"
	osURL, err := normalizeURL(base + "os.tar.gz")
	if err != nil {
		return Release{}, err
	}
	md5URL, err := normalizeURL(base + "os.md5")
	if err != nil {
		return Release{}, err
	}
	return Release{Version: version, URL: osURL, MD5URL: md5URL}, nil
}

// FileName is the name a downloaded release gets on disk.
func FileName(m Model, version string) string {
	return "rodecaster-" + m.ID + "-" + version + "-os.tar.gz"
}

// Progress is called while downloading with the bytes so far and the total
// (0 if unknown).
type Progress func(done, total int64)

// Result reports a finished download.
type Result struct {
	Path    string `json:"path"`    // the verified os.tar.gz
	MD5Path string `json:"md5Path"` // its sidecar, as RØDE ships it
	MD5     string `json:"md5"`
	Size    int64  `json:"size"`
	Reused  bool   `json:"reused"` // an intact copy was already there

	Image *fwimage.Info `json:"image"` // what the image says about itself
}

// Download fetches rel into dir, verifies it against RØDE's MD5 and checks
// that the image identifies as m at rel.Version. The file is written as
// <name>.part and renamed only after both checks passed, so a file with the
// final name is always complete and the right one. An intact earlier download
// is reused (and checked again).
func Download(ctx context.Context, c *http.Client, m Model, rel Release, dir string, progress Progress) (*Result, error) {
	if err := fetchable(m); err != nil {
		return nil, err
	}
	if !versionRE.MatchString(rel.Version) {
		return nil, fmt.Errorf("invalid version %q", rel.Version)
	}
	for _, raw := range []string{rel.URL, rel.MD5URL} {
		if _, err := normalizeURL(raw); err != nil {
			return nil, err
		}
	}
	want, err := fetchMD5(ctx, c, rel.MD5URL)
	if err != nil {
		if err == errNotFound {
			return nil, fmt.Errorf("firmware %s is not on RØDE's server", rel.Version)
		}
		return nil, fmt.Errorf("reading the checksum: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	final := filepath.Join(dir, FileName(m, rel.Version))
	res := &Result{Path: final, MD5Path: final + ".md5", MD5: want}

	if got, size, err := fileMD5(final); err == nil && got == want {
		if res.Image, err = checkImage(final, m, rel.Version); err != nil {
			return nil, err
		}
		res.Size, res.Reused = size, true
		return res, writeSidecar(res.MD5Path, want)
	}

	resp, err := get(ctx, c, rel.URL)
	if err != nil {
		if err == errNotFound {
			return nil, fmt.Errorf("firmware %s is not on RØDE's server", rel.Version)
		}
		return nil, err
	}
	defer resp.Body.Close()
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		return nil, err
	}
	defer os.Remove(part) // no-op after the rename
	h := md5.New()
	pw := &progressWriter{total: resp.ContentLength, fn: progress}
	n, err := io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > 0 && n != resp.ContentLength {
		return nil, fmt.Errorf("download incomplete: %d of %d bytes", n, resp.ContentLength)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return nil, fmt.Errorf("checksum mismatch: got %s, RØDE says %s; the download is damaged", got, want)
	}
	if res.Image, err = checkImage(part, m, rel.Version); err != nil {
		return nil, err
	}
	if err := os.Rename(part, final); err != nil {
		return nil, err
	}
	res.Size = n
	return res, writeSidecar(res.MD5Path, want)
}

// checkImage opens a downloaded image and checks model and version.
func checkImage(p string, m Model, version string) (*fwimage.Info, error) {
	in, err := fwimage.Inspect(p)
	if err != nil {
		return nil, err
	}
	if err := in.Check(m, version); err != nil {
		return nil, fmt.Errorf("the downloaded file is not the expected firmware: %w", err)
	}
	return in, nil
}

// Checksum reads the MD5 RØDE publishes for rel.
func Checksum(ctx context.Context, c *http.Client, rel Release) (string, error) {
	u, err := normalizeURL(rel.MD5URL)
	if err != nil {
		return "", err
	}
	return fetchMD5(ctx, c, u)
}

func fetchMD5(ctx context.Context, c *http.Client, u string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := get(ctx, c, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	// A bare digest, as RØDE ships it; tolerate the md5sum layout too.
	fields := strings.Fields(string(body))
	if len(fields) == 0 || len(fields[0]) != 32 {
		return "", fmt.Errorf("unexpected checksum file %q", strings.TrimSpace(string(body)))
	}
	sum := strings.ToLower(fields[0])
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("unexpected checksum file %q", sum)
	}
	return sum, nil
}

func fileMD5(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := md5.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func writeSidecar(p, sum string) error { return os.WriteFile(p, []byte(sum+"\n"), 0o644) }

type progressWriter struct {
	done, total int64
	fn          Progress
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return len(b), nil
}
