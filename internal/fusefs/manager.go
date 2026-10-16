package fusefs

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/cgofuse/fuse"

	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

// LibraryPath is where FUSE-T installs the library cgofuse loads.
const LibraryPath = "/usr/local/lib/libfuse-t.dylib"

// Available reports whether FUSE-T is installed.
func Available() bool {
	_, err := os.Stat(LibraryPath)
	return err == nil
}

// FSKitEnabled reports whether FUSE-T's FSKit module is enabled. macOS keeps
// this consent separately from pluginkit, and only the toggle in System
// Settings → General → Login Items & Extensions → File System Extensions sets
// it, so we ask mount(8), which refuses disabled modules up front.
func FSKitEnabled() bool {
	dir, err := os.MkdirTemp("", "avocado-probe")
	if err != nil {
		return false
	}
	defer os.Remove(dir)
	out, _ := exec.Command("/sbin/mount", "-F", "-t", "fuset", "/nonexistent/session.json", dir).CombinedOutput()
	return !strings.Contains(string(out), "is disabled")
}

// Manager mounts every storage of the phone as its own volume.
type Manager struct {
	Root    string   // directory that holds the mount points (e.g. ~/AvocadoMTP)
	FSKit   bool     // use FUSE-T's FSKit backend (falls back to NFS on failure)
	Options []string // extra FUSE options
	TmpDir  string   // staging area for files being written
	Log     *log.Logger
	Debug   *log.Logger // logs every file system call when set

	mu          sync.Mutex
	mounts      map[uint32]*mounted
	fskitFailed bool
}

type mounted struct {
	name string
	dir  string
	host *fuse.FileSystemHost
	done chan struct{}
}

func (m *Manager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log.Printf(format, args...)
	}
}

// MountAll mounts the storages of the connected phone that aren't mounted yet.
func (m *Manager) MountAll(ctx context.Context, r remote.Remote) error {
	v := vfs.New(r)
	v.DeviceName = r.Device().Model
	storages, err := v.Storages(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mounts == nil {
		m.mounts = map[uint32]*mounted{}
	}
	for _, st := range storages {
		if _, ok := m.mounts[st.ID]; ok {
			continue
		}
		mt, err := m.mount(r, st)
		if err != nil {
			m.logf("mount %s: %v", st.Name, err)
			continue
		}
		m.mounts[st.ID] = mt
	}
	return nil
}

// mount tries FUSE-T's FSKit backend first (a real local volume that Finder
// lists as a disk) and falls back to its NFS backend. Mount points live in
// Root: /Volumes would need root to create the folder.
func (m *Manager) mount(r remote.Remote, st *vfs.Storage) (*mounted, error) {
	if m.FSKit && !m.fskitFailed {
		mt, err := m.mountAt(r, st, m.Root, append([]string{"backend=fskit"}, m.Options...))
		if err == nil {
			return mt, nil
		}
		m.fskitFailed = true
		m.logf("FSKit mount failed (%v); using %s instead", err, m.Root)
	}
	return m.mountAt(r, st, m.Root, m.Options)
}

func (m *Manager) mountAt(r remote.Remote, st *vfs.Storage, root string, options []string) (*mounted, error) {
	dir := filepath.Join(root, st.Name)
	local := !strings.HasPrefix(dir, "/Volumes/")
	if local {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(m.TmpDir, 0o700); err != nil {
		return nil, err
	}
	fs := New(r, st.Name, st.ID, m.TmpDir)
	fs.Debug = m.Debug
	host := fuse.NewFileSystemHost(fs)
	host.SetCapCaseInsensitive(false)
	opts := []string{"-o", "volname=" + st.Name}
	for _, o := range options {
		opts = append(opts, "-o", o)
	}
	mt := &mounted{name: st.Name, dir: dir, host: host, done: make(chan struct{})}
	go func() {
		host.Mount(dir, opts)
		// Remove the empty mount point so nothing writes to the Mac's disk by mistake.
		if local {
			_ = os.Remove(dir)
			_ = os.Remove(root)
		}
		close(mt.done)
		m.mu.Lock()
		if m.mounts[st.ID] == mt {
			delete(m.mounts, st.ID)
		}
		m.mu.Unlock()
		m.logf("unmounted %s", dir)
	}()

	// FUSE initializes before the system mount happens, so wait until the
	// directory really belongs to another file system.
	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-mt.done:
			return nil, fmt.Errorf("FUSE-T could not mount %s", dir)
		case <-deadline:
			host.Unmount()
			<-mt.done
			return nil, fmt.Errorf("timed out mounting %s", dir)
		case <-tick.C:
			if isMountPoint(dir) {
				m.logf("mounted %s at %s", st.Name, dir)
				return mt, nil
			}
		}
	}
}

// isMountPoint reports whether dir is on a different file system than its parent.
func isMountPoint(dir string) bool {
	var a, b syscall.Statfs_t
	if syscall.Statfs(dir, &a) != nil || syscall.Statfs(filepath.Dir(dir), &b) != nil {
		return false
	}
	return a.Fsid != b.Fsid
}

// UnmountAll unmounts every volume and waits for the mounts to end.
func (m *Manager) UnmountAll() {
	m.mu.Lock()
	var all []*mounted
	for _, mt := range m.mounts {
		all = append(all, mt)
	}
	m.mounts = nil
	m.mu.Unlock()
	for _, mt := range all {
		if !mt.host.Unmount() {
			_ = exec.Command("umount", mt.dir).Run()
		}
		select {
		case <-mt.done:
		case <-time.After(10 * time.Second):
			_ = exec.Command("diskutil", "unmount", "force", mt.dir).Run()
		}
	}
}

// Mounted lists the current mount points.
func (m *Manager) Mounted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, mt := range m.mounts {
		out = append(out, mt.dir)
	}
	return out
}
