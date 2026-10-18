package main

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The embedded GUI must be present and its data-t keys must all exist in both
// language dictionaries, so no label shows up blank.
func TestEmbeddedGUIKeysResolve(t *testing.T) {
	b, err := fs.ReadFile(webFS, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	if !strings.Contains(html, "rcplug setup") || len(html) < 2000 {
		t.Fatalf("index.html looks wrong (%d bytes)", len(html))
	}
	// Every data-t="key" must be defined in both T.en and T.de.
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-t="([a-zA-Z]+)"`).FindAllStringSubmatch(html, -1) {
		keys[m[1]] = true
	}
	if len(keys) < 10 {
		t.Fatalf("found only %d data-t keys", len(keys))
	}
	// Each data-t key must appear twice as a dictionary key (en and de).
	for key := range keys {
		n := len(regexp.MustCompile(`\b`+key+`:`).FindAllString(html, -1))
		if n < 2 {
			t.Errorf("data-t key %q is defined %d time(s), want it in both en and de", key, n)
		}
	}
}
