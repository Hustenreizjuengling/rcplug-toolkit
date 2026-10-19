// Command sitegen builds the toolkit's web pages, as GitHub Pages serves
// them:
//
//	go run ./tools/sitegen -out dist/site
//
// It copies the pages in web/, adds rcplug's shared look (sdk/ui), builds
// the firmware patcher as WebAssembly (cmd/rcplug-web) with Go's
// wasm_exec.js, and writes firmware.json: the releases RØDE offers now and
// their checksums, which the page cannot read itself (RØDE's server sends no
// CORS headers). Without RØDE's list (-firmware=false, or the server does
// not answer) the page asks for the file without offering downloads; with
// -require-firmware (the Pages workflow) that fails the build instead, so
// the pages online keep yesterday's list.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwdl"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
	"github.com/Hustenreizjuengling/rcplug/sdk/ui"
)

// Firmware is firmware.json.
type Firmware struct {
	Checked  string    `json:"checked"`
	Model    string    `json:"model"`
	Releases []Release `json:"releases"`
}

// Release is one of RØDE's current releases.
type Release struct {
	Channel string `json:"channel"`
	Version string `json:"version"`
	URL     string `json:"url"`
	MD5     string `json:"md5"`
}

func main() {
	out := flag.String("out", "dist/site", "the directory to write the site to (emptied first)")
	web := flag.String("web", "web", "the pages")
	version := flag.String("version", "", "the toolkit's version (default: the VERSION file)")
	firmware := flag.Bool("firmware", true, "read RØDE's current releases for firmware.json")
	require := flag.Bool("require-firmware", false, "fail when RØDE's list cannot be read")
	flag.Parse()
	if *version == "" {
		b, err := os.ReadFile("VERSION")
		if err != nil {
			fail(err)
		}
		*version = strings.TrimSpace(string(b))
	}
	if err := build(*out, *web, *version, *firmware, *require); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "sitegen:", err)
	os.Exit(1)
}

func build(out, web, version string, firmware, require bool) error {
	if err := clearSite(out); err != nil {
		return err
	}
	if err := copyTree(os.DirFS(web), out); err != nil {
		return fmt.Errorf("the pages: %w", err)
	}
	if err := copyTree(ui.FS, filepath.Join(out, "ui")); err != nil {
		return fmt.Errorf("the shared look: %w", err)
	}
	if err := buildWasm(out, version); err != nil {
		return fmt.Errorf("the patcher: %w", err)
	}
	if firmware {
		if err := writeFirmware(filepath.Join(out, "firmware.json")); err != nil {
			if require {
				return fmt.Errorf("RØDE's list: %w", err)
			}
			fmt.Fprintln(os.Stderr, "sitegen: without RØDE's list:", err)
		}
	}
	// The release the pages come from (the home page's download links) and
	// a stamp of the patcher's two files, which the page loads with it so a
	// cached wasm_exec.js never meets another build's module.
	stamp := sha256.New()
	for _, f := range []string{"rcplug-web.wasm", "wasm_exec.js"} {
		b, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			return err
		}
		stamp.Write(b)
	}
	v, _ := json.Marshal(map[string]string{"version": version, "wasm": hex.EncodeToString(stamp.Sum(nil))[:16]})
	return os.WriteFile(filepath.Join(out, "toolkit.json"), append(v, '\n'), 0o644)
}

// clearSite empties out for a new build; it refuses a folder that holds
// anything but an earlier build (marked by toolkit.json).
func clearSite(out string) error {
	ents, err := os.ReadDir(out)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && len(ents) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(out, "toolkit.json")); err != nil {
		return fmt.Errorf("%s is not empty and holds no earlier build of the pages; choose another folder", out)
	}
	return os.RemoveAll(out)
}

func copyTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func buildWasm(out, version string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version,
		"-o", filepath.Join(out, "rcplug-web.wasm"), "./cmd/rcplug-web")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// wasm_exec.js belongs to the Go release that built the module.
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} { // Go 1.24 moved it
		if err := copyFile(filepath.Join(strings.TrimSpace(string(root)), rel), filepath.Join(out, "wasm_exec.js")); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return errors.New("wasm_exec.js not found in GOROOT")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	o, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}

func writeFirmware(file string) error {
	m, err := fwimage.ModelByID("duo")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := fwdl.Client()
	rels, err := fwdl.Latest(ctx, c, m)
	if err != nil {
		return err
	}
	doc := Firmware{Checked: time.Now().UTC().Format(time.RFC3339), Model: m.Name}
	for _, r := range rels {
		sum, err := fwdl.Checksum(ctx, c, r)
		if err != nil {
			return fmt.Errorf("the checksum of %s: %w", r.Version, err)
		}
		doc.Releases = append(doc.Releases, Release{Channel: r.Channel, Version: r.Version, URL: r.URL, MD5: sum})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o644)
}
