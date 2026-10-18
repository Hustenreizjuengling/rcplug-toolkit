// Command rcplug-cfw prepares a RØDECaster Duo firmware image with your SSH
// public key, so you can reach the device over SSH (the Duo offers no SSH
// access out of the box). See docs/enable-ssh.md for the whole procedure and
// its risks.
//
//	rcplug-cfw --devices                                  # RØDECaster models rcplug knows
//	rcplug-cfw --list                                     # firmware on RØDE's server
//	rcplug-cfw --download latest                          # fetch the stock image
//	rcplug-cfw --inspect rodecaster-duo-1.7.3-os.tar.gz   # which model and version is this?
//	rcplug-cfw --download 1.7.3 --key ~/.ssh/rodecaster.pub --out out/
//	rcplug-cfw --in stock-os.tar.gz --key ~/.ssh/rodecaster.pub --out out/
//
// The stock firmware is a plain gzipped root filesystem (os.tar.gz) that RØDE
// publishes with an MD5; --download fetches it from update.rode.com and checks
// it. Every image is checked to be for the chosen --device (default: duo)
// before it is used. The output is archive.tar.gz + archive.md5, ready for the
// USB flash step, which rcplug does not do.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/cfw"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwdl"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

var version = "dev"

type fileList []string

func (k *fileList) String() string { return strings.Join(*k, ", ") }
func (k *fileList) Set(v string) error {
	*k = append(*k, v)
	return nil
}

func main() {
	var keyFiles fileList
	in := flag.String("in", "", "stock firmware image (os.tar.gz) to add the key to")
	download := flag.String("download", "", "fetch the stock firmware from RØDE first: latest, beta or a version like 1.7.3")
	downloadDir := flag.String("download-dir", ".", "where --download saves the stock image")
	list := flag.Bool("list", false, "show the firmware versions RØDE currently offers for --device and exit")
	devices := flag.Bool("devices", false, "show the RØDECaster models rcplug knows and exit")
	inspect := flag.String("inspect", "", "show which model and version a firmware image is for, and exit")
	device := flag.String("device", "duo", "the device the firmware is for (see --devices)")
	out := flag.String("out", "out", "output directory for archive.tar.gz + archive.md5")
	flag.Var(&keyFiles, "key", "SSH public key file to add to root (repeatable)")
	enablePw := flag.Bool("enable-password-auth", false, "also allow password logins (edits sshd_config); off by default. On the Duo this alone gives no access: root's password is set by RØDE and not public")
	md5Format := flag.String("md5-format", "{hash}", "content of archive.md5; {hash} and {name} expand (stock is a bare digest)")
	force := flag.Bool("force", false, "continue with an image that has no /etc/passwd or names no known model (never one for another model)")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVer {
		fmt.Println(version)
		return
	}
	fwdl.UserAgent = "rcplug-cfw/" + version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *devices {
		for _, m := range fwimage.Models {
			note := ""
			if m.Note != "" {
				note = " — " + m.Note
			}
			fmt.Printf("%-11s %-24s %s%s\n", m.ID, m.Name, m.Support, note)
		}
		return
	}
	if *inspect != "" {
		in, err := fwimage.InspectContext(ctx, *inspect)
		if err != nil {
			fatal(err)
		}
		fmt.Println(in.Describe())
		fmt.Printf("hostname %q, device tree %v, %d entries\n", in.Hostname, in.DTBs, in.Entries)
		fmt.Println(in.KeysText())
		return
	}
	model, err := fwimage.ModelByID(*device)
	if err != nil {
		fatal(err)
	}

	if *list {
		rels, err := fwdl.Latest(ctx, fwdl.Client(), model)
		if err != nil {
			fatal(err)
		}
		for _, r := range rels {
			fmt.Printf("%-7s %s\n", r.Channel, r.Version)
		}
		fmt.Println("older versions: --download <version>, e.g. the one your device runs")
		return
	}
	if *download != "" && *in != "" {
		fatal(fmt.Errorf("give either --in or --download, not both"))
	}
	if *download == "" && (*in == "" || (len(keyFiles) == 0 && !*enablePw)) {
		flag.Usage()
		os.Exit(2)
	}

	// Validate the keys before a long download.
	var keys []string
	for _, p := range keyFiles {
		raw, err := os.ReadFile(p)
		if err != nil {
			fatal(err)
		}
		if err := cfw.ValidatePublicKey(strings.TrimSpace(string(raw))); err != nil {
			fatal(fmt.Errorf("%s: %w", p, err))
		}
		keys = append(keys, strings.TrimSpace(string(raw)))
	}

	if (len(keys) > 0 || *enablePw) && model.Support != fwimage.Full {
		fatal(fmt.Errorf("adding a key is not supported for the %s yet (%s)", model.Name, model.Support))
	}
	if *download != "" {
		path, err := fetch(ctx, model, *download, *downloadDir)
		if err != nil {
			fatal(err)
		}
		if len(keys) == 0 && !*enablePw {
			fmt.Println("next: rcplug-cfw --in " + path + " --key <your key>.pub")
			return
		}
		*in = path
	}

	res, err := cfw.Build(cfw.Options{Context: ctx, Input: *in, Model: model.ID, OutDir: *out, PublicKeys: keys,
		EnablePasswordAuth: *enablePw, MD5Format: *md5Format, Force: *force})
	if err != nil {
		fatal(err)
	}
	check := ""
	if res.InputMD5 {
		check = ", matches its .md5"
	}
	fmt.Printf("input: %s%s\n", res.Image.Describe(), check)
	fmt.Printf("added %d key(s) to %s/.ssh/authorized_keys\n", res.KeysAdded, res.RootHome)
	if *enablePw {
		fmt.Printf("password logins enabled in %d sshd_config file(s)\n", res.SSHDPatched)
	}
	fmt.Printf("wrote %s (%s)\n", res.ArchivePath, res.Digest)
	fmt.Printf("wrote %s\n", res.MD5Path)
	fmt.Printf("next: flash it over USB (Windows or Linux) with  rcplug-flash %s  (or step 3 of rcplug-setup); see docs/enable-ssh.md\n", filepath.Dir(res.ArchivePath))
}

// fetch downloads the stock image named by what (latest, beta or a version).
func fetch(ctx context.Context, m fwdl.Model, what, dir string) (string, error) {
	c := fwdl.Client()
	var rel fwdl.Release
	switch what {
	case "latest", "stable", "beta":
		rels, err := fwdl.Latest(ctx, c, m)
		if err != nil {
			return "", err
		}
		rel = rels[0]
		if what == "beta" {
			if len(rels) < 2 {
				fmt.Println("no separate beta right now; using the stable release")
			} else {
				rel = rels[1]
			}
		}
	default:
		var err error
		if rel, err = fwdl.ForVersion(m, what); err != nil {
			return "", err
		}
	}
	fmt.Printf("downloading %s firmware %s from %s\n", m.Name, rel.Version, rel.URL)
	pct := -1
	res, err := fwdl.Download(ctx, c, m, rel, dir, func(done, total int64) {
		if total <= 0 {
			return
		}
		if p := int(done * 100 / total); p != pct {
			pct = p
			fmt.Fprintf(os.Stderr, "\r  %3d%%  %d / %d MB", p, done>>20, total>>20)
		}
	})
	if pct >= 0 {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return "", err
	}
	if res.Reused {
		fmt.Printf("already downloaded and intact: %s\n", res.Path)
	} else {
		fmt.Printf("saved %s (md5 %s, verified)\n", res.Path, res.MD5)
	}
	fmt.Printf("checked: %s\n", res.Image.Describe())
	return res.Path, nil
}

func fatal(err error) {
	if errors.Is(err, context.Canceled) {
		err = errors.New("cancelled; nothing was written")
	}
	fmt.Fprintln(os.Stderr, "rcplug-cfw: "+err.Error())
	os.Exit(1)
}
