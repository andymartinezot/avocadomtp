package vfs

import (
	"context"
	"errors"
	"io/fs"
	"testing"

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
