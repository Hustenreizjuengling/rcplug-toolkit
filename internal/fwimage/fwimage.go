// Package fwimage knows the RØDECaster models and identifies firmware images.
//
// RØDE's app picks a firmware manifest per model; rcplug keeps the same table.
// An image also identifies itself: the stock root filesystem carries the
// board's hostname (etc/hostname), its device tree (boot/*.dtb) and the
// firmware version (usr/bin/rcp-os.version). Checking those before using an
// image catches a file for the wrong model or version, whatever it is called.
package fwimage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Support says how far rcplug supports a model.
type Support int

const (
	// Full: download, add a key, install rcplug.
	Full Support = iota
	// DownloadOnly: rcplug can fetch and check the stock firmware, nothing more.
	DownloadOnly
	// Unsupported: rcplug knows the model but cannot work with its firmware.
	Unsupported
)

func (s Support) String() string {
	switch s {
	case Full:
		return "supported"
	case DownloadOnly:
		return "download only"
	}
	return "not supported"
}

// Model is one RØDECaster variant.
type Model struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Support  Support `json:"support"`
	Note     string  `json:"note,omitempty"`
	Hostname string  `json:"-"` // etc/hostname in the stock image
	DTB      string  `json:"-"` // device tree in boot/
	Manifest string  `json:"-"` // RØDE's manifest URL, as the RODECaster App uses it
	Releases string  `json:"-"` // base URL of releases: <Releases><version>/os.tar.gz
}

// Models are the RØDECaster variants RØDE's app knows. The Duo is RØDE's
// "RØDECaster Pro II mini" internally (hostname, device tree, manifest name).
// The Video family uses encrypted .fwupd images (update.rode.com/rcv/*), which
// rcplug cannot use.
var Models = []Model{
	{ID: "duo", Name: "RØDECaster Duo", Support: Full,
		Hostname: "rodecaster-pro-ii-mini", DTB: "meson-axg-rodecaster-pro-ii-mini.dtb",
		Manifest: "https://update.rode.com/rcp2-mini-manifest.json", Releases: "https://update.rode.com/rcp2-duo/"},
	{ID: "pro2", Name: "RØDECaster Pro II", Support: DownloadOnly, Note: "rcplug is untested on the Pro II",
		Hostname: "rodecaster-pro-ii", DTB: "meson-axg-rodecaster-pro-ii.dtb",
		Manifest: "https://update.rode.com/manifest.json", Releases: "https://update.rode.com/rcp2/"},
	{ID: "video", Name: "RØDECaster Video", Support: Unsupported, Note: "encrypted firmware"},
	{ID: "video-s", Name: "RØDECaster Video S", Support: Unsupported, Note: "encrypted firmware"},
	{ID: "video-core", Name: "RØDECaster Video Core", Support: Unsupported, Note: "encrypted firmware"},
}

// ModelByID returns the model with the given ID.
func ModelByID(id string) (Model, error) {
	for _, m := range Models {
		if m.ID == id {
			return m, nil
		}
	}
	var ids []string
	for _, m := range Models {
		ids = append(ids, m.ID)
	}
	return Model{}, fmt.Errorf("unknown device %q (known: %s)", id, strings.Join(ids, ", "))
}

// ModelByHostname returns the model whose stock image uses hostname, as a
// running device reports it.
func ModelByHostname(hostname string) (Model, bool) {
	hostname = strings.TrimSpace(hostname)
	for _, m := range Models {
		if m.Hostname != "" && m.Hostname == hostname {
			return m, true
		}
	}
	return Model{}, false
}

// Info is what an image says about itself.
type Info struct {
	Hostname   string   `json:"hostname"`
	DTBs       []string `json:"dtbs"`
	Version    string   `json:"version"`    // usr/bin/rcp-os.version
	MCUVersion string   `json:"mcuVersion"` // Application/mcu-images/version.txt
	Entries    int      `json:"entries"`
	HasPasswd  bool     `json:"hasPasswd"`
	ModelID    string   `json:"model"`     // "" if no known model matches
	ModelName  string   `json:"modelName"` // display name of ModelID
	// Keys counts the keys in every .ssh/authorized_keys that are not RØDE's
	// own. Flashing an image without one gives no SSH access.
	Keys int `json:"keys"`
	// VendorKeys counts RØDE's own keys (stock 1.7.3 has two in root's
	// authorized_keys; 1.7.6 has none).
	VendorKeys int `json:"vendorKeys"`
}

// vendorKeys are the SHA-256 fingerprints (as ssh-keygen -l prints them) of
// the keys RØDE put into root's authorized_keys in stock firmware 1.7.3.
var vendorKeys = map[string]bool{
	"SHA256:6/X9EwH08yI/oW0Z19hMX9ZmUXKa6VSauwHgPYH5qhU": true, // RSA 4096
	"SHA256:CvYDlYDZBvjaHrgiwRu3nl+Se0c4fsEGqz42t/vanII": true, // Ed25519
}

// keyFingerprint returns the SHA-256 fingerprint of an authorized_keys line
// ("" if it has no readable key).
func keyFingerprint(line string) string {
	for _, f := range strings.Fields(line) {
		blob, err := base64.StdEncoding.DecodeString(f)
		if err != nil || len(blob) < 20 {
			continue
		}
		sum := sha256.Sum256(blob)
		return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	}
	return ""
}

// Scanner collects Info while the caller reads a tar stream; Observe is
// called for every entry, so callers that read the image anyway need no
// extra pass.
type Scanner struct{ info Info }

const maxMarker = 4096 // marker files are tiny; anything larger is not one

// Observe records what entry hdr (with body r) says about the image.
func (s *Scanner) Observe(hdr *tar.Header, r io.Reader) error {
	s.info.Entries++
	name := path.Clean(strings.TrimSuffix(strings.TrimPrefix(hdr.Name, "./"), "/"))
	reg := hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA
	small := func() (string, error) {
		b, err := io.ReadAll(io.LimitReader(r, maxMarker))
		return strings.TrimSpace(string(b)), err
	}
	var err error
	switch {
	case !reg:
	case name == "etc/hostname":
		s.info.Hostname, err = small()
	case name == "usr/bin/rcp-os.version":
		s.info.Version, err = small()
	case name == "Application/mcu-images/version.txt":
		s.info.MCUVersion, err = small()
	case name == "etc/passwd":
		s.info.HasPasswd = true
	case path.Base(name) == "authorized_keys" && path.Base(path.Dir(name)) == ".ssh":
		b, rerr := io.ReadAll(io.LimitReader(r, 1<<20))
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if vendorKeys[keyFingerprint(line)] {
				s.info.VendorKeys++
			} else {
				s.info.Keys++
			}
		}
		err = rerr
	case path.Dir(name) == "boot" && strings.HasSuffix(name, ".dtb"):
		s.info.DTBs = append(s.info.DTBs, path.Base(name))
	}
	return err
}

// Info returns what was learned, with the model identified.
func (s *Scanner) Info() *Info {
	in := s.info
	sort.Strings(in.DTBs)
	if m, ok := identify(&in); ok {
		in.ModelID, in.ModelName = m.ID, m.Name
	}
	return &in
}

// identify matches hostname and device tree; both must point at one model.
func identify(in *Info) (Model, bool) {
	m, ok := ModelByHostname(in.Hostname)
	if !ok {
		return Model{}, false
	}
	for _, d := range in.DTBs {
		if d == m.DTB {
			return m, true
		}
	}
	return Model{}, false
}

// Inspect reads a gzipped firmware image and reports what it is.
func Inspect(file string) (*Info, error) { return InspectContext(context.Background(), file) }

// InspectContext is Inspect that stops when ctx ends (Ctrl+C).
func InspectContext(ctx context.Context, file string) (*Info, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(ContextReader(ctx, f))
	if err != nil {
		return nil, fmt.Errorf("%s is not a gzipped firmware image: %w", file, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var s Scanner
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: damaged image: %w", file, err)
		}
		if err := s.Observe(hdr, tr); err != nil {
			return nil, err
		}
	}
	return s.Info(), nil
}

// ErrUnknownModel means the image names no model rcplug knows; it may be a
// stock image whose markers changed, or no firmware image at all.
var ErrUnknownModel = fmt.Errorf("the image does not identify as a known RØDECaster model")

// Check verifies that the image is for want and, if version is not empty, of
// that version. An image that identifies as another model is always an
// error; one that identifies as nothing returns ErrUnknownModel (wrapped).
func (in *Info) Check(want Model, version string) error {
	switch {
	case in.ModelID == want.ID:
	case in.ModelID != "":
		return fmt.Errorf("this image is for the %s (hostname %q), not the %s", in.ModelName, in.Hostname, want.Name)
	default:
		return fmt.Errorf("%w (hostname %q, device trees %v); expected the %s", ErrUnknownModel, in.Hostname, in.DTBs, want.Name)
	}
	if version != "" && in.Version != "" && in.Version != version {
		return fmt.Errorf("this image is firmware %s, not %s", in.Version, version)
	}
	return nil
}

// Describe is a one-line summary for people.
// ContextReader reads from r until ctx ends; then it returns ctx's error, so
// that long reads of a firmware image can be interrupted.
func ContextReader(ctx context.Context, r io.Reader) io.Reader {
	if ctx == nil {
		return r
	}
	return &ctxReader{ctx: ctx, r: r}
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// KeysText says which SSH keys an image holds, e.g. "1 SSH key (and 2 of
// RØDE's own)".
func (in *Info) KeysText() string {
	s := fmt.Sprintf("%d SSH key(s)", in.Keys)
	if in.Keys == 0 {
		s = "no SSH key of yours"
	}
	if in.VendorKeys > 0 {
		s += fmt.Sprintf(" (and %d of RØDE's own)", in.VendorKeys)
	}
	return s
}

func (in *Info) Describe() string {
	name := in.ModelName
	if name == "" {
		name = "unknown model (hostname " + fmt.Sprintf("%q", in.Hostname) + ")"
	}
	v := in.Version
	if v == "" {
		v = "unknown version"
	}
	return name + ", firmware " + v
}
