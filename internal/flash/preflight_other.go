//go:build !linux && !windows

package flash

// Supported reports whether this system can flash; see ErrUnsupportedOS.
func Supported() error { return ErrUnsupportedOS }

func (r *Report) hostChecks(o Options) {}
