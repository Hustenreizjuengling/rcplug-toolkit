// Package installer holds the device-side scripts. RemoteScript is embedded
// into rcplug-setup, so the program works without files next to it.
package installer

import _ "embed"

// RemoteScript is rcplug-remote.sh, the device side of every setup tool.
//
//go:embed rcplug-remote.sh
var RemoteScript []byte
