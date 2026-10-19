//go:build js && wasm

// Command rcplug-web is the toolkit's firmware patcher (internal/cfw) for
// the browser, compiled to WebAssembly: the web page hands it RØDE's stock
// firmware as a File, it adds the SSH key exactly as rcplug-cfw does and
// hands back archive.tar.gz in pieces. Nothing leaves the browser.
//
// It defines globalThis.rcplugWeb with:
//
//	prepare({file, md5, keys, password, force, onChunk(Uint8Array), onProgress(phase, done, total),
//	         onChecked({md5, model, version}) → boolean or Promise<boolean>})
//	    → Promise<{digest, md5Line, keysAdded, sshdPatched, rootHome, inputChecked, inputMD5, model, version}>
//	inspect({file, onProgress(done, total)})
//	    → Promise<{md5, model, modelName, version, keys, vendorKeys, keyFingerprints}>
//	checkKey(line) → "" or the reason the public key is not usable
//	version → the toolkit's version
//
// onChecked is called once the image passed the checks, before anything is
// written: false stops (the page asks about files it does not know).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall/js"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/cfw"
)

// version is set at build time.
var version = "dev"

const chunk = 4 << 20 // bytes read from or handed to JavaScript at a time

// blobReader reads a JavaScript Blob, one slice at a time; it reports what
// it has read through progress.
type blobReader struct {
	blob     js.Value
	off      int64
	size     int64
	buf      []byte
	progress func(n int64)
}

func (b *blobReader) Read(p []byte) (int, error) {
	if len(b.buf) == 0 {
		if b.off >= b.size {
			return 0, io.EOF
		}
		end := b.off + chunk
		if end > b.size {
			end = b.size
		}
		data, err := await(b.blob.Call("slice", b.off, end).Call("arrayBuffer"))
		if err != nil {
			return 0, err
		}
		u8 := js.Global().Get("Uint8Array").New(data)
		if n := int64(u8.Get("length").Int()); n != end-b.off {
			return 0, fmt.Errorf("reading the file gave %d bytes instead of %d", n, end-b.off)
		}
		b.buf = make([]byte, end-b.off)
		js.CopyBytesToGo(b.buf, u8)
		b.off = end
		if b.progress != nil {
			b.progress(b.off)
		}
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}

func (b *blobReader) Close() error { return nil }

// await waits for a JavaScript promise (from a goroutine).
func await(p js.Value) (js.Value, error) {
	type res struct {
		v   js.Value
		err error
	}
	ch := make(chan res, 1)
	then := js.FuncOf(func(_ js.Value, args []js.Value) any { ch <- res{v: args[0]}; return nil })
	catch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "reading the file failed"
		if len(args) > 0 && args[0].Truthy() {
			msg = args[0].Call("toString").String()
		}
		ch <- res{err: errors.New(msg)}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	p.Call("then", then, catch)
	r := <-ch
	return r.v, r.err
}

// chunkWriter hands what it is given to a JavaScript function in pieces.
type chunkWriter struct {
	fn  js.Value
	buf []byte
}

func (c *chunkWriter) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	if len(c.buf) >= chunk {
		c.flush()
	}
	return len(p), nil
}

func (c *chunkWriter) flush() {
	if len(c.buf) == 0 {
		return
	}
	u8 := js.Global().Get("Uint8Array").New(len(c.buf))
	js.CopyBytesToJS(u8, c.buf)
	c.fn.Invoke(u8)
	c.buf = c.buf[:0]
}

// inspect reads an archive once, as the flash checks do before anything is
// sent to the device: what it is, its keys, its MD5 (a damaged one fails).
func inspect(_ js.Value, args []js.Value) any {
	return async(func() (any, error) {
		o := args[0]
		file := o.Get("file")
		if !file.Truthy() {
			return nil, errors.New("no file")
		}
		size := int64(file.Get("size").Float())
		progress := o.Get("onProgress")
		src := func() (io.ReadCloser, error) {
			return &blobReader{blob: file, size: size, progress: func(n int64) {
				if progress.Truthy() {
					progress.Invoke(float64(n), float64(size))
				}
			}}, nil
		}
		in, sum, err := cfw.Inspect(context.Background(), src)
		if err != nil {
			return nil, err
		}
		fps := make([]any, len(in.KeyFingerprints))
		for i, f := range in.KeyFingerprints {
			fps[i] = f
		}
		return map[string]any{"md5": sum, "model": in.ModelID, "modelName": in.ModelName, "version": in.Version,
			"keys": in.Keys, "vendorKeys": in.VendorKeys, "keyFingerprints": fps}, nil
	})
}

// async runs fn in a goroutine and settles a promise with its result; a
// panic (a failing JavaScript callback) rejects it instead of ending the
// program with the promise open.
func async(fn func() (any, error)) js.Value {
	promise, resolve, reject := newPromise()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprint("the patcher failed: ", r)))
			}
		}()
		res, err := fn()
		if err != nil {
			reject.Invoke(js.Global().Get("Error").New(err.Error()))
			return
		}
		resolve.Invoke(res)
	}()
	return promise
}

func prepare(_ js.Value, args []js.Value) any {
	return async(func() (any, error) { return run(args[0]) })
}

func run(o js.Value) (any, error) {
	file := o.Get("file")
	if !file.Truthy() {
		return nil, errors.New("no firmware file")
	}
	size := int64(file.Get("size").Float())
	var keys []string
	if k := o.Get("keys"); k.Truthy() {
		for i := 0; i < k.Length(); i++ {
			if line := strings.TrimSpace(k.Index(i).String()); line != "" {
				keys = append(keys, line)
			}
		}
	}
	progress := o.Get("onProgress")
	pass := 0
	src := func() (io.ReadCloser, error) {
		pass++
		phase := map[int]string{1: "check", 2: "write"}[pass]
		return &blobReader{blob: file, size: size, progress: func(n int64) {
			if progress.Truthy() {
				progress.Invoke(phase, float64(n), float64(size))
			}
		}}, nil
	}
	out := &chunkWriter{fn: o.Get("onChunk")}
	so := cfw.StreamOptions{
		PublicKeys:         keys,
		EnablePasswordAuth: o.Get("password").Truthy(),
		Force:              o.Get("force").Truthy(),
		ExpectMD5:          str(o.Get("md5")),
		ExpectFrom:         "the checksum RØDE publishes for it",
		Name:               str(file.Get("name")),
		Context:            context.Background(),
	}
	if fn := o.Get("onChecked"); fn.Type() == js.TypeFunction {
		so.Checked = func(r *cfw.Result) error {
			model, ver := "", ""
			if r.Image != nil {
				model, ver = r.Image.ModelName, r.Image.Version
			}
			v := fn.Invoke(map[string]any{"md5": r.InputDigest, "model": model, "version": ver})
			if v.InstanceOf(js.Global().Get("Promise")) {
				var err error
				if v, err = await(v); err != nil {
					return err
				}
			}
			if !v.Truthy() {
				return errStopped
			}
			return nil
		}
	}
	r, err := cfw.BuildTo(so, src, out)
	if err != nil {
		return nil, err
	}
	out.flush()
	model, ver := "", ""
	if r.Image != nil {
		model, ver = r.Image.ModelName, r.Image.Version
	}
	return map[string]any{
		"digest": r.Digest, "md5Line": cfw.MD5Line("", r.Digest), "keysAdded": r.KeysAdded, "sshdPatched": r.SSHDPatched,
		"rootHome": r.RootHome, "inputChecked": r.InputMD5, "inputMD5": r.InputDigest, "model": model, "version": ver,
	}, nil
}

var errStopped = errors.New("stopped")

func str(v js.Value) string {
	if v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

func newPromise() (promise, resolve, reject js.Value) {
	var res, rej js.Value
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { res, rej = args[0], args[1]; return nil })
	defer executor.Release()
	p := js.Global().Get("Promise").New(executor)
	return p, res, rej
}

func main() {
	js.Global().Set("rcplugWeb", map[string]any{
		"version": version,
		"prepare": js.FuncOf(prepare),
		"inspect": js.FuncOf(inspect),
		"checkKey": js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return "no key"
			}
			if err := cfw.ValidatePublicKey(strings.TrimSpace(args[0].String())); err != nil {
				return err.Error()
			}
			return ""
		}),
	})
	select {} // keep the functions alive
}
