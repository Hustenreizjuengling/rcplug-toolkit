//go:build linux

package flash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonCheck(t *testing.T) {
	// An interpreter that reports an old version is refused with that version.
	old := filepath.Join(t.TempDir(), "python")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho 2.7.18\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if c := pythonCheck(old); c.OK || !strings.Contains(c.Detail, "Python 2.7.18") {
		t.Fatalf("pythonCheck(old) = %+v", c)
	}
	// Something that exits 0 without printing a version is not Python.
	if c := pythonCheck("/bin/true"); c.OK {
		t.Fatalf("pythonCheck(/bin/true) = %+v", c)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 here")
	}
	if c := pythonCheck(py); !c.OK || !strings.Contains(c.Detail, "Python 3.") {
		t.Fatalf("pythonCheck(python3) = %+v", c)
	}
}

// Preflight on this machine never touches a device: with no RØDECaster on USB
// (as in CI) it reports that, next to the archive checks.
func TestPreflightWithoutDevice(t *testing.T) {
	devs, err := FindDevices()
	if err != nil || len(devs) > 0 {
		t.Skip("a RØDECaster is connected (or /sys is unreadable); not probing it")
	}
	r := Preflight(Options{StageDir: stage(t, duoImage("ssh-ed25519 AAAA me\n")), NoPrompt: true})
	var ids []string
	for _, c := range r.Checks {
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, ","); got != "archive,image,device,mount,python" {
		t.Fatalf("checks = %s", got)
	}
	if dev, _ := check(r, "device"); dev.OK || dev.Detail != ErrNoDevice.Error() {
		t.Fatalf("device check = %+v", dev)
	}
	if _, err := Prepare(Options{StageDir: r.Staging.Dir}); err != ErrNoDevice {
		t.Fatalf("Prepare = %v, want ErrNoDevice", err)
	}
}
