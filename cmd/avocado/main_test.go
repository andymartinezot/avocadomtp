package main

import (
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
