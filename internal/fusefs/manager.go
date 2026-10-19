package fusefs

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/cgofuse/fuse"

	"github.com/andymartinezot/avocadomtp/internal/activity"
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
	dir, err := os.MkdirTemp("", "avocadomtp-probe")
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
	// Activity, if set, receives the list of mounts and transfer progress.
	Activity *activity.Hub

	mu          sync.Mutex
	mounts      map[uint32]*mounted
	ejected     map[uint32]bool // unmounted by the user; stay off until remount or reconnect
	fskitFailed bool
}

type mounted struct {
	id      uint32
	name    string
	dir     string
	backend string
	host    *fuse.FileSystemHost
	done    chan struct{}
}

// publishLocked sends the current mounts to the activity hub.
func (m *Manager) publishLocked() {
	if m.Activity == nil {
		return
	}
	list := make([]activity.Mount, 0, len(m.mounts))
	for _, mt := range m.mounts {
		list = append(list, activity.Mount{StorageID: mt.id, Name: mt.name, Path: mt.dir, Backend: mt.backend})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	m.Activity.SetMounts(list)
}

// Eject unmounts one storage and keeps it unmounted until Remount or until
// the phone reconnects.
func (m *Manager) Eject(storageID uint32) error {
	m.mu.Lock()
	mt := m.mounts[storageID]
	if m.ejected == nil {
		m.ejected = map[uint32]bool{}
	}
	m.ejected[storageID] = true
	m.mu.Unlock()
	if mt == nil {
		return fmt.Errorf("storage %08X is not mounted", storageID)
	}
	m.unmount(mt)
	return nil
}

// Remount mounts every storage again, including ejected ones.
func (m *Manager) Remount(ctx context.Context, r remote.Remote) error {
	m.mu.Lock()
	m.ejected = nil
	m.mu.Unlock()
	return m.MountAll(ctx, r)
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
		if _, ok := m.mounts[st.ID]; ok || m.ejected[st.ID] {
			continue
		}
		mt, err := m.mount(r, st)
		if err != nil {
			m.logf("mount %s: %v", st.Name, err)
			continue
		}
		m.mounts[st.ID] = mt
	}
	m.publishLocked()
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
	// vfs already refuses unusable names; never mount anywhere but directly
	// inside the root, whatever the device calls its storage.
	if !vfs.ValidName(st.Name) || filepath.Dir(dir) != filepath.Clean(root) {
		return nil, fmt.Errorf("refusing to mount storage %q outside %s", st.Name, root)
	}
	// Never stack two volumes (m.mu is held by the callers). One of ours
	// mounted there is refused; a leftover mount (e.g. after a crash) is
	// cleared, and refused if it won't go.
	for _, mt := range m.mounts {
		if mt.dir == dir {
			return nil, fmt.Errorf("%s is already mounted", dir)
		}
	}
	if isMountPoint(dir) {
		_ = exec.Command("umount", dir).Run()
		if isMountPoint(dir) {
			return nil, fmt.Errorf("%s is already a mount point", dir)
		}
	}
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
	fs.Activity = m.Activity
	host := fuse.NewFileSystemHost(fs)
	host.SetCapCaseInsensitive(false)
	// FUSE splits options on commas: keep the device's name from adding any.
	opts := []string{"-o", "volname=" + strings.ReplaceAll(st.Name, ",", " ")}
	for _, o := range options {
		opts = append(opts, "-o", o)
	}
	backend := "nfs"
	for _, o := range options {
		if o == "backend=fskit" {
			backend = "fskit"
		}
	}
	mt := &mounted{id: st.ID, name: st.Name, dir: dir, backend: backend, host: host, done: make(chan struct{})}
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
			m.publishLocked() // e.g. ejected from Finder
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

// UnmountAll unmounts every volume and waits for the mounts to end. It also
// forgets ejected storages, so they mount again when the phone reconnects.
func (m *Manager) UnmountAll() {
	m.mu.Lock()
	var all []*mounted
	for _, mt := range m.mounts {
		all = append(all, mt)
	}
	m.ejected = nil
	m.mu.Unlock()
	for _, mt := range all {
		m.unmount(mt)
	}
	m.mu.Lock()
	m.mounts = nil
	m.publishLocked()
	m.mu.Unlock()
}

func (m *Manager) unmount(mt *mounted) {
	if !mt.host.Unmount() {
		_ = exec.Command("umount", mt.dir).Run()
	}
	select {
	case <-mt.done:
	case <-time.After(10 * time.Second):
		_ = exec.Command("diskutil", "unmount", "force", mt.dir).Run()
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
