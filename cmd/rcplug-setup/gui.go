package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/cfw"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/flash"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwdl"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

//go:embed web
var webFS embed.FS

// gui is the local web-UI server. It runs on 127.0.0.1, guarded by a random
// token, and drives the same operations as the CLI on the user's own machine.
type gui struct {
	version string
	token   string
	def     guiDefaults

	mu   sync.Mutex
	sess *session // set by /api/connect
	idle *time.Timer
	quit chan struct{}

	dl download // the firmware download, one at a time
	fl flashJob // the USB stage/flash run, one at a time
}

// download is the state of a firmware download running in the background;
// the page polls it for progress.
type download struct {
	mu          sync.Mutex
	State       string // "", "running", "done", "error"
	Version     string
	Done, Total int64
	Result      *fwdl.Result
	Error       string
	cancel      context.CancelFunc
}

// guiDefaults are values from the command line that the page starts with.
type guiDefaults struct {
	Host, User, Key, Payload string
}

func runGUI(version string, def guiDefaults) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	g := &gui{version: version, token: hex.EncodeToString(tok), quit: make(chan struct{}), def: def}
	g.idle = time.AfterFunc(2*time.Hour, g.idleQuit)
	fwdl.UserAgent = "rcplug-setup/" + version

	srv := &http.Server{Handler: g.routes()}
	go func() { _ = srv.Serve(ln) }()

	url := fmt.Sprintf("http://%s/?t=%s&lang=%s", ln.Addr(), g.token, curLang())
	fmt.Printf("\n  rcplug setup %s\n\n", g.version)
	fmt.Println("  " + tr("guiServing"))
	fmt.Println("    " + url)
	if err := openBrowser(url); err != nil {
		fmt.Println("  " + tr("guiOpenManually"))
	}
	fmt.Println("  " + tr("guiQuit"))

	// Ctrl+C or a terminating signal end the setup like Quit, so the SSH
	// connection and its temporary folder are cleaned up; never during a
	// flash, which must not be cut off.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		for range sig {
			if g.fl.running() {
				fmt.Println("  " + tr("guiFlashBusy"))
				continue
			}
			g.stop()
			return
		}
	}()

	<-g.quit
	time.Sleep(200 * time.Millisecond) // let the quit response flush
	g.dl.mu.Lock()
	if g.dl.cancel != nil {
		g.dl.cancel()
	}
	g.dl.mu.Unlock()
	_ = srv.Close()
	if g.sess != nil {
		g.sess.close()
	}
	return nil
}

// routes serves the page and its API.
func (g *gui) routes() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/env", g.guard(g.handleEnv))
	mux.HandleFunc("GET /api/browse", g.guard(g.handleBrowse))
	mux.HandleFunc("POST /api/connect", g.guard(g.handleConnect))
	mux.HandleFunc("POST /api/genkey", g.guard(g.handleGenKey))
	mux.HandleFunc("POST /api/cfw", g.guard(g.handleCFW))
	mux.HandleFunc("GET /api/devices", g.guard(g.handleDevices))
	mux.HandleFunc("GET /api/inspect", g.guard(g.handleInspect))
	mux.HandleFunc("GET /api/firmware", g.guard(g.handleFirmware))
	mux.HandleFunc("POST /api/firmware/download", g.guard(g.handleFirmwareDownload))
	mux.HandleFunc("GET /api/firmware/progress", g.guard(g.handleFirmwareProgress))
	mux.HandleFunc("POST /api/firmware/cancel", g.guard(g.handleFirmwareCancel))
	mux.HandleFunc("GET /api/flash/check", g.guard(g.handleFlashCheck))
	mux.HandleFunc("POST /api/flash/start", g.guard(g.handleFlashStart))
	mux.HandleFunc("GET /api/flash/progress", g.guard(g.handleFlashProgress))
	mux.HandleFunc("POST /api/install", g.guard(g.sessionOp(func(s *session, r req) error { return s.install(r.Payload) })))
	mux.HandleFunc("POST /api/plugin", g.guard(g.sessionOp(func(s *session, r req) error { return s.plugin(r.Dir) })))
	mux.HandleFunc("POST /api/diagnostics", g.guard(g.handleDiagnostics))
	mux.HandleFunc("POST /api/uninstall", g.guard(g.sessionOp(func(s *session, r req) error { return s.device(s.out, "uninstall") })))
	mux.HandleFunc("POST /api/stop", g.guard(g.sessionOp(func(s *session, r req) error { return s.device(s.out, "stop") })))
	mux.HandleFunc("POST /api/resetaccess", g.guard(g.sessionOp(func(s *session, r req) error { return s.device(s.out, "resetaccess") })))
	mux.HandleFunc("POST /api/start", g.guard(g.sessionOp(func(s *session, r req) error { return s.device(s.out, "start") })))
	mux.HandleFunc("POST /api/pushkey", g.guard(g.handlePushKey))
	mux.HandleFunc("POST /api/quit", g.guard(g.handleQuit))
	return mux
}

// handleQuit ends the setup, but not while a flash runs: the scripts would
// be killed half-way.
func (g *gui) handleQuit(w http.ResponseWriter, r *http.Request) {
	if g.fl.running() {
		writeJSON(w, map[string]string{"error": tr("guiFlashBusy")})
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
	g.stop()
}

// idleQuit ends an abandoned setup, but never under a running flash.
func (g *gui) idleQuit() {
	if g.fl.running() {
		g.idle.Reset(10 * time.Minute)
		return
	}
	g.stop()
}

func (g *gui) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	select {
	case <-g.quit:
	default:
		close(g.quit)
	}
}

// guard checks the token and that the request is loopback, and resets the
// idle timer. Without this, any local page could drive ssh on this machine.
func (g *gui) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Setup-Token") != g.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		useLang(r.Header.Get("X-Setup-Lang")) // messages follow the page's language
		g.idle.Reset(2 * time.Hour)
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

// req is the union of fields the API accepts.
type req struct {
	Host, User, Key string
	Input, Out      string
	KeyPaths        []string
	Generate        bool
	Dir, Name       string
	Payload         string
	Version         string // firmware version to download, or
	Channel         string // "stable" / "beta"
	Device          string // fwimage model ID ("" = "duo")
	Flash           bool   // flash after staging (needs Confirm)
	Confirm         string // the word the user typed to confirm a flash
}

// model returns the device a request is about.
func model(id string) (fwimage.Model, error) {
	if id == "" {
		id = "duo"
	}
	return fwimage.ModelByID(id)
}

func decodeReq(r *http.Request) (req, error) {
	var in req
	if r.Body == nil {
		return in, nil
	}
	err := json.NewDecoder(r.Body).Decode(&in)
	for _, p := range []*string{&in.Key, &in.Input, &in.Out, &in.Dir, &in.Payload} {
		*p = expandHome(*p)
	}
	for i := range in.KeyPaths {
		in.KeyPaths[i] = expandHome(in.KeyPaths[i])
	}
	return in, err
}

// expandHome turns a leading ~ into the home directory, as a shell would;
// the page's placeholders suggest paths like ~/.ssh/rodecaster.pub.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (g *gui) handleEnv(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	payload := g.def.Payload
	if payload == "" {
		payload = newestPayload()
	}
	env := map[string]interface{}{
		"version": g.version, "os": runtime.GOOS, "lang": curLang(),
		"home": home, "cwd": cwd, "defaultPayload": payload,
		"flash": flash.Supported() == nil,
		"host":  g.def.Host, "user": g.def.User, "key": g.def.Key,
	}
	// After a reload of the page (the language switch reloads it) the
	// connection made before is still there.
	g.mu.Lock()
	if s := g.sess; s != nil {
		env["connected"] = map[string]interface{}{"target": s.target, "device": s.deviceInfo()}
	}
	g.mu.Unlock()
	writeJSON(w, env)
}

// sessionOp wraps an operation that needs a connected session, capturing its
// output. The decoded request is passed through.
func (g *gui) sessionOp(op func(*session, req) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in, err := decodeReq(r)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		g.mu.Lock()
		s := g.sess
		g.mu.Unlock()
		if s == nil {
			writeJSON(w, map[string]string{"error": tr("guiNotConnected")})
			return
		}
		var buf bytes.Buffer
		s.out = &buf
		opErr := op(s, in)
		resp := map[string]interface{}{"log": buf.String()}
		if opErr != nil {
			resp["error"] = opErr.Error()
		}
		writeJSON(w, resp)
	}
}

func (g *gui) handleConnect(w http.ResponseWriter, r *http.Request) {
	in, err := decodeReq(r)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	s, err := newSession(orDefault(in.User, "root"), in.Host, in.Key)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	var buf bytes.Buffer
	s.out = &buf
	if err := s.probe(); err != nil {
		s.close()
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	s.report()
	g.mu.Lock()
	if g.sess != nil {
		g.sess.close()
	}
	g.sess = s
	g.mu.Unlock()
	writeJSON(w, map[string]interface{}{"ok": true, "report": buf.String(), "device": s.deviceInfo()})
}

func (g *gui) handleGenKey(w http.ResponseWriter, r *http.Request) {
	in, _ := decodeReq(r)
	dir := in.Dir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".ssh")
	}
	name := in.Name
	if name == "" {
		name = "rodecaster"
	}
	if strings.ContainsAny(name, `/\.`) {
		writeJSON(w, map[string]string{"error": "the key name must be a plain file name"})
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	priv := filepath.Join(dir, name)
	if _, err := os.Stat(priv); err == nil {
		writeJSON(w, map[string]string{"error": tr("guiKeyExists") + " " + priv})
		return
	}
	cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "rodecaster", "-f", priv)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, map[string]string{"error": "ssh-keygen: " + strings.TrimSpace(string(out)+" "+err.Error())})
		return
	}
	pub, _ := os.ReadFile(priv + ".pub")
	writeJSON(w, map[string]interface{}{"ok": true, "privateKey": priv, "publicKey": priv + ".pub", "publicKeyText": strings.TrimSpace(string(pub))})
}

func (g *gui) handleCFW(w http.ResponseWriter, r *http.Request) {
	in, err := decodeReq(r)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	var keys []string
	for _, p := range in.KeyPaths {
		raw, err := os.ReadFile(p)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		keys = append(keys, strings.TrimSpace(string(raw)))
	}
	out := in.Out
	if out == "" {
		out = filepath.Join(filepath.Dir(in.Input), "rcplug-cfw-out")
	}
	res, err := cfw.Build(cfw.Options{Input: in.Input, Model: in.Device, OutDir: out, PublicKeys: keys})
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "outDir": out, "archive": res.ArchivePath, "md5": res.MD5Path,
		"digest": res.Digest, "keysAdded": res.KeysAdded, "rootHome": res.RootHome,
		"image": res.Image, "imageText": res.Image.Describe(), "inputMD5": res.InputMD5})
}

// handleDevices lists the RØDECaster models and what rcplug can do with each.
func (g *gui) handleDevices(w http.ResponseWriter, r *http.Request) {
	type dev struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Download bool   `json:"download"` // stock firmware can be fetched
		Keys     bool   `json:"keys"`     // a key can be added
		Note     string `json:"note,omitempty"`
	}
	var out []dev
	for _, m := range fwimage.Models {
		out = append(out, dev{ID: m.ID, Name: m.Name, Download: m.Support != fwimage.Unsupported,
			Keys: m.Support == fwimage.Full, Note: m.Note})
	}
	writeJSON(w, map[string]interface{}{"devices": out})
}

// handleInspect says which model and version an image on disk is for.
func (g *gui) handleInspect(w http.ResponseWriter, r *http.Request) {
	m, err := model(r.URL.Query().Get("device"))
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	in, err := fwimage.Inspect(expandHome(r.URL.Query().Get("path")))
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]interface{}{"image": in, "text": in.Describe()}
	if err := in.Check(m, ""); err != nil {
		resp["mismatch"] = err.Error()
	}
	writeJSON(w, resp)
}

// handleFirmware lists the firmware RØDE currently offers for a device.
func (g *gui) handleFirmware(w http.ResponseWriter, r *http.Request) {
	m, err := model(r.URL.Query().Get("device"))
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	rels, err := fwdl.Latest(r.Context(), fwdl.Client(), m)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"model": m.Name, "releases": rels})
}

// handleFirmwareDownload starts downloading a release (by channel or version)
// into a folder; /api/firmware/progress reports how it goes.
func (g *gui) handleFirmwareDownload(w http.ResponseWriter, r *http.Request) {
	in, err := decodeReq(r)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	m, err := model(in.Device)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	var rel fwdl.Release
	if in.Version != "" {
		rel, err = fwdl.ForVersion(m, in.Version)
	} else {
		var rels []fwdl.Release
		rels, err = fwdl.Latest(r.Context(), fwdl.Client(), m)
		if err == nil {
			rel = rels[0]
			for _, x := range rels {
				if x.Channel == in.Channel {
					rel = x
				}
			}
		}
	}
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	dir := in.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}

	g.dl.mu.Lock()
	defer g.dl.mu.Unlock()
	if g.dl.State == "running" {
		writeJSON(w, map[string]string{"error": "a download is already running"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.dl.State, g.dl.Version, g.dl.Done, g.dl.Total = "running", rel.Version, 0, 0
	g.dl.Result, g.dl.Error, g.dl.cancel = nil, "", cancel
	go func() {
		defer cancel()
		res, err := fwdl.Download(ctx, fwdl.Client(), m, rel, dir, func(done, total int64) {
			g.dl.mu.Lock()
			g.dl.Done, g.dl.Total = done, total
			g.dl.mu.Unlock()
		})
		g.dl.mu.Lock()
		defer g.dl.mu.Unlock()
		g.dl.cancel = nil
		switch {
		case ctx.Err() != nil:
			g.dl.State, g.dl.Error = "error", "cancelled"
		case err != nil:
			g.dl.State, g.dl.Error = "error", err.Error()
		default:
			g.dl.State, g.dl.Result = "done", res
		}
	}()
	writeJSON(w, map[string]interface{}{"ok": true, "version": rel.Version, "url": rel.URL})
}

func (g *gui) handleFirmwareProgress(w http.ResponseWriter, r *http.Request) {
	g.dl.mu.Lock()
	defer g.dl.mu.Unlock()
	writeJSON(w, map[string]interface{}{"state": g.dl.State, "version": g.dl.Version,
		"done": g.dl.Done, "total": g.dl.Total, "result": g.dl.Result, "failure": g.dl.Error})
}

func (g *gui) handleFirmwareCancel(w http.ResponseWriter, r *http.Request) {
	g.dl.mu.Lock()
	if g.dl.cancel != nil {
		g.dl.cancel()
	}
	g.dl.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func (g *gui) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	in, _ := decodeReq(r)
	g.mu.Lock()
	s := g.sess
	g.mu.Unlock()
	if s == nil {
		writeJSON(w, map[string]string{"error": tr("guiNotConnected")})
		return
	}
	// diagnostics() writes into the current directory; run it in the chosen dir.
	dir := in.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	defer os.Chdir(old)
	var buf bytes.Buffer
	s.out = &buf
	err := s.diagnostics()
	resp := map[string]interface{}{"log": buf.String()}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

func (g *gui) handlePushKey(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	s := g.sess
	g.mu.Unlock()
	if s == nil {
		writeJSON(w, map[string]string{"error": tr("guiNotConnected")})
		return
	}
	var buf bytes.Buffer
	s.out = &buf
	err := s.keys()
	resp := map[string]interface{}{"log": buf.String()}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

// handleBrowse lists a directory so the web UI can pick local files and folders.
func (g *gui) handleBrowse(w http.ResponseWriter, r *http.Request) {
	p := expandHome(r.URL.Query().Get("path"))
	if p == "" {
		p, _ = os.UserHomeDir()
	}
	p = filepath.Clean(p)
	ents, err := os.ReadDir(p)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	type item struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
	}
	var dirs, files []item
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") && e.Name() != ".ssh" {
			continue // hide dotfiles except ~/.ssh, where keys live
		}
		it := item{Name: e.Name(), Path: filepath.Join(p, e.Name()), Dir: e.IsDir()}
		if e.IsDir() {
			dirs = append(dirs, it)
		} else {
			if info, err := e.Info(); err == nil {
				it.Size = info.Size()
			}
			files = append(files, it)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, map[string]interface{}{"path": p, "parent": parent, "roots": browseRoots(), "dirs": dirs, "files": files})
}

func browseRoots() []map[string]string {
	var roots []map[string]string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, map[string]string{"name": "Home", "path": home})
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, map[string]string{"name": ".", "path": cwd})
	}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			d := string(c) + ":\\"
			if _, err := os.Stat(d); err == nil {
				roots = append(roots, map[string]string{"name": d, "path": d})
			}
		}
	} else {
		roots = append(roots, map[string]string{"name": "/", "path": "/"})
	}
	return roots
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// openBrowser opens url in the system browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
