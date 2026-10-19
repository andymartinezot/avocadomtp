package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveCLILink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "avocadomtp")
	other := filepath.Join(dir, "other")
	ours := filepath.Join(dir, "ours")
	theirs := filepath.Join(dir, "theirs")
	plain := filepath.Join(dir, "plain")
	if err := os.Symlink(target, ours); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, theirs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{ours, theirs, plain, filepath.Join(dir, "missing")} {
		if err := removeCLILink(link, target); err != nil {
			t.Fatalf("%s: %v", link, err)
		}
	}
	if _, err := os.Lstat(ours); !os.IsNotExist(err) {
		t.Error("our symlink should be removed")
	}
	for _, keep := range []string{theirs, plain} {
		if _, err := os.Lstat(keep); err != nil {
			t.Errorf("%s should be kept: %v", keep, err)
		}
	}
}
