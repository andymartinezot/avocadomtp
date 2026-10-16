package fusefs

import (
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/winfsp/cgofuse/fuse"
)

const maxShadowFile = 16 << 20

// shadow keeps macOS metadata files in memory so they never reach the phone.
type shadow struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
	mtime map[string]time.Time
}

func newShadow() *shadow {
	return &shadow{files: map[string][]byte{}, dirs: map[string]bool{}, mtime: map[string]time.Time{}}
}

// refused folders: without them Finder deletes immediately instead of copying
// files into a trash, and Spotlight / fseventsd leave the volume alone.
func refusedDir(p string) bool {
	switch path.Base(p) {
	case ".Trashes", ".Spotlight-V100", ".fseventsd", ".TemporaryItems":
		return true
	}
	return false
}

func (s *shadow) getattr(f *FS, p string, st *fuse.Stat_t) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.files[p]; ok {
		f.fill(st, false, uint64(len(b)), s.mtime[p])
		return 0
	}
	if s.dirs[p] {
		f.fill(st, true, 0, s.mtime[p])
		return 0
	}
	if p == "/.metadata_never_index" { // asks Spotlight not to index the phone
		f.fill(st, false, 0, time.Time{})
		return 0
	}
	return -fuse.ENOENT
}

func (s *shadow) create(p string) int {
	if refusedDir(path.Dir(p)) {
		return -fuse.EACCES
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[p] = []byte{}
	s.mtime[p] = time.Now()
	return 0
}

// put stores a whole file (from a rename onto a metadata name).
func (s *shadow) put(p string, data []byte) int {
	if refusedDir(path.Dir(p)) {
		return -fuse.EACCES
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[p] = data
	s.mtime[p] = time.Now()
	return 0
}

func (s *shadow) get(p string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[p]
	return b, ok
}

func (s *shadow) open(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.files[p]; ok || p == "/.metadata_never_index" {
		return 0
	}
	return -fuse.ENOENT
}

func (s *shadow) read(p string, buff []byte, ofst int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.files[p]
	if ofst >= int64(len(b)) {
		return 0
	}
	return copy(buff, b[ofst:])
}

func (s *shadow) write(p string, buff []byte, ofst int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[p]
	if !ok {
		return -fuse.ENOENT
	}
	end := ofst + int64(len(buff))
	if end > maxShadowFile {
		return -fuse.EFBIG
	}
	if end > int64(len(b)) {
		b = append(b, make([]byte, end-int64(len(b)))...)
	}
	copy(b[ofst:], buff)
	s.files[p] = b
	s.mtime[p] = time.Now()
	return len(buff)
}

func (s *shadow) truncate(p string, size int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[p]
	if !ok {
		return -fuse.ENOENT
	}
	if size > maxShadowFile {
		return -fuse.EFBIG
	}
	if size <= int64(len(b)) {
		b = b[:size]
	} else {
		b = append(b, make([]byte, size-int64(len(b)))...)
	}
	s.files[p] = b
	return 0
}

func (s *shadow) unlink(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.files[p]; !ok {
		return -fuse.ENOENT
	}
	delete(s.files, p)
	delete(s.mtime, p)
	return 0
}

func (s *shadow) mkdir(p string) int {
	if refusedDir(p) || refusedDir(path.Dir(p)) {
		return -fuse.EACCES
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirs[p] = true
	s.mtime[p] = time.Now()
	return 0
}

func (s *shadow) rmdir(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirs[p] {
		return -fuse.ENOENT
	}
	if len(s.childrenLocked(p)) > 0 {
		return -fuse.ENOTEMPTY
	}
	delete(s.dirs, p)
	return 0
}

// rename moves a shadow entry and everything below it (no-op if absent).
func (s *shadow) rename(oldp, newp string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, m := range []map[string][]byte{s.files} {
		for k, v := range m {
			if k == oldp || strings.HasPrefix(k, oldp+"/") {
				delete(m, k)
				m[newp+strings.TrimPrefix(k, oldp)] = v
				found = true
			}
		}
	}
	for k := range s.dirs {
		if k == oldp || strings.HasPrefix(k, oldp+"/") {
			delete(s.dirs, k)
			s.dirs[newp+strings.TrimPrefix(k, oldp)] = true
			found = true
		}
	}
	// AppleDouble companions follow their file: /a/x -> /b/y moves /a/._x to /b/._y
	oldAD := path.Join(path.Dir(oldp), "._"+path.Base(oldp))
	if b, ok := s.files[oldAD]; ok {
		delete(s.files, oldAD)
		s.files[path.Join(path.Dir(newp), "._"+path.Base(newp))] = b
		found = true
	}
	if !found && isShadow(oldp) {
		return -fuse.ENOENT
	}
	return 0
}

// removeTree drops every shadow entry at or below p.
func (s *shadow) removeTree(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.files {
		if k == p || strings.HasPrefix(k, p+"/") {
			delete(s.files, k)
		}
	}
	for k := range s.dirs {
		if k == p || strings.HasPrefix(k, p+"/") {
			delete(s.dirs, k)
		}
	}
}

func (s *shadow) children(dir string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.childrenLocked(dir)
}

func (s *shadow) childrenLocked(dir string) []string {
	seen := map[string]bool{}
	for _, m := range []map[string]bool{s.dirs, keys(s.files)} {
		for k := range m {
			if path.Dir(k) == dir && k != dir {
				seen[path.Base(k)] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keys(m map[string][]byte) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func (s *shadow) readdir(f *FS, p string, fill func(string, *fuse.Stat_t, int64) bool) int {
	s.mu.Lock()
	ok := s.dirs[p]
	s.mu.Unlock()
	if !ok {
		return -fuse.ENOENT
	}
	var st fuse.Stat_t
	f.fill(&st, true, 0, time.Time{})
	fill(".", &st, 0)
	fill("..", &st, 0)
	for _, name := range s.children(p) {
		var cst fuse.Stat_t
		if s.getattr(f, path.Join(p, name), &cst) == 0 && !fill(name, &cst, 0) {
			break
		}
	}
	return 0
}
