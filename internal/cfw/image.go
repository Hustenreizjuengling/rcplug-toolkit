package cfw

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// imageScan is what one read pass over the firmware learns: the member-name
// style ("./"-prefixed or not), which members exist, root's passwd home, a
// timestamp to stamp any files we add, what the image says it is, and the
// MD5 of the file.
type imageScan struct {
	dotSlash bool
	members  map[string]bool
	passwd   string // root's home field from /etc/passwd, "" if none
	modTime  time.Time
	info     *fwimage.Info
	md5      string
}

// scanImage reads the whole archive once, recording names, /etc/passwd, the
// newest modification time and the image's model markers, and hashes the file.
func scanImage(ctx context.Context, file string) (*imageScan, error) {
	s := &imageScan{members: map[string]bool{}}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := md5.New()
	raw := io.TeeReader(fwimage.ContextReader(ctx, f), h)
	var markers fwimage.Scanner
	dot, plain := 0, 0
	err = eachEntryFrom(raw, func(hdr *tar.Header, r io.Reader) error {
		if err := markers.Observe(hdr, r); err != nil {
			return err
		}
		name := normalize(hdr.Name)
		s.members[name] = true
		if strings.HasPrefix(hdr.Name, "./") {
			dot++
		} else if hdr.Name != "." {
			plain++
		}
		if hdr.ModTime.After(s.modTime) {
			s.modTime = hdr.ModTime
		}
		if name == "etc/passwd" && hdr.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			s.passwd = rootHomeFromPasswd(body)
		}
		return nil
	})
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, tar.ErrHeader) {
		return nil, fmt.Errorf("%s is truncated or not a firmware image (%v)", file, err)
	}
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(io.Discard, raw); err != nil { // hash the bytes after the tar end
		return nil, err
	}
	s.md5 = hex.EncodeToString(h.Sum(nil))
	s.info = markers.Info()
	s.dotSlash = dot >= plain && dot > 0
	if s.modTime.IsZero() {
		s.modTime = time.Now()
	}
	return s, nil
}

// rootHome is root's home directory as a normalized path (no leading slash),
// defaulting to home/root (the Yocto convention on this device).
func (s *imageScan) rootHome() string {
	if s.passwd != "" {
		return normalize(strings.TrimPrefix(s.passwd, "/"))
	}
	return "home/root"
}

func (s *imageScan) has(normName string) bool { return s.members[normName] }

// name renders a normalized path in the archive's own style.
func (s *imageScan) name(normName string) string {
	if s.dotSlash {
		return "./" + normName
	}
	return normName
}

// rootHomeFromPasswd returns root's home field (field 6) from an /etc/passwd.
func rootHomeFromPasswd(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 6 && f[0] == "root" && f[5] != "" {
			return f[5]
		}
	}
	return ""
}

// eachEntry calls fn for every tar entry of a gzipped archive. The reader is
// valid only during the call.
func eachEntry(ctx context.Context, file string, fn func(hdr *tar.Header, r io.Reader) error) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return eachEntryFrom(fwimage.ContextReader(ctx, f), fn)
}

func eachEntryFrom(f io.Reader, fn func(hdr *tar.Header, r io.Reader) error) error {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}

// normalize strips a leading "./" and any trailing "/" so paths compare
// regardless of the archive's style.
func normalize(name string) string {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimSuffix(name, "/")
	return path.Clean(name)
}
