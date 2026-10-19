// Package cfw adds SSH public keys to a RØDECaster Duo firmware image: it
// takes the stock firmware (os.tar.gz, a plain gzipped root filesystem) and
// writes a flashable archive.tar.gz + archive.md5 with the keys in root's
// authorized_keys.
//
// It rewrites the tar faithfully — order, ownership, modes, symlinks, device
// nodes, hard links and extended attributes are preserved — so it needs no
// root or fakeroot and carries no secrets. It does not flash; see
// docs/enable-ssh.md for the whole procedure.
package cfw

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// File names the device expects on its update disk.
const (
	StagedName = "archive.tar.gz"
	MD5Name    = "archive.md5"
)

// Options controls one build.
type Options struct {
	Input              string   // stock firmware image (os.tar.gz)
	Model              string   // device the image must be for (fwimage model ID; "" = "duo")
	OutDir             string   // output directory
	PublicKeys         []string // OpenSSH public key lines to add to root
	EnablePasswordAuth bool     // also allow password logins (edits sshd_config)
	MD5Format          string   // archive.md5 content; {hash}/{name} expand ("" = "{hash}")
	Force              bool     // proceed with an image that has no /etc/passwd or names no known model
	// Context stops the build when it ends (Ctrl+C); nil means never.
	Context context.Context
}

// Result reports what a build produced.
type Result struct {
	ArchivePath string
	MD5Path     string
	Digest      string
	KeysAdded   int
	SSHDPatched int
	RootHome    string
	Image       *fwimage.Info // what the input image says it is
	InputMD5    bool          // the input matched its .md5 sidecar
	InputDigest string        // the input's MD5
}

// Build creates the flashable archive from a stock firmware image. Before
// writing anything it checks the input: against its .md5 sidecar if there is
// one (as RØDE ships it), and that it identifies as the chosen model. An
// image for another model is refused even with Force.
func Build(o Options) (*Result, error) {
	outPath := filepath.Join(o.OutDir, StagedName)
	if a, err := os.Stat(o.Input); err == nil {
		if b, err := os.Stat(outPath); err == nil && os.SameFile(a, b) {
			return nil, fmt.Errorf("the output %s would replace the input image; choose another output folder", outPath)
		}
	}
	so := StreamOptions{Model: o.Model, PublicKeys: o.PublicKeys, EnablePasswordAuth: o.EnablePasswordAuth,
		Force: o.Force, Context: o.Context, Name: o.Input, ForceHint: " (rcplug-cfw --force proceeds anyway)"}
	if want, side, ok := sidecarMD5(o.Input); ok {
		so.ExpectMD5, so.ExpectFrom = want, "its checksum file "+side
	}
	// The output folder and file appear once the checks passed and there is
	// something to write, as before the build could stream.
	out := &lazyFile{dir: o.OutDir, path: outPath + ".tmp"}
	defer os.Remove(out.path)
	res, err := BuildTo(so, FileSource(o.Input), out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	tmp := out.path
	if err := os.Rename(tmp, outPath); err != nil {
		return nil, err
	}
	res.ArchivePath = outPath
	res.MD5Path = filepath.Join(o.OutDir, MD5Name)
	if err := os.WriteFile(res.MD5Path, []byte(MD5Line(o.MD5Format, res.Digest)+"\n"), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

// StreamOptions controls a build from a Source into a writer (BuildTo); the
// browser version of the toolkit uses it.
type StreamOptions struct {
	Model              string   // device the image must be for ("" = "duo")
	PublicKeys         []string // OpenSSH public key lines to add to root
	EnablePasswordAuth bool     // also allow password logins
	Force              bool     // proceed with an image that names no known model
	Context            context.Context
	// ExpectMD5 is the image's checksum as RØDE publishes it ("" = none);
	// ExpectFrom names where it came from, Name the image, both for errors.
	ExpectMD5, ExpectFrom, Name string
	// ForceHint ends the errors that Force would get past (the command
	// line names its flag).
	ForceHint string
	// Checked, if set, is called once the image passed the checks and
	// before anything is written; an error stops the build.
	Checked func(r *Result) error
}

// MD5Line is archive.md5's content for a digest; {hash} and {name} expand
// in format ("" = "{hash}").
func MD5Line(format, digest string) string {
	if format == "" {
		format = "{hash}"
	}
	return strings.NewReplacer("{hash}", digest, "{name}", StagedName).Replace(format)
}

// BuildTo checks the image from src as Build does and writes the flashable
// archive (archive.tar.gz) to w; Result.Digest is its MD5.
func BuildTo(o StreamOptions, src Source, w io.Writer) (*Result, error) {
	modelID := o.Model
	if modelID == "" {
		modelID = "duo"
	}
	model, err := fwimage.ModelByID(modelID)
	if err != nil {
		return nil, err
	}
	if model.Support != fwimage.Full {
		return nil, fmt.Errorf("adding a key is not supported for the %s yet (%s)", model.Name, model.Support)
	}
	for _, k := range o.PublicKeys {
		if err := ValidatePublicKey(k); err != nil {
			return nil, err
		}
	}
	if len(o.PublicKeys) == 0 && !o.EnablePasswordAuth {
		return nil, fmt.Errorf("nothing to do: give a public key or enable password auth")
	}
	name := o.Name
	if name == "" {
		name = "the image"
	}
	ctx := o.Context
	if ctx == nil {
		ctx = context.Background()
	}
	scan, err := scanImage(ctx, src, name)
	if err != nil {
		return nil, err
	}
	inputMD5 := false
	if want := strings.ToLower(strings.TrimSpace(o.ExpectMD5)); want != "" {
		if want != scan.md5 {
			from := o.ExpectFrom
			if from == "" {
				from = "its checksum"
			}
			return nil, fmt.Errorf("%s does not match %s (md5 %s, expected %s); the file is damaged", name, from, scan.md5, want)
		}
		inputMD5 = true
	}
	if err := scan.info.Check(model, ""); err != nil && !(o.Force && errors.Is(err, fwimage.ErrUnknownModel)) {
		if errors.Is(err, fwimage.ErrUnknownModel) {
			return nil, fmt.Errorf("%w%s", err, o.ForceHint)
		}
		return nil, err
	}
	if scan.passwd == "" && !o.Force {
		return nil, fmt.Errorf("%s has no etc/passwd; this does not look like a root filesystem%s", name, o.ForceHint)
	}
	home := scan.rootHome()
	sshDir := path.Join(home, ".ssh")
	authKeys := path.Join(sshDir, "authorized_keys")

	hash := md5.New()
	// Compress like the stock image (maximum level, Unix). Go's deflate is a
	// little less tight than GNU gzip, so the result is ~2% larger than the
	// stock 1.7.6 image; RØDE's own 1.7.3 image is larger still.
	gz, _ := gzip.NewWriterLevel(io.MultiWriter(w, hash), gzip.BestCompression)
	gz.OS = 3
	tw := tar.NewWriter(gz)

	res := &Result{RootHome: home, Image: scan.info, InputMD5: inputMD5, InputDigest: scan.md5}
	if o.Checked != nil {
		if err := o.Checked(res); err != nil {
			return nil, err
		}
	}
	// The second pass reads the image again: it must be the same bytes the
	// checks saw (a file changed meanwhile, or a reader that slipped).
	in, err := src()
	if err != nil {
		return nil, err
	}
	defer in.Close()
	inHash := md5.New()
	raw := io.TeeReader(fwimage.ContextReader(ctx, in), inHash)
	err = eachEntryFrom(raw, func(hdr *tar.Header, r io.Reader) error {
		name := normalize(hdr.Name)
		switch {
		case len(o.PublicKeys) > 0 && name == authKeys && hdr.Typeflag == tar.TypeReg:
			body, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			res.KeysAdded = len(o.PublicKeys)
			return writeReg(tw, hdr, appendKeys(body, o.PublicKeys))
		case o.EnablePasswordAuth && isSSHDConfig(name) && hdr.Typeflag == tar.TypeReg:
			body, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			res.SSHDPatched++
			return writeReg(tw, hdr, enablePassword(body))
		default:
			return passthrough(tw, hdr, r)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(o.PublicKeys) > 0 && res.KeysAdded == 0 {
		if !scan.has(sshDir) {
			if err := tw.WriteHeader(newDir(scan.name(sshDir), scan.modTime)); err != nil {
				return nil, err
			}
		}
		if err := writeReg(tw, &tar.Header{
			Typeflag: tar.TypeReg, Name: scan.name(authKeys), Mode: 0o600,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root", ModTime: scan.modTime,
		}, appendKeys(nil, o.PublicKeys)); err != nil {
			return nil, err
		}
		res.KeysAdded = len(o.PublicKeys)
	}
	for _, c := range []io.Closer{tw, gz} {
		if err := c.Close(); err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, raw); err != nil {
		return nil, err
	}
	if got := hex.EncodeToString(inHash.Sum(nil)); got != scan.md5 {
		return nil, fmt.Errorf("%s changed while it was being read (md5 %s, then %s)", name, scan.md5, got)
	}
	res.Digest = hex.EncodeToString(hash.Sum(nil))
	return res, nil
}

// sidecarMD5 reads the checksum file next to an image: <file>.md5 (as rcplug
// saves downloads) or, for RØDE's own os.tar.gz, os.md5.
func sidecarMD5(file string) (sum, side string, ok bool) {
	for _, p := range []string{file + ".md5", strings.TrimSuffix(file, ".tar.gz") + ".md5"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f := strings.Fields(string(raw))
		if len(f) > 0 && len(f[0]) == 32 {
			return strings.ToLower(f[0]), p, true
		}
	}
	return "", "", false
}

func writeReg(tw *tar.Writer, hdr *tar.Header, body []byte) error {
	h := *hdr
	h.Typeflag = tar.TypeReg
	h.Size = int64(len(body))
	if err := tw.WriteHeader(&h); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func passthrough(tw *tar.Writer, hdr *tar.Header, r io.Reader) error {
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
		if _, err := io.Copy(tw, r); err != nil {
			return err
		}
	}
	return nil
}

func newDir(name string, modTime time.Time) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o700, Uid: 0, Gid: 0,
		Uname: "root", Gname: "root", ModTime: modTime}
}

func appendKeys(existing []byte, keys []string) []byte {
	var b bytes.Buffer
	b.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteByte('\n')
	}
	for _, k := range keys {
		b.WriteString(strings.TrimSpace(k))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func enablePassword(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	seen := false
	for i, line := range lines {
		t := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(t, "passwordauthentication") || strings.HasPrefix(t, "#passwordauthentication") {
			lines[i] = "PasswordAuthentication yes"
			seen = true
		}
	}
	if !seen {
		lines = append(lines, "PasswordAuthentication yes")
	}
	return []byte(strings.Join(lines, "\n"))
}

func isSSHDConfig(name string) bool { return strings.HasPrefix(path.Base(name), "sshd_config") }

// lazyFile creates its folder and file on the first write.
type lazyFile struct {
	dir, path string
	f         *os.File
}

func (l *lazyFile) Write(p []byte) (int, error) {
	if l.f == nil {
		if err := os.MkdirAll(l.dir, 0o755); err != nil {
			return 0, err
		}
		f, err := os.Create(l.path)
		if err != nil {
			return 0, err
		}
		l.f = f
	}
	return l.f.Write(p)
}

func (l *lazyFile) Close() error {
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}
