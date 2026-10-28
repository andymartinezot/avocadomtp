package volnames

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "names.json")
	t.Setenv("AVOCADOMTP_VOLUME_NAMES", p)
	a, b := Key("A1", "Phone"), Key("", "Phone")
	if a == b {
		t.Fatal("serial and model keys must differ")
	}
	if got := Load(a); len(got) != 0 {
		t.Fatalf("no file: %v", got)
	}
	if err := Set(a, 0x10001, "Games"); err != nil {
		t.Fatal(err)
	}
	if err := Set(b, 1, "Other"); err != nil {
		t.Fatal(err)
	}
	if got := Load(a); got[0x10001] != "Games" || len(got) != 1 {
		t.Fatalf("Load(a) = %v", got)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode: %v %v", fi, err)
	}
	if err := Set(a, 0x10001, ""); err != nil {
		t.Fatal(err)
	}
	if got := Load(a); len(got) != 0 {
		t.Fatalf("after reset: %v", got)
	}
	if err := Set(b, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("an empty store must remove the file: %v", err)
	}
}

func TestCorruptFileMeansNoNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "names.json")
	t.Setenv("AVOCADOMTP_VOLUME_NAMES", p)
	os.WriteFile(p, []byte("{not json"), 0o600)
	if got := Load(Key("A", "")); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if err := Set(Key("A", ""), 1, "x"); err == nil {
		t.Fatal("Set must not overwrite a file it can't read")
	}
}
