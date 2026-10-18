package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/flash"
)

// preflight and runFlash check and run a stage/flash; tests replace them, as
// nothing here may reach a real device.
var (
	preflight = flash.Preflight
	runFlash  = flash.Run
)

// flashConfirmWord must come with a flash request, as typed by the user. The
// page asks for it too; the server checks it so no request can flash by
// accident.
const flashConfirmWord = "flash"

// flashJob is a stage or flash run in the background; the page polls it.
// There is no cancel: stopping the scripts half-way (update disk mounted, or
// the device mid-flash) would only make things worse.
type flashJob struct {
	mu      sync.Mutex
	State   string // "", "running", "done", "error"
	Flash   bool   // the run sends the flash command (else it stages only)
	Dir     string
	Started time.Time
	Ended   time.Time
	Error   string
	log     syncBuffer
}

func (j *flashJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State == "running"
}

// syncBuffer collects script output while the page reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

// handleFlashCheck runs the pre-flight checks for a staging folder. It only
// reads files and device information; nothing is sent to the device.
func (g *gui) handleFlashCheck(w http.ResponseWriter, r *http.Request) {
	rep := preflight(flash.Options{StageDir: expandHome(r.URL.Query().Get("dir")), NoPrompt: true})
	resp := map[string]interface{}{"supported": flash.Supported() == nil, "ready": rep.Err() == nil, "report": rep}
	for _, c := range rep.Checks {
		if c.ID == "access" && !c.OK && runtime.GOOS == "linux" {
			resp["udevRule"] = string(flash.UdevRule())
		}
	}
	writeJSON(w, resp)
}

// handleFlashStart stages the archive in a folder onto the device and, with
// flash set and the confirmation word, flashes it. flash.Run repeats every
// pre-flight check first and stops at the first problem.
func (g *gui) handleFlashStart(w http.ResponseWriter, r *http.Request) {
	in, err := decodeReq(r)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	if in.Flash && in.Confirm != flashConfirmWord {
		writeJSON(w, map[string]string{"error": tr("guiFlashConfirm")})
		return
	}
	if err := flash.Supported(); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	j := &g.fl
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State == "running" {
		writeJSON(w, map[string]string{"error": tr("guiFlashBusy")})
		return
	}
	j.State, j.Flash, j.Dir, j.Started, j.Ended, j.Error = "running", in.Flash, in.Dir, time.Now(), time.Time{}, ""
	j.log.Reset()
	what := "stage only, no flash"
	if in.Flash {
		what = "stage and FLASH"
	}
	fmt.Fprintf(&j.log, "rcplug-setup %s: %s, %s\nchecking the archive and the device …\n", g.version, in.Dir, what)
	go func() {
		err := runFlash(context.Background(), flash.Options{StageDir: in.Dir, Flash: in.Flash, NoPrompt: true, Stdout: &j.log, Stderr: &j.log})
		j.mu.Lock()
		defer j.mu.Unlock()
		j.Ended = time.Now()
		if err != nil {
			j.State, j.Error = "error", err.Error()
		} else {
			j.State = "done"
		}
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

func (g *gui) handleFlashProgress(w http.ResponseWriter, r *http.Request) {
	j := &g.fl
	j.mu.Lock()
	defer j.mu.Unlock()
	var secs int
	switch {
	case !j.Ended.IsZero():
		secs = int(j.Ended.Sub(j.Started).Seconds())
	case !j.Started.IsZero():
		secs = int(time.Since(j.Started).Seconds())
	}
	writeJSON(w, map[string]interface{}{"state": j.State, "flash": j.Flash, "dir": j.Dir,
		"seconds": secs, "log": j.log.String(), "failure": j.Error})
}
