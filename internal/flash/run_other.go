//go:build !linux && !windows

package flash

import "context"

// Run is unsupported here: staging needs the device's HID interface and its
// update disk, which rcplug reaches only on Linux and Windows. See
// ErrUnsupportedOS.
func Run(ctx context.Context, o Options) error { return ErrUnsupportedOS }
