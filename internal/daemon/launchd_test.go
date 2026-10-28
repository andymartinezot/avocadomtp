package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/volnames"
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

func TestRemoveDirIfOurs(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "junk")
	if err := os.Mkdir(junk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, ".DS_Store"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeDirIfOurs(junk); err != nil {
		t.Fatalf("folder with only .DS_Store: %v", err)
	}
	if _, err := os.Lstat(junk); !os.IsNotExist(err) {
		t.Error("folder with only .DS_Store should be removed")
	}

	kept := filepath.Join(dir, "kept")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".DS_Store", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(kept, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeDirIfOurs(kept); err == nil {
		t.Error("a folder with unknown files should be refused")
	}
	for _, name := range []string{".DS_Store", "notes.txt"} {
		if _, err := os.Lstat(filepath.Join(kept, name)); err != nil {
			t.Errorf("%s should be kept: %v", name, err)
		}
	}
}

func TestNoHomeRefusesToTouchFiles(t *testing.T) {
	for _, h := range []string{"", "relative/home"} {
		t.Setenv("HOME", h)
		if err := Uninstall(); err == nil {
			t.Errorf("HOME=%q: Uninstall should refuse", h)
		}
		if _, err := Install("/nonexistent", false); err == nil {
			t.Errorf("HOME=%q: Install should refuse", h)
		}
	}
}

func TestStaleAutostartMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if staleMarker() {
		t.Fatal("no marker is not stale")
	}
	m := autostartMarker()
	if err := os.MkdirAll(filepath.Dir(m), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if staleMarker() {
		t.Error("a fresh marker belongs to a Start in progress")
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(m, old, old); err != nil {
		t.Fatal(err)
	}
	if !staleMarker() {
		t.Error("an old marker is left by an interrupted Start")
	}
}

func TestUninstallRemovesVolumeNames(t *testing.T) {
	t.Setenv("AVOCADOMTP_VOLUME_NAMES", "")
	found := false
	for _, p := range serviceFiles() {
		found = found || p == volnames.Path()
	}
	if !found {
		t.Fatalf("Uninstall must remove %s", volnames.Path())
	}
}
