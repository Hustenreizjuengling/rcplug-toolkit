//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// systemLang returns the user's locale name, e.g. "de-DE".
func systemLang() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
