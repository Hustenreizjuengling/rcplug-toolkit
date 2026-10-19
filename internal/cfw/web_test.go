//go:build !js

package cfw

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// webDriver runs the web page's patcher (cmd/rcplug-web) under Node the way
// the page does: the image as a Blob, the archive handed out in chunks.
const webDriver = `
const fs = require('fs'), path = require('path');
const [execJs, wasm, image, key, out, answer] = process.argv.slice(2);
require(execJs);
(async () => {
  const go = new Go();
  const { instance } = await WebAssembly.instantiate(fs.readFileSync(wasm), go.importObject);
  go.run(instance);
  const chunks = [];
  let asked = null;
  try {
    const r = await globalThis.rcplugWeb.prepare({
      file: await fs.openAsBlob(image), keys: [fs.readFileSync(key, 'utf8')],
      onChunk: (u8) => chunks.push(Buffer.from(u8)),
      onChecked: (info) => { asked = info; return Promise.resolve(answer === 'yes'); },
    });
    fs.writeFileSync(out, Buffer.concat(chunks));
    console.log(JSON.stringify({ ok: true, digest: r.digest, md5Line: r.md5Line, asked }));
  } catch (e) {
    console.log(JSON.stringify({ ok: false, error: e.message, chunks: chunks.length, asked }));
  }
  process.exit(0);
})();
`

// The browser's archive is byte for byte the command line's; the page's
// question after the checks can stop it before anything is written.
func TestWebPatcherMatchesBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the WebAssembly module")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("needs node")
	}
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skip("needs the go command")
	}
	execJs := ""
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		if p := filepath.Join(strings.TrimSpace(string(root)), rel); fileExists(p) {
			execJs = p
		}
	}
	if execJs == "" {
		t.Skip("no wasm_exec.js in GOROOT")
	}
	dir := t.TempDir()
	wasm := filepath.Join(dir, "rcplug-web.wasm")
	cmd := exec.Command("go", "build", "-o", wasm, "../../cmd/rcplug-web")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the module: %v\n%s", err, b)
	}
	driver := filepath.Join(dir, "driver.cjs")
	os.WriteFile(driver, []byte(webDriver), 0o644)

	img := buildImage(t, entry{tar.Header{Typeflag: tar.TypeReg, Name: "./usr/lib/big", Mode: 0o644}, randomBytes(6 << 20)}) // more than one 4 MB slice, compressed
	key := writeKey(t, testKey)
	want, err := build(img, filepath.Join(dir, "cli"), []string{testKey}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	cliOut, _ := os.ReadFile(want.ArchivePath)

	run := func(answer string) string {
		out := filepath.Join(dir, "web-"+answer)
		c := exec.Command(node, driver, execJs, wasm, img, key, out, answer)
		c.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")} // Go's wasm takes little environment
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, b)
		}
		return string(b)
	}
	res := run("yes")
	if !strings.Contains(res, `"ok":true`) || !strings.Contains(res, `"md5":"`+want.InputDigest+`"`) {
		t.Fatalf("web run: %s", res)
	}
	webOut, _ := os.ReadFile(filepath.Join(dir, "web-yes"))
	if !bytes.Equal(webOut, cliOut) {
		t.Fatalf("the web archive (%d bytes) differs from the command line's (%d bytes)", len(webOut), len(cliOut))
	}
	if !strings.Contains(res, `"digest":"`+want.Digest+`"`) {
		t.Fatalf("digest: %s, want %s", res, want.Digest)
	}
	// "Stop" after the checks: nothing written.
	if res := run("no"); !strings.Contains(res, `"error":"stopped"`) || !strings.Contains(res, `"chunks":0`) {
		t.Fatalf("stopped run: %s", res)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, fs.ErrNotExist)
}

// randomBytes does not compress, so the image spans several of the page's slices.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(b)
	return b
}
