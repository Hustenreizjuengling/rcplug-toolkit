// Command mkzip packs files into a zip without needing a zip program:
//
//	go run ./tools/mkzip out.zip file... [dir/...]
//
// Files are stored under their base name; executables keep their mode.
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: mkzip out.zip file...")
		os.Exit(2)
	}
	out, err := os.Create(os.Args[1])
	if err != nil {
		fail(err)
	}
	z := zip.NewWriter(out)
	for _, path := range os.Args[2:] {
		info, err := os.Stat(path)
		if err != nil {
			fail(err)
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			fail(err)
		}
		h.Name, h.Method = filepath.Base(path), zip.Deflate
		w, err := z.CreateHeader(h)
		if err != nil {
			fail(err)
		}
		f, err := os.Open(path)
		if err != nil {
			fail(err)
		}
		if _, err := io.Copy(w, f); err != nil {
			fail(err)
		}
		f.Close()
	}
	if err := z.Close(); err != nil {
		fail(err)
	}
	if err := out.Close(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "mkzip:", err)
	os.Exit(1)
}
