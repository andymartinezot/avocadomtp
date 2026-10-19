package fusefs

import (
	"bytes"
	"context"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/winfsp/cgofuse/fuse"

	"github.com/andymartinezot/avocadomtp/internal/mtp/mtptest"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

const internal = 0x00010001

func newFS(t *testing.T) (*FS, *mtptest.Phone) {
	p := mtptest.NewPhone(t)
	return New(p, "Internal shared storage", internal, t.TempDir()), p
}

func writeFile(t *testing.T, f *FS, p string, data []byte) {
	t.Helper()
	rc, fh := f.Create(p, fuse.O_WRONLY|fuse.O_CREAT, 0o644)
	if rc != 0 {
		t.Fatalf("create %s: %d", p, rc)
	}
	for off := 0; off < len(data); off += 64 << 10 {
		end := min(off+64<<10, len(data))
		if n := f.Write(p, data[off:end], int64(off), fh); n != end-off {
			t.Fatalf("write %s: %d", p, n)
		}
	}
	if rc := f.Release(p, fh); rc != 0 {
		t.Fatalf("release %s: %d", p, rc)
	}
}

func readFile(t *testing.T, f *FS, p string) []byte {
	t.Helper()
	rc, fh := f.Open(p, fuse.O_RDONLY)
	if rc != 0 {
		t.Fatalf("open %s: %d", p, rc)
	}
	defer f.Release(p, fh)
	var out []byte
	buf := make([]byte, 100<<10) // odd size: reads straddle read-ahead blocks
	for {
		n := f.Read(p, buf, int64(len(out)), fh)
		if n < 0 {
			t.Fatalf("read %s: %d", p, n)
		}
		if n == 0 {
			return out
		}
		out = append(out, buf[:n]...)
	}
}

func names(t *testing.T, f *FS, dir string) []string {
	t.Helper()
	var out []string
	rc := f.Readdir(dir, func(name string, _ *fuse.Stat_t, _ int64) bool {
		if name != "." && name != ".." {
			out = append(out, name)
		}
		return true
	}, 0, 0)
	if rc != 0 {
		t.Fatalf("readdir %s: %d", dir, rc)
	}
	return out
}

func phoneNames(t *testing.T, p *mtptest.Phone, parent uint32) []string {
	objs, err := p.List(context.Background(), internal, parent)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range objs {
		out = append(out, o.Name)
	}
	return out
}

func TestWriteReadOverwrite(t *testing.T) {
	f, p := newFS(t)
	data := make([]byte, 9<<20+123) // spans several read-ahead blocks
	rand.New(rand.NewSource(1)).Read(data)
	writeFile(t, f, "/movie.bin", data)
	if got := readFile(t, f, "/movie.bin"); !bytes.Equal(got, data) {
		t.Fatalf("read back %d bytes, want %d", len(got), len(data))
	}
	var st fuse.Stat_t
	if rc := f.Getattr("/movie.bin", &st, 0); rc != 0 || st.Size != int64(len(data)) || st.Mode&fuse.S_IFREG == 0 {
		t.Fatalf("getattr rc=%d %+v", rc, st)
	}

	// Overwrite with O_TRUNC: the phone must end up with exactly one, new file.
	rc, fh := f.Open("/movie.bin", fuse.O_WRONLY|fuse.O_TRUNC)
	if rc != 0 {
		t.Fatal(rc)
	}
	f.Write("/movie.bin", []byte("short"), 0, fh)
	if rc := f.Release("/movie.bin", fh); rc != 0 {
		t.Fatal(rc)
	}
	if got := string(readFile(t, f, "/movie.bin")); got != "short" {
		t.Fatalf("after overwrite: %q", got)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 1 || n[0] != "movie.bin" {
		t.Fatalf("phone root after overwrite: %v", n)
	}

	// Append in place (open without O_TRUNC stages the existing content).
	rc, fh = f.Open("/movie.bin", fuse.O_RDWR)
	if rc != 0 {
		t.Fatal(rc)
	}
	f.Write("/movie.bin", []byte("!"), 5, fh)
	f.Release("/movie.bin", fh)
	if got := string(readFile(t, f, "/movie.bin")); got != "short!" {
		t.Fatalf("after append: %q", got)
	}

	// Truncate without an open handle.
	if rc := f.Truncate("/movie.bin", 2, ^uint64(0)); rc != 0 {
		t.Fatal(rc)
	}
	if got := string(readFile(t, f, "/movie.bin")); got != "sh" {
		t.Fatalf("after truncate: %q", got)
	}
}

func TestEmptyFileAndDirs(t *testing.T) {
	f, p := newFS(t)
	writeFile(t, f, "/empty.txt", nil)
	if rc := f.Mkdir("/Music", 0o755); rc != 0 {
		t.Fatal(rc)
	}
	if rc := f.Mkdir("/Music", 0o755); rc != -fuse.EEXIST {
		t.Fatalf("mkdir existing: %d", rc)
	}
	writeFile(t, f, "/Music/song.mp3", []byte("la la"))
	if rc := f.Rmdir("/Music"); rc != -fuse.ENOTEMPTY {
		t.Fatalf("rmdir non-empty: %d", rc)
	}
	if rc := f.Rename("/Music/song.mp3", "/tune.mp3"); rc != 0 {
		t.Fatalf("rename/move: %d", rc)
	}
	if rc := f.Rmdir("/Music"); rc != 0 {
		t.Fatalf("rmdir: %d", rc)
	}
	if got := names(t, f, "/"); len(got) != 2 {
		t.Fatalf("root: %v", got)
	}
	if rc := f.Unlink("/tune.mp3"); rc != 0 {
		t.Fatal(rc)
	}
	var st fuse.Stat_t
	if rc := f.Getattr("/tune.mp3", &st, 0); rc != -fuse.ENOENT {
		t.Fatalf("getattr deleted: %d", rc)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 1 || n[0] != "empty.txt" {
		t.Fatalf("phone: %v", n)
	}
}

func TestRenameReplaces(t *testing.T) {
	f, p := newFS(t)
	writeFile(t, f, "/a.txt", []byte("A"))
	writeFile(t, f, "/b.txt", []byte("B"))
	if rc := f.Rename("/a.txt", "/b.txt"); rc != 0 {
		t.Fatal(rc)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 1 {
		t.Fatalf("phone: %v", n)
	}
	if got := string(readFile(t, f, "/b.txt")); got != "A" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestMacMetadataStaysOnMac(t *testing.T) {
	f, p := newFS(t)
	writeFile(t, f, "/photo.jpg", []byte("jpg"))
	writeFile(t, f, "/._photo.jpg", []byte("appledouble"))
	writeFile(t, f, "/.DS_Store", []byte("ds"))
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 1 || n[0] != "photo.jpg" {
		t.Fatalf("metadata leaked to the phone: %v", n)
	}
	if got := string(readFile(t, f, "/._photo.jpg")); got != "appledouble" {
		t.Fatalf("shadow read: %q", got)
	}
	if got := names(t, f, "/"); len(got) != 1 || got[0] != "photo.jpg" {
		t.Fatalf("metadata files must stay hidden from listings: %v", got)
	}
	// The AppleDouble companion follows its file.
	f.Rename("/photo.jpg", "/pic.jpg")
	if got := string(readFile(t, f, "/._pic.jpg")); got != "appledouble" {
		t.Fatalf("companion not moved: %q", got)
	}
	if rc := f.Mkdir("/.Trashes", 0o755); rc != -fuse.EACCES {
		t.Fatalf(".Trashes must be refused, got %d", rc)
	}
	var st fuse.Stat_t
	if rc := f.Getattr("/.metadata_never_index", &st, 0); rc != 0 {
		t.Fatalf(".metadata_never_index: %d", rc)
	}
}

func TestStatfs(t *testing.T) {
	f, _ := newFS(t)
	var st fuse.Statfs_t
	if rc := f.Statfs("/", &st); rc != 0 || st.Blocks*st.Bsize != 64<<30 || st.Bavail*st.Bsize != 32<<30 {
		t.Fatalf("statfs rc=%d %+v", rc, st)
	}
}

func TestDecomposedUnicodeNames(t *testing.T) {
	f, p := newFS(t)
	composed := "/a\u00f1o.txt"    // ñ as one code point (how Android stores it)
	decomposed := "/an\u0303o.txt" // n + combining tilde (how macOS often sends it)
	writeFile(t, f, composed, []byte("x"))
	var st fuse.Stat_t
	if rc := f.Getattr(decomposed, &st, 0); rc != 0 {
		t.Fatalf("getattr with NFD name: %d", rc)
	}
	if rc := f.Unlink(decomposed); rc != 0 {
		t.Fatalf("unlink with NFD name: %d", rc)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 0 {
		t.Fatalf("phone: %v", n)
	}
}

func TestRenameAcrossMetadataStore(t *testing.T) {
	f, p := newFS(t)
	// ditto-style: write a temp file on the phone, rename it to .DS_Store
	writeFile(t, f, "/.BC.tmp", []byte("ds"))
	if rc := f.Rename("/.BC.tmp", "/.DS_Store"); rc != 0 {
		t.Fatalf("rename to metadata: %d", rc)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 0 {
		t.Fatalf("temp file left on phone: %v", n)
	}
	if got := string(readFile(t, f, "/.DS_Store")); got != "ds" {
		t.Fatalf(".DS_Store = %q", got)
	}
	// and back: a metadata file renamed to a normal name goes to the phone
	writeFile(t, f, "/._x", []byte("ad"))
	if rc := f.Rename("/._x", "/x"); rc != 0 {
		t.Fatalf("rename from metadata: %d", rc)
	}
	if n := phoneNames(t, p, ptp.HandleRoot); len(n) != 1 || n[0] != "x" {
		t.Fatalf("phone: %v", n)
	}
}

// TestReadAheadAdapts checks that small random reads stay small while
// sequential reads grow the read-ahead window.
func TestReadAheadAdapts(t *testing.T) {
	f, p := newFS(t)
	data := make([]byte, 20<<20)
	rand.New(rand.NewSource(2)).Read(data)
	writeFile(t, f, "/big.bin", data)

	before := p.Stats().BytesIn
	rc, fh := f.Open("/big.bin", fuse.O_RDONLY)
	if rc != 0 {
		t.Fatal(rc)
	}
	buf := make([]byte, 64<<10)
	if n := f.Read("/big.bin", buf, 0, fh); n != len(buf) || !bytes.Equal(buf, data[:len(buf)]) {
		t.Fatalf("header read: %d", n)
	}
	f.Release("/big.bin", fh)
	if got := p.Stats().BytesIn - before; got > minReadAhead+4096 { // + metadata lookups
		t.Fatalf("reading a 64 KiB header transferred %d bytes", got)
	}

	before = p.Stats().BytesIn
	if got := readFile(t, f, "/big.bin"); !bytes.Equal(got, data) {
		t.Fatal("sequential read mismatch")
	}
	if got := p.Stats().BytesIn - before; got > uint64(len(data))+maxReadAhead {
		t.Fatalf("sequential read transferred %d bytes for %d", got, len(data))
	}
}

// FUSE-T's FSKit backend opens files read-write even for plain reads; that
// must not download the whole file or upload anything.
func TestReadWriteOpenWithoutWritesStaysCheap(t *testing.T) {
	f, p := newFS(t)
	data := make([]byte, 10<<20)
	rand.New(rand.NewSource(3)).Read(data)
	writeFile(t, f, "/photo.jpg", data)
	in, out := p.Stats().BytesIn, p.Stats().BytesOut

	rc, fh := f.Open("/photo.jpg", fuse.O_RDWR)
	if rc != 0 {
		t.Fatal(rc)
	}
	buf := make([]byte, 64<<10)
	if n := f.Read("/photo.jpg", buf, 0, fh); n != len(buf) || !bytes.Equal(buf, data[:len(buf)]) {
		t.Fatalf("read: %d", n)
	}
	if rc := f.Release("/photo.jpg", fh); rc != 0 {
		t.Fatal(rc)
	}
	if got := p.Stats().BytesIn - in; got > minReadAhead+4096 {
		t.Fatalf("read-write open transferred %d bytes from the phone", got)
	}
	if p.Stats().BytesOut != out {
		t.Fatal("an unmodified file was uploaded")
	}
}

// A folder whose only children have names vfs hides must not look empty:
// deleting a folder on the phone deletes everything in it.
func TestRmdirKeepsFoldersWithHiddenChildren(t *testing.T) {
	f, p := newFS(t)
	ctx := context.Background()
	if rc := f.Mkdir("/keep", 0o755); rc != 0 {
		t.Fatalf("mkdir: %d", rc)
	}
	if rc := f.Mkdir("/target", 0o755); rc != 0 {
		t.Fatalf("mkdir: %d", rc)
	}
	dir, err := f.vfs.Stat(ctx, f.remotePath("/keep"))
	if err != nil {
		t.Fatal(err)
	}
	// Bypass vfs, like a phone that already holds such a file.
	if _, err := p.Upload(ctx, internal, dir.Handle, "bad\x01name", 3, time.Now(), strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	f.vfs.Invalidate(internal, dir.Handle)
	if rc := f.Rmdir("/keep"); rc != -fuse.ENOTEMPTY {
		t.Fatalf("rmdir of a folder with a hidden child = %d, want ENOTEMPTY", rc)
	}
	if rc := f.Rename("/target", "/keep"); rc != -fuse.ENOTEMPTY {
		t.Fatalf("rename over a folder with a hidden child = %d, want ENOTEMPTY", rc)
	}
	objs, err := p.List(ctx, internal, dir.Handle)
	if err != nil || len(objs) != 1 {
		t.Fatalf("the hidden file must still be on the phone: %v %v", objs, err)
	}
}
