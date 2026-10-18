// Command mkzip packs files into a zip without needing a zip program:
//
//	go run ./tools/mkzip out.zip file...
//
// Files are stored under their base name; executables keep their mode.
// Windows text files (.cmd, .bat, .txt) get CRLF line endings, whatever the
// checkout has: cmd.exe misreads batch files with bare LF.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: mkzip out.zip file...")
		os.Exit(2)
	}
	if err := pack(os.Args[1], os.Args[2:]); err != nil {
		_ = os.Remove(os.Args[1]) // no half-written zip
		fmt.Fprintln(os.Stderr, "mkzip:", err)
		os.Exit(1)
	}
}

func pack(zipPath string, files []string) error {
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer out.Close()
	z := zip.NewWriter(out)
	seen := map[string]string{}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory; list its files instead", path)
		}
		name := filepath.Base(path)
		if other, dup := seen[name]; dup {
			return fmt.Errorf("%s and %s would both be stored as %s", other, path, name)
		}
		seen[name] = path
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".cmd", ".bat", ".txt":
			data = crlf(data)
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name, h.Method = name, zip.Deflate
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	return out.Close()
}

// crlf turns every line ending into CRLF.
func crlf(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}
