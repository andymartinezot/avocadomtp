package fusefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

// A storage name that escapes the mount root must never reach MkdirAll or
// the mount (vfs already renames such storages; this is the second line).
func TestMountRefusesNamesOutsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mounts")
	m := &Manager{Root: root, TmpDir: t.TempDir()}
	for _, name := range []string{"..", ".", "a/b", "../escape", ""} {
		if _, err := m.mountAt(nil, &vfs.Storage{ID: 1, Name: name}, root, nil); err == nil {
			t.Errorf("mountAt(%q) should refuse", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape")); !os.IsNotExist(err) {
		t.Errorf("a directory was created outside the root: %v", err)
	}
}
