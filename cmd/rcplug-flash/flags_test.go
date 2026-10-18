package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestFlagsOnBothSidesOfTheFolder(t *testing.T) {
	for _, args := range [][]string{
		{"--flash", "--yes", "out/"},
		{"out/", "--flash", "--yes"},
		{"--flash", "out/", "--yes"},
	} {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		doFlash := fs.Bool("flash", false, "")
		yes := fs.Bool("yes", false, "")
		rest := parseFlags(fs, args)
		if !*doFlash || !*yes || !reflect.DeepEqual(rest, []string{"out/"}) {
			t.Errorf("%v: flash=%v yes=%v rest=%v", args, *doFlash, *yes, rest)
		}
	}
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if rest := parseFlags(fs, []string{"out/", "--bogus"}); rest != nil {
		t.Errorf("an unknown flag after the folder was accepted: %v", rest)
	}
}
