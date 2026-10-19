package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The words sent to the device's shell arrive there exactly as given.
func TestShellQuoteKeepsEveryWord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell, like the device")
	}
	words := []string{"addkey", "ssh-ed25519", "AAAAC3Nz", "Sam's laptop", `$(touch /tmp/x)`, "`id`", `a"b\c`, "", "two  spaces", "*"}
	line := `printf '%s\n'`
	for _, w := range words {
		line += " " + shellQuote(w)
	}
	out, err := exec.Command("sh", "-c", line).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if !reflect.DeepEqual(got, words) {
		t.Fatalf("the shell got %q, want %q", got, words)
	}
}

func TestSessionRefusesOptionLikeTargets(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh here")
	}
	for _, c := range []struct{ user, host string }{
		{"root", "-oProxyCommand"}, {"-E", "x"}, {"root", "-v"}, {"root", "dev ice"}, {"root", ""}, {"", "duo"},
	} {
		if s, err := newSession(c.user, c.host, ""); err == nil {
			s.close()
			t.Errorf("newSession(%q, %q) was accepted", c.user, c.host)
		}
	}
	for _, c := range []struct{ user, host string }{
		{"root", "192.168.1.20"}, {"root", "rodecaster.local"}, {"root", "::1"}, {"_svc", "duo-1"},
	} {
		s, err := newSession(c.user, c.host, "")
		if err != nil {
			t.Errorf("newSession(%q, %q): %v", c.user, c.host, err)
			continue
		}
		s.close()
	}
}

// A key saved by a browser (0644 or 0664) becomes owner-only, as ssh wants.
func TestProtectKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permissions")
	}
	key := filepath.Join(t.TempDir(), "rodecaster")
	os.WriteFile(key, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"), 0o664)
	os.Chmod(key, 0o664)
	protectKey(key)
	if fi, _ := os.Stat(key); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}
