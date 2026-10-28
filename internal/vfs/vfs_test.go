package vfs

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

type fakeBackend struct {
	objs  map[uint32][]*mtp.Object // parent handle -> children (single storage 1)
	lists int
}

func (b *fakeBackend) StorageIDs(context.Context) ([]uint32, error) { return []uint32{1, 2}, nil }
func (b *fakeBackend) StorageInfo(_ context.Context, id uint32) (*ptp.StorageInfo, error) {
	if id == 1 {
		return &ptp.StorageInfo{Description: "Internal shared storage"}, nil
	}
	return &ptp.StorageInfo{VolumeLabel: "SD card"}, nil
}
func (b *fakeBackend) List(_ context.Context, storage, parent uint32) ([]*mtp.Object, error) {
	b.lists++
	if storage != 1 {
		return nil, nil
	}
	return b.objs[parent], nil
}

func newBackend() *fakeBackend {
	return &fakeBackend{objs: map[uint32][]*mtp.Object{
		ptp.HandleRoot: {{Handle: 10, StorageID: 1, Name: "DCIM", IsDir: true}},
		10:             {{Handle: 11, StorageID: 1, Name: "Camera", IsDir: true}},
		11:             {{Handle: 12, StorageID: 1, Name: "IMG_1.jpg", Size: 42}},
	}}
}

func TestResolve(t *testing.T) {
	b := newBackend()
	f := New(b)
	ctx := context.Background()

	root, err := f.ReadDir(ctx, "/")
	if err != nil || len(root) != 2 || root[0].Name != "Internal shared storage" || root[1].Name != "SD card" {
		t.Fatalf("root %+v %v", root, err)
	}
	e, err := f.Stat(ctx, "/Internal shared storage/DCIM/Camera/IMG_1.jpg")
	if err != nil || e.Handle != 12 || e.Size != 42 || e.IsDir {
		t.Fatalf("%+v %v", e, err)
	}
	if e, err := f.Stat(ctx, "internal shared storage/dcim/./camera/../Camera"); err != nil || e.Handle != 11 {
		t.Fatalf("case-insensitive/clean lookup: %+v %v", e, err)
	}
	if _, err := f.Stat(ctx, "/Internal shared storage/Nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected not exist, got %v", err)
	}
	if _, err := f.Stat(ctx, "/Internal shared storage/DCIM/Camera/IMG_1.jpg/x"); err == nil {
		t.Fatal("expected error traversing a file")
	}
	before := b.lists
	f.Stat(ctx, "/Internal shared storage/DCIM/Camera/IMG_1.jpg")
	if b.lists != before {
		t.Fatal("expected cached listings")
	}
	f.Invalidate(1, 11)
	f.Stat(ctx, "/Internal shared storage/DCIM/Camera/IMG_1.jpg")
	if b.lists != before+1 {
		t.Fatalf("expected exactly one refetch, got %d", b.lists-before)
	}
}

func TestDeviceName(t *testing.T) {
	f := New(newBackend())
	f.DeviceName = "Retroid Pocket 6"
	ctx := context.Background()
	root, err := f.ReadDir(ctx, "/")
	if err != nil || root[0].Name != "Retroid Pocket 6" || root[1].Name != "SD card" {
		t.Fatalf("root %+v %v", root, err)
	}
	// The phone's own description still works as a path.
	for _, p := range []string{"/Retroid Pocket 6/DCIM", "/Internal shared storage/DCIM"} {
		if e, err := f.Stat(ctx, p); err != nil || e.Handle != 10 {
			t.Fatalf("%s: %+v %v", p, e, err)
		}
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"": false, ".": false, "..": false, "a/b": false, "../x": false, "x\x00": false,
		"\x1b[2J": false, "tab\there": false, "\xff\xfe": false,
		"ok": true, "Música": true, ".hidden": true, "a,b": true, "IMG 1 (2).jpg": true, "...": true,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

// hostileBackend is a device that sends whatever names a test gives it.
type hostileBackend struct {
	storages map[uint32]*ptp.StorageInfo
	objs     []*mtp.Object // children of storage 1's root
}

func (b *hostileBackend) StorageIDs(context.Context) ([]uint32, error) {
	ids := make([]uint32, 0, len(b.storages))
	for id := uint32(1); int(id) <= len(b.storages); id++ {
		ids = append(ids, id)
	}
	return ids, nil
}
func (b *hostileBackend) StorageInfo(_ context.Context, id uint32) (*ptp.StorageInfo, error) {
	return b.storages[id], nil
}
func (b *hostileBackend) List(_ context.Context, storage, parent uint32) ([]*mtp.Object, error) {
	if storage == 1 && parent == ptp.HandleRoot {
		return b.objs, nil
	}
	return nil, nil
}

func TestHostileStorageNames(t *testing.T) {
	b := &hostileBackend{storages: map[uint32]*ptp.StorageInfo{
		1: {Description: ".."},
		2: {Description: "."},
		3: {Description: "\x1b]0;owned\x07"},
		4: {Description: "", VolumeLabel: "../x"},
		5: {Description: ".hidden"},
		6: {Description: "Internal shared storage"}, // DeviceName below is hostile
		7: {Description: "SD/Card"},
	}}
	f := New(b)
	f.DeviceName = ".."
	got, err := f.Storages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Storage 00000001", "Storage 00000002", "Storage 00000003", "-x", "hidden", "Internal shared storage", "SD-Card"}
	for i, s := range got {
		if s.Name != want[i] {
			t.Errorf("storage %d: name %q, want %q", s.ID, s.Name, want[i])
		}
		if !ValidName(s.Name) || s.Name[0] == '.' {
			t.Errorf("storage %d: unsafe name %q", s.ID, s.Name)
		}
	}
}

func TestHostileObjectNamesAreHidden(t *testing.T) {
	b := &hostileBackend{
		storages: map[uint32]*ptp.StorageInfo{1: {Description: "SD card"}},
		objs: []*mtp.Object{
			{Handle: 1, StorageID: 1, Name: "../evil"},
			{Handle: 2, StorageID: 1, Name: "a/b", IsDir: true},
			{Handle: 3, StorageID: 1, Name: "x\x01"},
			{Handle: 4, StorageID: 1, Name: ".."},
			{Handle: 5, StorageID: 1, Name: ""},
			{Handle: 6, StorageID: 1, Name: "good.txt"},
		},
	}
	f := New(b)
	ctx := context.Background()
	entries, err := f.ReadDir(ctx, "/SD card")
	if err != nil || len(entries) != 1 || entries[0].Name != "good.txt" {
		t.Fatalf("listing should only show good.txt: %+v %v", entries, err)
	}
	for _, p := range []string{"/SD card/x\x01", "/SD card/"} {
		if e, err := f.Stat(ctx, p); err == nil && e.Handle != ptp.HandleRoot {
			t.Errorf("Stat(%q) resolved hidden object %+v", p, e)
		}
	}
}

func TestUnsafeDisplayCharacters(t *testing.T) {
	for _, name := range []string{"invoice‮fdp.exe", "a⁦b", "line break", "x‏y"} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) should be false", name)
		}
		if got := Printable(name); strings.ContainsAny(got, "‮⁦ ‏") {
			t.Errorf("Printable(%q) = %q", name, got)
		}
	}
	if !ValidName("family 👨‍👩‍👧.jpg") {
		t.Error("emoji sequences with zero-width joiners must stay valid")
	}
	if got := Printable("Pixel\x1b]0;x\x07\xff"); got != "Pixel?]0;x??" {
		t.Errorf("Printable = %q", got)
	}
}

func TestStorageNamesAreUniqueOnAPFS(t *testing.T) {
	b := &hostileBackend{storages: map[uint32]*ptp.StorageInfo{
		1: {Description: "X"}, 2: {Description: "X (2)"}, 3: {Description: "X"},
		4: {Description: "sd"}, 5: {Description: "SD"},
		6: {Description: "Música"}, 7: {Description: "Música"},
	}}
	got, err := New(b).Storages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, s := range got {
		k := strings.ToLower(norm.NFC.String(s.Name))
		if prev, dup := seen[k]; dup {
			t.Errorf("storages %q and %q share a mount point", prev, s.Name)
		}
		seen[k] = s.Name
	}
}

func TestEmptyCountsHiddenChildren(t *testing.T) {
	b := &hostileBackend{
		storages: map[uint32]*ptp.StorageInfo{1: {Description: "SD card"}},
		objs:     []*mtp.Object{{Handle: 1, StorageID: 1, Name: "x\x01"}},
	}
	f := New(b)
	root, err := f.Stat(context.Background(), "/SD card")
	if err != nil {
		t.Fatal(err)
	}
	if empty, err := f.Empty(context.Background(), root); err != nil || empty {
		t.Fatalf("Empty = %v, %v; want false (a hidden child)", empty, err)
	}
}

// Two storages described the same way: the description resolves to the
// first one, never to "SD card (2)".
func TestAliasResolvesToFirstMatch(t *testing.T) {
	b := &hostileBackend{storages: map[uint32]*ptp.StorageInfo{
		1: {Description: "SD card"}, 2: {Description: "SD card"},
	}}
	f := New(b)
	ctx := context.Background()
	for _, p := range []string{"/SD card", "/sd card"} {
		e, err := f.Stat(ctx, p)
		if err != nil || e.StorageID != 1 {
			t.Errorf("Stat(%q) = storage %v, %v; want 1", p, e, err)
		}
	}
	if e, err := f.Stat(ctx, "/SD card (2)"); err != nil || e.StorageID != 2 {
		t.Errorf("Stat(SD card (2)) = %v, %v", e, err)
	}
}

func TestNamelessChildrenKeepFoldersNonEmpty(t *testing.T) {
	b := &hostileBackend{
		storages: map[uint32]*ptp.StorageInfo{1: {Description: "SD card"}},
		objs:     []*mtp.Object{{Handle: 1, StorageID: 1, Name: ""}},
	}
	f := New(b)
	root, _ := f.Stat(context.Background(), "/SD card")
	if empty, err := f.Empty(context.Background(), root); err != nil || empty {
		t.Fatalf("Empty = %v, %v; want false", empty, err)
	}
}

func TestDeviceNameByStorageType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		storages map[uint32]*ptp.StorageInfo
		want     []string
	}{
		{"localized internal + SD", map[uint32]*ptp.StorageInfo{
			1: {StorageType: ptp.StorageFixedRAM, Description: "Almacenamiento interno compartido"},
			2: {StorageType: ptp.StorageRemovableRAM, Description: "Retroid4"},
		}, []string{"Retroid Pocket 4 Pro", "Retroid4"}},
		{"two built-in storages keep their descriptions", map[uint32]*ptp.StorageInfo{
			1: {StorageType: ptp.StorageFixedRAM, Description: "Speicher A"},
			2: {StorageType: ptp.StorageFixedRAM, Description: "Speicher B"},
		}, []string{"Speicher A", "Speicher B"}},
		{"unknown type falls back to the English rule", map[uint32]*ptp.StorageInfo{
			1: {Description: "Internal shared storage"},
			2: {Description: "Stockage interne partagé"},
		}, []string{"Retroid Pocket 4 Pro", "Stockage interne partagé"}},
	} {
		f := New(&hostileBackend{storages: tc.storages})
		f.DeviceName = "Retroid Pocket 4 Pro"
		got, err := f.Storages(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range got {
			if s.Name != tc.want[i] {
				t.Errorf("%s: storage %d named %q, want %q", tc.name, s.ID, s.Name, tc.want[i])
			}
		}
	}
}

func TestCustomNames(t *testing.T) {
	b := &hostileBackend{storages: map[uint32]*ptp.StorageInfo{
		1: {StorageType: ptp.StorageFixedRAM, Description: "Internal shared storage"},
		2: {StorageType: ptp.StorageRemovableRAM, Description: "SD card"},
		3: {StorageType: ptp.StorageRemovableRAM, Description: "USB"},
	}}
	f := New(b)
	f.DeviceName = "Phone"
	f.Custom = map[uint32]string{1: "Games", 2: "../evil", 3: "games"}
	got, err := f.Storages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// An invalid stored name is ignored; a colliding one gets a suffix.
	want := []string{"Games", "SD card", "games (2)"}
	for i, s := range got {
		if s.Name != want[i] {
			t.Errorf("storage %d named %q, want %q", s.ID, s.Name, want[i])
		}
	}
	if e, err := f.Stat(context.Background(), "/Internal shared storage"); err != nil || e.StorageID != 1 {
		t.Errorf("the phone's own description must still resolve: %+v %v", e, err)
	}
}

func TestCheckVolumeName(t *testing.T) {
	for name, ok := range map[string]bool{
		"Games": true, "Música 2": true, "a,b": true,
		"": false, " x": false, "x ": false, ".hidden": false, "a/b": false, "a:b": false,
		"..": false, "x\x1b": false, "x‮": false, strings.Repeat("a", MaxVolumeName+1): false,
	} {
		if err := CheckVolumeName(name); (err == nil) != ok {
			t.Errorf("CheckVolumeName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
}
