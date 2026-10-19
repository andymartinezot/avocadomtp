// Package vfs maps slash-separated paths ("/SD card/DCIM/Camera") onto MTP
// storages and object handles, caching directory listings.
package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

// Backend is the subset of *mtp.Session used for lookups.
type Backend interface {
	StorageIDs(ctx context.Context) ([]uint32, error)
	StorageInfo(ctx context.Context, id uint32) (*ptp.StorageInfo, error)
	List(ctx context.Context, storage, parent uint32) ([]*mtp.Object, error)
}

// Storage is a top-level volume such as internal memory or an SD card.
type Storage struct {
	ID    uint32
	Name  string
	Alias string // the phone's own description, accepted in paths too
	Info  *ptp.StorageInfo
}

// Entry is a resolved path.
type Entry struct {
	Path      string
	Name      string
	StorageID uint32 // 0 for "/"
	Handle    uint32 // ptp.HandleRoot for a storage root, 0 for "/"
	IsDir     bool
	Size      uint64
	Modified  time.Time
	Storage   *Storage // set for storage roots
}

// IsRoot reports whether e is the virtual "/" directory listing the storages.
func (e *Entry) IsRoot() bool { return e.StorageID == 0 }

type dirKey struct{ storage, handle uint32 }

// FS resolves paths. It is safe for concurrent use.
type FS struct {
	b        Backend
	mu       sync.Mutex
	storages []*Storage
	dirs     map[dirKey]dirEntry

	// TTL is how long a directory listing stays cached (0 = until invalidated).
	TTL time.Duration
	// DeviceName, if set, replaces generic internal-storage descriptions
	// ("Internal shared storage") so the volume is named after the phone.
	DeviceName string
}

type dirEntry struct {
	objs   []*mtp.Object
	hidden int // objects left out because of their names (see ValidName)
	at     time.Time
}

func New(b Backend) *FS { return &FS{b: b, dirs: map[dirKey]dirEntry{}} }

// Storages returns the device storages, named after their description.
func (f *FS) Storages(ctx context.Context) ([]*Storage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.storagesLocked(ctx)
}

func (f *FS) storagesLocked(ctx context.Context) ([]*Storage, error) {
	if f.storages != nil {
		return f.storages, nil
	}
	ids, err := f.b.StorageIDs(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{} // case- and normalization-insensitive, like APFS
	var out []*Storage
	for _, id := range ids {
		info, err := f.b.StorageInfo(ctx, id)
		if err != nil {
			return nil, err
		}
		name := storageName(info.Description)
		if name == "" {
			name = storageName(info.VolumeLabel)
		}
		if name == "" {
			name = fmt.Sprintf("Storage %08X", id)
		}
		alias := name
		if dev := storageName(f.DeviceName); dev != "" && isGenericInternal(name) {
			name = dev
		}
		key := func(s string) string { return cases.Fold().String(norm.NFC.String(s)) }
		for base, n := name, 2; seen[key(name)]; n++ {
			name = fmt.Sprintf("%s (%d)", base, n)
		}
		seen[key(name)] = true
		out = append(out, &Storage{ID: id, Name: name, Alias: alias, Info: info})
	}
	f.storages = out
	return out, nil
}

// ValidName reports whether a name that came from the device can be used as
// one path component on the Mac: not empty, not "." or "..", valid UTF-8, no
// "/" and no unsafe characters (see unsafeRune). Devices are untrusted: a
// storage called ".." would mount over the home folder, a file called "../x"
// would escape pull -r, and "invoice\u202Efdp.exe" would show as "invoiceexe.pdf".
func ValidName(s string) bool {
	if s == "" || s == "." || s == ".." || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '/' || unsafeRune(r) {
			return false
		}
	}
	return true
}

// unsafeRune reports control characters, line and paragraph separators, and
// the bidirectional controls that can make a name display as something else.
// (Zero-width joiners stay allowed: emoji sequences in file names use them.)
func unsafeRune(r rune) bool {
	switch {
	case unicode.IsControl(r), r == '\u2028', r == '\u2029':
		return true
	case r == '\u061C', r == '\u200E', r == '\u200F',
		r >= '\u202A' && r <= '\u202E', r >= '\u2066' && r <= '\u2069':
		return true
	}
	return false
}

// Printable replaces unsafe characters in text from the device (model names,
// errors) before it's printed or logged, so a hostile device can't send
// terminal escape sequences or forge log lines.
func Printable(s string) string {
	if utf8.ValidString(s) {
		ok := true
		for _, r := range s {
			if unsafeRune(r) {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unsafeRune(r) {
			return '?'
		}
		return r
	}, s)
}

// storageName turns a device-supplied storage description into a volume
// name, or "" if nothing usable is left. Slashes become dashes and leading
// dots go, so a volume is never hidden or named "." or "..".
func storageName(s string) string {
	s = strings.TrimLeft(strings.ReplaceAll(strings.TrimSpace(s), "/", "-"), ".")
	if !ValidName(s) {
		return ""
	}
	return s
}

// validObjects drops objects whose names can't be used safely (see ValidName)
// and reports how many it dropped. They're not shown, but a folder holding
// them isn't empty (see Empty).
func validObjects(objs []*mtp.Object) ([]*mtp.Object, int) {
	for i, o := range objs {
		if ValidName(o.Name) {
			continue
		}
		out := append([]*mtp.Object(nil), objs[:i]...)
		for _, o := range objs[i+1:] {
			if ValidName(o.Name) {
				out = append(out, o)
			}
		}
		return out, len(objs) - len(out)
	}
	return objs, 0
}

// isGenericInternal reports descriptions that name the phone's built-in
// memory rather than anything the user chose ("Internal shared storage").
func isGenericInternal(desc string) bool {
	d := strings.ToLower(desc)
	return strings.Contains(d, "internal") || d == "phone" || d == "phone storage"
}

// Clean normalizes p to an absolute slash path.
func Clean(p string) string { return path.Clean("/" + p) }

func notExist(p string) error { return &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist} }

// Stat resolves a path.
func (f *FS) Stat(ctx context.Context, p string) (*Entry, error) {
	p = Clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == "/" {
		return &Entry{Path: "/", Name: "/", IsDir: true}, nil
	}
	parts := strings.Split(p[1:], "/")
	storages, err := f.storagesLocked(ctx)
	if err != nil {
		return nil, err
	}
	// First exact name, then name ignoring case, then the phone's own
	// description: always the first match, so two storages with the same
	// description resolve predictably.
	var st *Storage
	for _, match := range []func(*Storage) bool{
		func(s *Storage) bool { return s.Name == parts[0] },
		func(s *Storage) bool { return strings.EqualFold(s.Name, parts[0]) },
		func(s *Storage) bool { return strings.EqualFold(s.Alias, parts[0]) },
	} {
		for _, s := range storages {
			if st == nil && match(s) {
				st = s
			}
		}
	}
	if st == nil {
		return nil, notExist(p)
	}
	e := &Entry{Path: "/" + st.Name, Name: st.Name, StorageID: st.ID, Handle: ptp.HandleRoot, IsDir: true, Storage: st}
	for _, name := range parts[1:] {
		if !e.IsDir {
			return nil, &fs.PathError{Op: "stat", Path: p, Err: fmt.Errorf("%s is not a directory", e.Path)}
		}
		objs, err := f.listLocked(ctx, e.StorageID, e.Handle)
		if err != nil {
			return nil, err
		}
		o := find(objs, name)
		if o == nil {
			return nil, notExist(p)
		}
		e = entryFor(path.Join(e.Path, o.Name), o)
	}
	return e, nil
}

func find(objs []*mtp.Object, name string) *mtp.Object {
	for _, o := range objs {
		if o.Name == name {
			return o
		}
	}
	// macOS often sends decomposed Unicode (NFD) while Android stores
	// composed names (NFC): compare normalized forms.
	nname := norm.NFC.String(name)
	for _, o := range objs {
		if norm.NFC.String(o.Name) == nname {
			return o
		}
	}
	for _, o := range objs {
		if strings.EqualFold(norm.NFC.String(o.Name), nname) {
			return o
		}
	}
	return nil
}

func entryFor(p string, o *mtp.Object) *Entry {
	return &Entry{Path: p, Name: o.Name, StorageID: o.StorageID, Handle: o.Handle,
		IsDir: o.IsDir, Size: o.Size, Modified: o.Modified}
}

func (f *FS) listLocked(ctx context.Context, storage, handle uint32) ([]*mtp.Object, error) {
	k := dirKey{storage, handle}
	if d, ok := f.dirs[k]; ok && (f.TTL == 0 || time.Since(d.at) < f.TTL) {
		return d.objs, nil
	}
	objs, err := f.b.List(ctx, storage, handle)
	if err != nil {
		return nil, err
	}
	objs, hidden := validObjects(objs)
	f.dirs[k] = dirEntry{objs: objs, hidden: hidden, at: time.Now()}
	return objs, nil
}

// Empty reports whether a folder has no children at all, counting the ones
// ReadDir hides. Deleting a folder on the phone deletes everything in it, so
// "rmdir" must not mistake hidden children for an empty folder.
func (f *FS) Empty(ctx context.Context, e *Entry) (bool, error) {
	if !e.IsDir || e.IsRoot() {
		return false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	objs, err := f.listLocked(ctx, e.StorageID, e.Handle)
	if err != nil {
		return false, err
	}
	return len(objs) == 0 && f.dirs[dirKey{e.StorageID, e.Handle}].hidden == 0, nil
}

// ReadDir lists a directory. For "/" it returns the storages.
func (f *FS) ReadDir(ctx context.Context, p string) ([]*Entry, error) {
	e, err := f.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	return f.ReadDirEntry(ctx, e)
}

// ReadDirEntry lists an already resolved directory.
func (f *FS) ReadDirEntry(ctx context.Context, e *Entry) ([]*Entry, error) {
	if !e.IsDir {
		return nil, &fs.PathError{Op: "readdir", Path: e.Path, Err: fmt.Errorf("not a directory")}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.IsRoot() {
		storages, err := f.storagesLocked(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]*Entry, len(storages))
		for i, s := range storages {
			out[i] = &Entry{Path: "/" + s.Name, Name: s.Name, StorageID: s.ID, Handle: ptp.HandleRoot, IsDir: true, Storage: s}
		}
		return out, nil
	}
	objs, err := f.listLocked(ctx, e.StorageID, e.Handle)
	if err != nil {
		return nil, err
	}
	out := make([]*Entry, len(objs))
	for i, o := range objs {
		out[i] = entryFor(path.Join(e.Path, o.Name), o)
	}
	return out, nil
}

// Invalidate drops the cached listing of a directory.
func (f *FS) Invalidate(storage, handle uint32) {
	f.mu.Lock()
	delete(f.dirs, dirKey{storage, handle})
	f.mu.Unlock()
}

// Reset drops every cached listing (and storage info).
func (f *FS) Reset() {
	f.mu.Lock()
	f.dirs = map[dirKey]dirEntry{}
	f.storages = nil
	f.mu.Unlock()
}
