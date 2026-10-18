//go:build linux

package flash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Run stages the archive onto the device's update disk and, when o.Flash is
// set, triggers the flash. It drives the vendored flash_cfw.py; the 'U' flash
// command is sent only when o.Flash is true (the caller confirms first). Script
// output is streamed to o.Stdout/o.Stderr.
func Run(ctx context.Context, o Options) error {
	p, err := Prepare(o)
	if err != nil {
		return err
	}
	dir, cleanup, err := extractScripts()
	if err != nil {
		return err
	}
	defer cleanup()

	// Name the node Preflight checked, so the script cannot pick another one.
	args := []string{filepath.Join(dir, "flash_cfw.py"), o.StageDir, "--node", p.Node}
	if o.Flash {
		// The operator has already confirmed in the caller; --yes stops the
		// script from prompting on a stdin we do not attach.
		args = append(args, "--flash", "--yes")
	}
	cmd := exec.CommandContext(ctx, p.Python, args...)
	cmd.Stdout = orStdout(o.Stdout)
	cmd.Stderr = orStderr(o.Stderr)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1") // stream lines into a pipe, too
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flash script failed: %w", err)
	}
	return nil
}

// extractScripts writes the vendored scripts into a fresh 0700 temp directory
// and returns it with a cleanup function. Both scripts land together so
// flash_cfw.py can import rode_hid.
func extractScripts() (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "rcplug-flash-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	for _, name := range []string{"rode_hid.py", "flash_cfw.py"} {
		data, rerr := scriptFS.ReadFile("scripts/" + name)
		if rerr != nil {
			cleanup()
			return "", nil, rerr
		}
		if werr := os.WriteFile(filepath.Join(dir, name), data, 0o600); werr != nil {
			cleanup()
			return "", nil, werr
		}
	}
	return dir, cleanup, nil
}
