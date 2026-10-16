// Package fusefs exposes one phone storage as a FUSE file system (mounted
// with FUSE-T), so Finder can browse and copy files with drag and drop.
//
// MTP cannot modify files in place through a plain file API, so files opened
// for writing are staged in a local temporary file and uploaded when they are
// flushed or closed. macOS metadata files (._*, .DS_Store, Spotlight and Trash
// folders) are kept in memory and never written to the phone.
package fusefs

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/cgofuse/fuse"
	"golang.org/x/text/unicode/norm"

	"github.com/andymartinezot/avocadomtp/internal/bufpool"
	"github.com/andymartinezot/avocadomtp/internal/errcode"
	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

const (
	// Read-ahead starts small (thumbnails and metadata readers only touch a
	// file's first few KiB) and doubles while reads stay sequential.
	minReadAhead = 128 << 10
	maxReadAhead = 8 << 20
	opTimeout    = 10 * time.Minute // generous: a commit may upload gigabytes
	statfsTTL    = 5 * time.Second
)

// FS is one storage of the phone as a FUSE file system.
type FS struct {
	fuse.FileSystemBase

	r         remote.Remote
	vfs       *vfs.FS
	storage   string // storage name, the first path element in vfs
	storageID uint32
	tmpDir    string
	uid, gid  uint32

	mu      sync.Mutex
	handles map[uint64]*handle
	next    uint64
	shadow  *shadow

	statMu   sync.Mutex
	statAt   time.Time
	capacity uint64
	free     uint64

	// Debug, if set, logs file system calls.
	Debug *log.Logger
}

func (f *FS) debugf(format string, args ...any) {
	if f.Debug != nil {
		f.Debug.Printf("[%s] "+format, append([]any{f.storage}, args...)...)
	}
}

// New returns the file system for one storage. tmpDir holds files being written.
func New(r remote.Remote, storageName string, storageID uint32, tmpDir string) *FS {
	v := vfs.New(r)
	v.TTL = 3 * time.Second         // pick up changes made on the phone
	v.DeviceName = r.Device().Model // same storage names as the manager
	return &FS{
		r: r, vfs: v, storage: storageName, storageID: storageID, tmpDir: tmpDir,
		uid: uint32(os.Getuid()), gid: uint32(os.Getgid()),
		handles: map[uint64]*handle{}, next: 1, shadow: newShadow(),
	}
}

// handle is an open file or directory.
type handle struct {
	mu   sync.Mutex
	path string

	// reading from the phone
	objHandle uint32
	size      uint64
	block     []byte // pooled read-ahead buffer
	blockOff  int64
	window    int   // current read-ahead size
	next      int64 // offset a sequential reader would ask for next

	// writing: staged locally, uploaded on flush/release
	tmp   *os.File
	dirty bool
}

func errno(err error) int {
	if err == nil {
		return 0
	}
	return -int(errcode.Errno(err))
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

// remotePath maps a mount-relative path to the vfs path.
func (f *FS) remotePath(p string) string { return vfs.Clean("/" + f.storage + "/" + p) }

func (f *FS) fill(st *fuse.Stat_t, isDir bool, size uint64, mtime time.Time) {
	*st = fuse.Stat_t{}
	if isDir {
		st.Mode = fuse.S_IFDIR | 0o755
		st.Nlink = 2
	} else {
		st.Mode = fuse.S_IFREG | 0o644
		st.Nlink = 1
		st.Size = int64(size)
		st.Blocks = (int64(size) + 511) / 512
	}
	st.Blksize = 1 << 20
	st.Uid, st.Gid = f.uid, f.gid
	if mtime.IsZero() {
		mtime = time.Unix(0, 0)
	}
	ts := fuse.NewTimespec(mtime)
	st.Mtim, st.Ctim, st.Atim, st.Birthtim = ts, ts, ts, ts
}

func (f *FS) addHandle(h *handle) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	fh := f.next
	f.next++
	f.handles[fh] = h
	return fh
}

func (f *FS) getHandle(fh uint64) *handle {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handles[fh]
}

// openWriter returns an open write handle for path, if any (for getattr sizes).
func (f *FS) openWriter(p string) *handle {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.handles {
		if h.tmp != nil && h.path == p {
			return h
		}
	}
	return nil
}

// ---- metadata ----

func (f *FS) Statfs(p string, st *fuse.Statfs_t) int {
	f.statMu.Lock()
	defer f.statMu.Unlock()
	if time.Since(f.statAt) > statfsTTL {
		c, cancel := ctx()
		defer cancel()
		if info, err := f.r.StorageInfo(c, f.storageID); err == nil {
			f.capacity, f.free, f.statAt = info.MaxCapacity, info.FreeSpaceInBytes, time.Now()
		}
	}
	const bs = 4096
	*st = fuse.Statfs_t{Bsize: bs, Frsize: bs, Blocks: f.capacity / bs, Bfree: f.free / bs, Bavail: f.free / bs,
		Files: 1 << 20, Ffree: 1 << 20, Namemax: 255}
	return 0
}

func (f *FS) Getattr(p string, st *fuse.Stat_t, fh uint64) int {
	p = nfc(p)
	if p == "/" {
		f.fill(st, true, 0, time.Time{})
		return 0
	}
	if isShadow(p) {
		return f.shadow.getattr(f, p, st)
	}
	if h := f.openWriter(p); h != nil {
		h.mu.Lock()
		fi, err := h.tmp.Stat()
		h.mu.Unlock()
		if err == nil {
			f.fill(st, false, uint64(fi.Size()), fi.ModTime())
			return 0
		}
	}
	c, cancel := ctx()
	defer cancel()
	e, err := f.vfs.Stat(c, f.remotePath(p))
	if err != nil {
		return errno(err)
	}
	f.fill(st, e.IsDir, e.Size, e.Modified)
	return 0
}

func (f *FS) Access(p string, mask uint32) int { return 0 }

// Times, modes and owners cannot be set over MTP; pretend success so copies don't fail.
func (f *FS) Chmod(p string, mode uint32) int            { return 0 }
func (f *FS) Chown(p string, uid, gid uint32) int        { return 0 }
func (f *FS) Utimens(p string, tmsp []fuse.Timespec) int { return 0 }
func (f *FS) Setchgtime(p string, tmsp fuse.Timespec) int {
	return 0
}
func (f *FS) Setcrtime(p string, tmsp fuse.Timespec) int { return 0 }
func (f *FS) Chflags(p string, flags uint32) int         { return 0 }

func (f *FS) Getxattr(p, name string) (int, []byte) { return -fuse.ENOATTR, nil }
func (f *FS) Setxattr(p, name string, value []byte, flags int) int {
	return -fuse.ENOTSUP
}
func (f *FS) Removexattr(p, name string) int { return -fuse.ENOATTR }
func (f *FS) Listxattr(p string, fill func(name string) bool) int {
	return 0
}

// ---- directories ----

func (f *FS) Opendir(p string) (int, uint64) { return 0, 0 }

func (f *FS) Readdir(p string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.readdir(f, p, fill)
	}
	c, cancel := ctx()
	defer cancel()
	entries, err := f.vfs.ReadDir(c, f.remotePath(p))
	if err != nil {
		return errno(err)
	}
	var st fuse.Stat_t
	f.fill(&st, true, 0, time.Time{})
	fill(".", &st, 0)
	fill("..", &st, 0)
	for _, e := range entries {
		f.fill(&st, e.IsDir, e.Size, e.Modified)
		if !fill(e.Name, &st, 0) {
			break
		}
	}
	// Metadata files stay hidden: macOS looks them up by name and removes a
	// file's ._ companion itself, so listing them would only confuse rm -r.
	return 0
}

func (f *FS) Mkdir(p string, mode uint32) int {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.mkdir(p)
	}
	c, cancel := ctx()
	defer cancel()
	parent, err := f.vfs.Stat(c, path.Dir(f.remotePath(p)))
	if err != nil {
		return errno(err)
	}
	if _, err := f.vfs.Stat(c, f.remotePath(p)); err == nil {
		return -fuse.EEXIST
	}
	_, err = f.r.Mkdir(c, parent.StorageID, parent.Handle, path.Base(p))
	f.vfs.Invalidate(parent.StorageID, parent.Handle)
	return errno(err)
}

func (f *FS) Rmdir(p string) int {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.rmdir(p)
	}
	c, cancel := ctx()
	defer cancel()
	e, err := f.vfs.Stat(c, f.remotePath(p))
	if err != nil {
		return errno(err)
	}
	if !e.IsDir {
		return -fuse.ENOTDIR
	}
	children, err := f.vfs.ReadDirEntry(c, e)
	if err != nil {
		return errno(err)
	}
	if len(children) > 0 {
		return -fuse.ENOTEMPTY
	}
	f.shadow.removeTree(p) // Finder's ._ files inside the folder
	return f.deleteEntry(c, e)
}

func (f *FS) Unlink(p string) int {
	p = nfc(p)
	f.debugf("unlink %s", p)
	if isShadow(p) {
		return f.shadow.unlink(p)
	}
	c, cancel := ctx()
	defer cancel()
	e, err := f.vfs.Stat(c, f.remotePath(p))
	if err != nil {
		return errno(err)
	}
	if e.IsDir {
		return -fuse.EISDIR
	}
	return f.deleteEntry(c, e)
}

func (f *FS) deleteEntry(c context.Context, e *vfs.Entry) int {
	err := f.r.Delete(c, e.Handle)
	f.invalidateParent(c, e.Path)
	return errno(err)
}

func (f *FS) invalidateParent(c context.Context, remote string) {
	if par, err := f.vfs.Stat(c, path.Dir(remote)); err == nil {
		f.vfs.Invalidate(par.StorageID, par.Handle)
	}
}

func (f *FS) Rename(oldpath, newpath string) int {
	oldpath, newpath = nfc(oldpath), nfc(newpath)
	f.debugf("rename %s -> %s", oldpath, newpath)
	switch {
	case isShadow(oldpath) && isShadow(newpath):
		return f.shadow.rename(oldpath, newpath)
	case isShadow(newpath):
		return f.moveToShadow(oldpath, newpath)
	case isShadow(oldpath):
		return f.moveFromShadow(oldpath, newpath)
	}
	c, cancel := ctx()
	defer cancel()
	src, err := f.vfs.Stat(c, f.remotePath(oldpath))
	if err != nil {
		return errno(err)
	}
	srcDir, err := f.vfs.Stat(c, path.Dir(f.remotePath(oldpath)))
	if err != nil {
		return errno(err)
	}
	dstDir, err := f.vfs.Stat(c, path.Dir(f.remotePath(newpath)))
	if err != nil {
		return errno(err)
	}
	newName := path.Base(newpath)
	if dst, err := f.vfs.Stat(c, f.remotePath(newpath)); err == nil && dst.Handle != src.Handle {
		// rename(2) replaces the destination.
		if dst.IsDir {
			if !src.IsDir {
				return -fuse.EISDIR
			}
			if kids, err := f.vfs.ReadDirEntry(c, dst); err != nil || len(kids) > 0 {
				return -fuse.ENOTEMPTY
			}
		} else if src.IsDir {
			return -fuse.ENOTDIR
		}
		if err := f.r.Delete(c, dst.Handle); err != nil {
			return errno(err)
		}
	}
	defer f.vfs.Invalidate(srcDir.StorageID, srcDir.Handle)
	defer f.vfs.Invalidate(dstDir.StorageID, dstDir.Handle)
	if srcDir.Handle != dstDir.Handle {
		if err := f.r.Move(c, src.Handle, dstDir.StorageID, dstDir.Handle); err != nil {
			return errno(err)
		}
	}
	if newName != src.Name {
		if err := f.r.Rename(c, src.Handle, newName); err != nil {
			return errno(err)
		}
	}
	f.shadow.rename(oldpath, newpath) // carry Finder metadata along, if any
	return 0
}

// moveToShadow handles tools that write a temporary file and rename it to a
// metadata name (ditto writing .DS_Store): the content moves off the phone.
func (f *FS) moveToShadow(oldpath, newpath string) int {
	c, cancel := ctx()
	defer cancel()
	e, err := f.vfs.Stat(c, f.remotePath(oldpath))
	if err != nil {
		return errno(err)
	}
	if e.IsDir || e.Size > maxShadowFile {
		return -fuse.EXDEV
	}
	var buf bytesBuffer
	if err := f.r.Download(c, e.Handle, &buf); err != nil {
		return errno(err)
	}
	if rc := f.shadow.put(newpath, buf); rc != 0 {
		return rc
	}
	return f.deleteEntry(c, e)
}

// moveFromShadow uploads a metadata-store file that is renamed to a normal name.
func (f *FS) moveFromShadow(oldpath, newpath string) int {
	data, ok := f.shadow.get(oldpath)
	if !ok {
		return -fuse.ENOENT
	}
	rc, fh := f.Create(newpath, fuse.O_WRONLY|fuse.O_CREAT, 0o644)
	if rc != 0 {
		return rc
	}
	if n := f.Write(newpath, data, 0, fh); n < 0 {
		f.Release(newpath, fh)
		return n
	}
	if rc := f.Release(newpath, fh); rc != 0 {
		return rc
	}
	return f.shadow.unlink(oldpath)
}

type bytesBuffer []byte

func (b *bytesBuffer) Write(p []byte) (int, error) {
	if len(*b)+len(p) > maxShadowFile {
		return 0, syscall.EFBIG
	}
	*b = append(*b, p...)
	return len(p), nil
}

func nfc(p string) string { return norm.NFC.String(p) }

// ---- files ----

func (f *FS) Create(p string, flags int, mode uint32) (int, uint64) {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.create(p), 0
	}
	f.debugf("create %s flags=%#x", p, flags)
	tmp, err := os.CreateTemp(f.tmpDir, "w-*")
	if err != nil {
		return errno(err), ^uint64(0)
	}
	os.Remove(tmp.Name()) // unlinked: vanishes when closed
	// dirty so that an empty new file is created on the phone too
	return 0, f.addHandle(&handle{path: p, tmp: tmp, dirty: true})
}

func (f *FS) Open(p string, flags int) (int, uint64) {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.open(p), 0
	}
	f.debugf("open %s flags=%#x", p, flags)
	c, cancel := ctx()
	defer cancel()
	e, err := f.vfs.Stat(c, f.remotePath(p))
	if err != nil {
		return errno(err), ^uint64(0)
	}
	if e.IsDir {
		return -fuse.EISDIR, ^uint64(0)
	}
	h := &handle{path: p, objHandle: e.Handle, size: e.Size}
	if flags&fuse.O_TRUNC != 0 && flags&(fuse.O_WRONLY|fuse.O_RDWR) != 0 {
		if rc := f.stage(h, false); rc != 0 {
			return rc, ^uint64(0)
		}
		h.dirty = true
	}
	// Files opened for writing are staged lazily, on the first write: FUSE-T's
	// FSKit backend opens everything read-write, even for plain reads.
	return 0, f.addHandle(h)
}

// stage copies the file into a local temporary file so it can be modified
// (download=false starts empty, for truncation). The caller holds h.mu or
// owns h exclusively.
func (f *FS) stage(h *handle, download bool) int {
	if h.tmp != nil {
		return 0
	}
	tmp, err := os.CreateTemp(f.tmpDir, "w-*")
	if err != nil {
		return errno(err)
	}
	os.Remove(tmp.Name()) // unlinked: vanishes when closed
	if download && h.size > 0 {
		c, cancel := ctx()
		defer cancel()
		if err := f.r.Download(c, h.objHandle, tmp); err != nil {
			tmp.Close()
			return errno(err)
		}
	}
	h.tmp = tmp
	bufpool.Put(h.block) // reads now come from the staged copy
	h.block = nil
	return 0
}

func (f *FS) Read(p string, buff []byte, ofst int64, fh uint64) int {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.read(p, buff, ofst)
	}
	h := f.getHandle(fh)
	if h == nil {
		return -fuse.EBADF
	}
	f.debugf("read %s ofst=%d len=%d", p, ofst, len(buff))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tmp != nil {
		n, err := h.tmp.ReadAt(buff, ofst)
		if err != nil && !errors.Is(err, io.EOF) {
			return errno(err)
		}
		return n
	}
	size := int64(h.size)
	if ofst >= size {
		return 0
	}
	end := min(ofst+int64(len(buff)), size)
	if ofst < h.blockOff || end > h.blockOff+int64(len(h.block)) {
		if ofst == h.next && h.window > 0 {
			h.window = min(h.window*2, maxReadAhead)
		} else {
			h.window = minReadAhead
		}
		want := int(min(max(end-ofst, int64(h.window)), size-ofst))
		if cap(h.block) < want {
			bufpool.Put(h.block)
			h.block = bufpool.Get(want)
		}
		c, cancel := ctx()
		n, err := f.r.ReadAt(c, h.objHandle, h.block[:want], ofst)
		cancel()
		if err != nil && !errors.Is(err, io.EOF) {
			h.block = h.block[:0]
			return errno(err)
		}
		h.block, h.blockOff = h.block[:n], ofst
	}
	n := copy(buff, h.block[ofst-h.blockOff:])
	h.next = ofst + int64(n)
	return n
}

func (f *FS) Write(p string, buff []byte, ofst int64, fh uint64) int {
	p = nfc(p)
	if isShadow(p) {
		return f.shadow.write(p, buff, ofst)
	}
	h := f.getHandle(fh)
	if h == nil {
		return -fuse.EBADF
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if rc := f.stage(h, true); rc != 0 {
		return rc
	}
	n, err := h.tmp.WriteAt(buff, ofst)
	if err != nil {
		return errno(err)
	}
	h.dirty = true
	return n
}

func (f *FS) Truncate(p string, size int64, fh uint64) int {
	p = nfc(p)
	f.debugf("truncate %s size=%d fh=%d", p, size, fh)
	if isShadow(p) {
		return f.shadow.truncate(p, size)
	}
	if h := f.getHandle(fh); h != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if rc := f.stage(h, size > 0); rc != 0 {
			return rc
		}
		if err := h.tmp.Truncate(size); err != nil {
			return errno(err)
		}
		h.dirty = true
		return 0
	}
	if h := f.openWriter(p); h != nil {
		return f.Truncate(p, size, f.handleID(h))
	}
	// Truncating a closed file: stage it, truncate, upload.
	rc, fh2 := f.Open(p, fuse.O_RDWR)
	if rc != 0 {
		return rc
	}
	defer f.Release(p, fh2)
	return f.Truncate(p, size, fh2)
}

func (f *FS) handleID(h *handle) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, x := range f.handles {
		if x == h {
			return id
		}
	}
	return ^uint64(0)
}

// Flush and Fsync don't upload: the NFS layer syncs repeatedly while a file
// is still being written, and every upload sends the whole file. Data is
// safe in the staging file until Release.
func (f *FS) Flush(p string, fh uint64) int {
	f.debugf("flush %s fh=%d", p, fh)
	return 0
}

func (f *FS) Fsync(p string, datasync bool, fh uint64) int {
	f.debugf("fsync %s fh=%d", p, fh)
	return 0
}

func (f *FS) Release(p string, fh uint64) int {
	p = nfc(p)
	f.debugf("release %s fh=%d", p, fh)
	rc := f.commit(fh)
	f.mu.Lock()
	h := f.handles[fh]
	delete(f.handles, fh)
	f.mu.Unlock()
	if h != nil {
		if h.tmp != nil {
			h.tmp.Close()
		}
		h.mu.Lock()
		bufpool.Put(h.block)
		h.block = nil
		h.mu.Unlock()
	}
	return rc
}

// commit uploads a dirty staged file, replacing the version on the phone.
func (f *FS) commit(fh uint64) int {
	h := f.getHandle(fh)
	if h == nil || h.tmp == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return 0
	}
	f.debugf("commit %s: uploading", h.path)
	start := time.Now()
	defer func() { f.debugf("commit %s: done in %s", h.path, time.Since(start).Round(time.Millisecond)) }()
	c, cancel := ctx()
	defer cancel()
	remote := f.remotePath(h.path)
	parent, err := f.vfs.Stat(c, path.Dir(remote))
	if err != nil {
		return errno(err)
	}
	fi, err := h.tmp.Stat()
	if err != nil {
		return errno(err)
	}
	name := path.Base(remote)
	old, oldErr := f.vfs.Stat(c, remote)
	if oldErr == nil && old.IsDir {
		return -fuse.EISDIR
	}

	// MTP cannot overwrite: upload under a temporary name, then swap, so a
	// failed upload never destroys the existing file.
	uploadName := name
	if oldErr == nil {
		uploadName = ".avocado-" + name
	}
	if _, err := h.tmp.Seek(0, io.SeekStart); err != nil {
		return errno(err)
	}
	newHandle, err := f.r.Upload(c, parent.StorageID, parent.Handle, uploadName, fi.Size(), fi.ModTime(), h.tmp)
	f.vfs.Invalidate(parent.StorageID, parent.Handle)
	if err != nil {
		return errno(err)
	}
	if oldErr == nil {
		if err := f.r.Delete(c, old.Handle); err != nil {
			return errno(err)
		}
		if err := f.r.Rename(c, newHandle, name); err != nil {
			return errno(err)
		}
		f.vfs.Invalidate(parent.StorageID, parent.Handle)
	}
	h.dirty = false
	return 0
}

// isShadow reports whether a path is macOS metadata that stays on the Mac.
func isShadow(p string) bool {
	for _, el := range strings.Split(p, "/") {
		switch {
		case strings.HasPrefix(el, "._"),
			el == ".DS_Store", el == ".Spotlight-V100", el == ".Trashes", el == ".fseventsd",
			el == ".TemporaryItems", el == ".metadata_never_index", el == ".VolumeIcon.icns":
			return true
		}
	}
	return false
}
