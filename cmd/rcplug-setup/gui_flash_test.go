package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/flash"
)

func call(t *testing.T, h http.HandlerFunc, method, target, body string) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: %v (%q)", method, target, err, rec.Body.String())
	}
	return out
}

// fakeFlash replaces flash.Run for one test; it records the options and
// waits for release before it returns.
func fakeFlash(t *testing.T) (got chan flash.Options, release chan error) {
	got, release = make(chan flash.Options, 1), make(chan error)
	old := runFlash
	runFlash = func(ctx context.Context, o flash.Options) error {
		fmt.Fprintln(o.Stdout, "device: /dev/hidraw-fake")
		got <- o
		return <-release
	}
	t.Cleanup(func() { runFlash = old })
	return got, release
}

func waitState(t *testing.T, g *gui, want string) map[string]interface{} {
	t.Helper()
	for i := 0; i < 200; i++ {
		p := call(t, g.handleFlashProgress, "GET", "/api/flash/progress", "")
		if p["state"] == want {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("flash job never reached %q", want)
	return nil
}

func newTestGUI() *gui {
	g := &gui{version: "test", quit: make(chan struct{})}
	g.idle = time.AfterFunc(time.Hour, func() {})
	return g
}

func TestFlashNeedsTheConfirmationWord(t *testing.T) {
	got, _ := fakeFlash(t)
	g := newTestGUI()
	for _, body := range []string{`{"dir":"/x","flash":true}`, `{"dir":"/x","flash":true,"confirm":"yes"}`, `{"dir":"/x","flash":true,"confirm":"Flash"}`} {
		out := call(t, g.handleFlashStart, "POST", "/api/flash/start", body)
		if out["error"] == nil {
			t.Fatalf("%s: started without the confirmation word", body)
		}
	}
	select {
	case o := <-got:
		t.Fatalf("flash ran: %+v", o)
	default:
	}
}

func TestFlashJob(t *testing.T) {
	if flash.Supported() != nil {
		t.Skip("flashing is not supported on " + runtime.GOOS)
	}
	got, release := fakeFlash(t)
	g := newTestGUI()

	if out := call(t, g.handleFlashStart, "POST", "/api/flash/start", `{"dir":"/stage","flash":true,"confirm":"flash"}`); out["ok"] != true {
		t.Fatalf("start: %v", out)
	}
	o := <-got
	if o.StageDir != "/stage" || !o.Flash || !o.NoPrompt {
		t.Fatalf("options = %+v", o)
	}

	// While it runs: no second run, and no quitting.
	if out := call(t, g.handleFlashStart, "POST", "/api/flash/start", `{"dir":"/stage"}`); out["error"] == nil {
		t.Fatal("a second run started while one was running")
	}
	if out := call(t, g.handleQuit, "POST", "/api/quit", ""); out["error"] == nil {
		t.Fatal("quit was accepted during a flash")
	}
	select {
	case <-g.quit:
		t.Fatal("the GUI stopped during a flash")
	default:
	}
	p := waitState(t, g, "running")
	if p["flash"] != true || !strings.Contains(p["log"].(string), "/dev/hidraw-fake") {
		t.Fatalf("progress = %v", p)
	}

	release <- fmt.Errorf("flash script failed: exit status 1")
	p = waitState(t, g, "error")
	if p["failure"] != "flash script failed: exit status 1" {
		t.Fatalf("progress = %v", p)
	}

	// Staging only needs no confirmation; afterwards quitting works again.
	if out := call(t, g.handleFlashStart, "POST", "/api/flash/start", `{"dir":"/stage"}`); out["ok"] != true {
		t.Fatalf("start: %v", out)
	}
	if o = <-got; o.Flash {
		t.Fatalf("a stage-only run asked to flash: %+v", o)
	}
	release <- nil
	waitState(t, g, "done")
	if out := call(t, g.handleQuit, "POST", "/api/quit", ""); out["ok"] != true {
		t.Fatalf("quit: %v", out)
	}
	<-g.quit
}

// The check only reads: with an empty folder it reports the missing archive.
func TestFlashCheckReportsProblems(t *testing.T) {
	g := newTestGUI()
	out := call(t, g.handleFlashCheck, "GET", "/api/flash/check?dir="+t.TempDir(), "")
	if out["ready"] != false {
		t.Fatalf("ready with an empty folder: %v", out)
	}
	if !strings.Contains(fmt.Sprint(out["report"]), "missing archive.tar.gz") {
		t.Fatalf("report = %v", out["report"])
	}
}
