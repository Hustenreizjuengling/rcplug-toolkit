// Command rcplug-flash puts a prepared RØDECaster Duo firmware archive onto the
// device over USB. It is step 3 of enabling SSH; prepare the archive first with
// rcplug-cfw (steps 1–2). See docs/enable-ssh.md for the whole procedure and
// its risks.
//
//	rcplug-flash --detect                 # find the device on USB and exit
//	rcplug-flash out/                     # stage the archive onto the device (no flash)
//	rcplug-flash out/ --flash             # stage, then flash and reboot (asks first)
//	rcplug-flash out/ --flash --yes       # ... without the confirmation prompt
//
// rcplug-flash detects the device, checks the staged archive.tar.gz +
// archive.md5, and then runs the device's update procedure from the proven
// rodecaster-duo-ssh tool (MIT, © Guid-Lab). On Linux it runs that tool's
// vendored scripts; they need Python 3 and access to the HID node (install
// internal/flash/scripts/70-rode-hidraw.rules, or run as root). On Windows,
// where the scripts cannot run, rcplug-flash does the same steps itself through
// the Windows APIs; close the RØDECaster App first. The destructive flash
// command ('U', which overwrites the inactive firmware partition and reboots) is
// sent only with --flash and after you confirm.
//
// The device keeps two firmware partitions (A/B): a bad flash of one still
// boots from the other.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/flash"
)

var version = "dev"

func main() {
	detect := flag.Bool("detect", false, "only look for the RØDECaster on USB, then exit")
	doFlash := flag.Bool("flash", false, "after staging, flash the archive and reboot (DESTRUCTIVE: overwrites a firmware partition)")
	yes := flag.Bool("yes", false, "skip the confirmation prompt for --flash")
	node := flag.String("node", "", "HID interface to use: hidraw node on Linux, device path on Windows (default: auto-detect by USB vendor 19f7)")
	drive := flag.String("drive", "", "Windows: drive letter of the update disk, e.g. E: (default: the new FAT drive on the Duo in update mode; only if that check is wrong)")
	python := flag.String("python", "", "Linux: Python 3 interpreter for the flash scripts (default: python3)")
	printUdev := flag.Bool("print-udev-rule", false, "print the udev rule for non-root HID access and exit")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	args := parseFlags(flag.CommandLine, os.Args[1:])

	if *showVer {
		fmt.Println(version)
		return
	}
	if *printUdev {
		os.Stdout.Write(flash.UdevRule())
		return
	}
	if *detect {
		if err := detectOnly(); err != nil {
			fatal(err)
		}
		return
	}

	if len(args) != 1 {
		usage()
		os.Exit(2)
	}
	stageDir := args[0]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *drive != "" && runtime.GOOS != "windows" {
		fatal(fmt.Errorf("--drive is for Windows; on %s the flash scripts find the update disk themselves", runtime.GOOS))
	}
	opts := flash.Options{StageDir: stageDir, Node: *node, Drive: *drive, Python: *python, Flash: *doFlash}

	// Pre-flight: check the archive and find the device without sending anything.
	plan, err := flash.Prepare(opts)
	if err != nil {
		fatal(err)
	}
	printPlan(plan, *doFlash)

	if *doFlash && !*yes {
		if !confirm() {
			fmt.Println("aborted; nothing was sent to the device.")
			return
		}
	}

	if err := flash.Run(ctx, opts); err != nil {
		fatal(err)
	}

	if *doFlash {
		fmt.Println("\nflash command sent. The device writes its inactive partition (a few")
		fmt.Println("minutes) and reboots into the new firmware.")
		fmt.Println("then clear the old host key and connect (see docs/enable-ssh.md):")
		fmt.Println("  ssh-keygen -R <device-ip> && ssh -i <your key> root@<device-ip>")
	} else {
		fmt.Println("\nstaged only — no firmware was flashed. The device stays in update mode:")
		fmt.Println("switch it off and on again before the next run. To flash, then run:")
		fmt.Printf("  %s --flash %s\n", progName(), stageDir)
	}
}

func detectOnly() error {
	devs, err := flash.FindDevices()
	if err != nil {
		return err
	}
	if len(devs) == 0 {
		return flash.ErrNoDevice
	}
	fmt.Printf("found %d RØDECaster HID interface(s):\n", len(devs))
	for _, d := range devs {
		fmt.Println("  " + d.String())
	}
	return nil
}

func printPlan(p *flash.Plan, doFlash bool) {
	st := p.Staging
	fmt.Printf("archive:  %s (%d MB, md5 %s, matches %s)\n",
		st.Archive, st.Size>>20, st.Digest, filepath.Base(st.MD5Path))
	fmt.Printf("image:    %s, %s\n", p.Image.Describe(), p.Image.KeysText())
	if p.Device.Node == "" {
		fmt.Printf("device:   %s (from --node)\n", p.Node)
	} else {
		fmt.Printf("device:   %s\n", p.Device.String())
	}
	if p.Python != "" {
		fmt.Printf("python:   %s\n", p.Python)
	}
	for _, w := range p.Warnings {
		fmt.Printf("warning:  %s\n", w)
	}
	if doFlash {
		fmt.Println(strings.Repeat("-", 68))
		fmt.Println("WARNING: --flash overwrites a firmware partition on the device and")
		fmt.Println("reboots it. This is your decision for your own device. The A/B")
		fmt.Println("partitioning means a bad image still leaves the other partition")
		fmt.Println("bootable, but do not disconnect power during the flash.")
		fmt.Println(strings.Repeat("-", 68))
	}
}

func confirm() bool {
	fmt.Print("Send 'U' to FLASH now? Type 'flash' to proceed: ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	return strings.TrimSpace(sc.Text()) == "flash"
}

func usage() {
	fmt.Fprintf(os.Stderr, `rcplug-flash: put a prepared RØDECaster Duo firmware archive onto the device (step 3).

usage:
  rcplug-flash --detect              find the device on USB and exit
  rcplug-flash <dir>                 stage dir/archive.tar.gz onto the device (no flash)
  rcplug-flash <dir> --flash         stage, then flash and reboot (asks first)

<dir> is the output of:  rcplug-cfw --download <ver> --key <your key>.pub --out <dir>
Works on Windows and Linux with the device on USB. See docs/enable-ssh.md.

flags:
`)
	flag.PrintDefaults()
}

func progName() string { return filepath.Base(os.Args[0]) }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rcplug-flash: "+err.Error())
	os.Exit(1)
}

// parseFlags parses flags before and after the archive folder, so that both
// "rcplug-flash --flash out/" and "rcplug-flash out/ --flash" work (Go's flag
// package stops at the first argument that is not a flag). It returns the
// other arguments.
func parseFlags(fs *flag.FlagSet, args []string) []string {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil // ContinueOnError in tests; ExitOnError has exited
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}
