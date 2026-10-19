package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		`ls -l`:                        {"ls", "-l"},
		`cd "Internal shared storage"`: {"cd", "Internal shared storage"},
		`push ~/a\ b.txt 'SD card/x'`:  {"push", "~/a b.txt", "SD card/x"},
		`  mv  "a"b  ""  `:             {"mv", "ab", ""},
	}
	for in, want := range cases {
		got, err := splitArgs(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := splitArgs(`cd "oops`); err == nil {
		t.Error("expected unterminated quote error")
	}
}

func TestParseFlags(t *testing.T) {
	f, rest, err := parseFlags([]string{"a", "-rf", "b", "--", "-c"}, "rf")
	if err != nil || !f['r'] || !f['f'] || !reflect.DeepEqual(rest, []string{"a", "b", "-c"}) {
		t.Fatalf("%v %v %v", f, rest, err)
	}
	if _, _, err := parseFlags([]string{"-x"}, "r"); err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func TestResolve(t *testing.T) {
	if got := resolve("/SD card/Music", "../DCIM"); got != "/SD card/DCIM" {
		t.Fatal(got)
	}
	if got := resolve("/SD card", "/Internal shared storage"); got != "/Internal shared storage" {
		t.Fatal(got)
	}
}

func TestVolumesLine(t *testing.T) {
	cases := []struct {
		fuseT, fskit bool
		want         string
	}{
		{false, false, "volumes:   unavailable (FUSE-T not installed)"},
		{true, true, "volumes:   fskit"},
		{true, false, "volumes:   nfs (enable fuse-t in File System Extensions for local disks)"},
	}
	for _, c := range cases {
		if got := volumesLine(c.fuseT, c.fskit); got != c.want {
			t.Errorf("volumesLine(%v, %v) = %q, want %q", c.fuseT, c.fskit, got, c.want)
		}
	}
}

func TestPrintable(t *testing.T) {
	if got := printable("Pixel\x1b]0;owned\x07 8"); got != "Pixel?]0;owned? 8" {
		t.Errorf("printable = %q", got)
	}
	if got := printable("Música"); got != "Música" {
		t.Errorf("printable changed a normal name: %q", got)
	}
}

func TestChildPath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"..", "../x", "a/b", "/abs", "", "x\x00"} {
		if p, err := childPath(dir, name); err == nil {
			t.Errorf("childPath(%q) = %q, want an error", name, p)
		}
	}
	if p, err := childPath(dir, "IMG 1.jpg"); err != nil || p != filepath.Join(dir, "IMG 1.jpg") {
		t.Errorf("childPath(ok) = %q, %v", p, err)
	}
}
