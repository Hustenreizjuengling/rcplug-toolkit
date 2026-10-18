//go:build linux

package flash

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Supported reports whether this system can flash: on Linux it can.
func Supported() error { return nil }

// hostChecks checks access to the HID node, how the update disk will be
// mounted, and Python.
func (r *Report) hostChecks(o Options) {
	if r.Node != "" {
		const rOK, wOK = 4, 2 // access(2) modes
		if err := syscall.Access(r.Node, rOK|wOK); err != nil {
			r.add(fail("access", fmt.Errorf("no read/write access to %s (%v): install the udev rule (rcplug-flash --print-udev-rule), then replug the device", r.Node, err)))
		} else {
			r.add(pass("access", r.Node+" is readable and writable"))
		}
	}
	r.add(mountCheck(o.NoPrompt))
	py, err := FindPython(o.Python)
	if err != nil {
		r.add(fail("python", err))
		return
	}
	r.Python = py
	r.add(pythonCheck(py))
}

// mountCheck says how flash_cfw.py will mount the update disk: with udisksctl
// if it is installed, else with sudo mount. Without a terminal (noPrompt) sudo
// must not need a password, as nobody could type it.
func mountCheck(noPrompt bool) Check {
	if p, err := exec.LookPath("udisksctl"); err == nil {
		return pass("mount", "udisksctl ("+p+")")
	}
	if os.Geteuid() == 0 {
		return pass("mount", "sudo mount (running as root)")
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fail("mount", errors.New("neither udisksctl nor sudo found: install udisks2, or run as root"))
	}
	if !noPrompt {
		return pass("mount", "sudo mount (asks for your password)")
	}
	if exec.Command("sudo", "-n", "true").Run() == nil {
		return pass("mount", "sudo mount (no password needed)")
	}
	return fail("mount", errors.New("udisksctl is not installed and sudo needs a password, which cannot be typed here: install udisks2 (udisksctl), or use rcplug-flash in a terminal"))
}

// pythonCheck checks the interpreter is Python 3.8 or newer, which the
// flash scripts need.
func pythonCheck(py string) Check {
	out, err := exec.Command(py, "-I", "-c",
		"import sys; print('%d.%d.%d' % sys.version_info[:3]); sys.exit(sys.version_info < (3, 8))").Output()
	v := strings.TrimSpace(string(out))
	switch {
	case err == nil:
		return pass("python", py+" (Python "+v+")")
	case v != "":
		return fail("python", fmt.Errorf("%s is Python %s; the flash scripts need 3.8 or newer", py, v))
	default:
		return fail("python", fmt.Errorf("%s does not run as Python 3: %v", py, err))
	}
}
