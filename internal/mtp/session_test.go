package mtp_test

import (
	"bytes"
	"context"
	"math/rand"
	"testing"
	"time"

	. "github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/mtp/mtptest"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

const internal = 0x00010001

const chunkSize = 1 << 20 // mirrors mtp.chunkSize

func openFake(t *testing.T) (*mtptest.Fake, *Session) {
	t.Helper()
	f := mtptest.New(t)
	s, err := Open(f, f.MPS)
	if err != nil {
		t.Fatal(err)
	}
	if s.Info.Model != "Phone" {
		t.Fatalf("model %q", s.Info.Model)
	}
	return f, s
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(b)
	return b
}

func TestStorages(t *testing.T) {
	_, s := openFake(t)
	ctx := context.Background()
	ids, err := s.StorageIDs(ctx)
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids %v %v", ids, err)
	}
	si, err := s.StorageInfo(ctx, ids[1])
	if err != nil || si.Description != "SD card" {
		t.Fatalf("%+v %v", si, err)
	}
}

func TestUploadDownloadSizes(t *testing.T) {
	_, s := openFake(t)
	ctx := context.Background()
	dir, err := s.Mkdir(ctx, internal, ptp.HandleRoot, "Download")
	if err != nil {
		t.Fatal(err)
	}
	sizes := []int{
		0, 1, 500, 512 - ptp.HeaderSize, // container exactly one packet -> ZLP
		3*512 - ptp.HeaderSize, chunkSize - ptp.HeaderSize, // exactly one chunk -> ZLP
		chunkSize, 3*chunkSize + 777,
	}
	for _, n := range sizes {
		data := randBytes(n)
		h, err := s.Upload(ctx, internal, dir, "f.bin", int64(n), time.Now(), bytes.NewReader(data))
		if err != nil {
			t.Fatalf("upload %d: %v", n, err)
		}
		var got bytes.Buffer
		if err := s.Download(ctx, h, &got); err != nil {
			t.Fatalf("download %d: %v", n, err)
		}
		if !bytes.Equal(got.Bytes(), data) {
			t.Fatalf("size %d: content mismatch (got %d bytes)", n, got.Len())
		}
	}
}

func TestDownloadUnknownLength(t *testing.T) {
	f, s := openFake(t)
	ctx := context.Background()
	for _, n := range []int{100, chunkSize - ptp.HeaderSize, 2*chunkSize + 5} {
		data := randBytes(n)
		h, err := s.Upload(ctx, internal, ptp.HandleRoot, "big.bin", int64(n), time.Now(), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		f.UnknownLen = true
		var got bytes.Buffer
		if err := s.Download(ctx, h, &got); err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		f.UnknownLen = false
		if !bytes.Equal(got.Bytes(), data) {
			t.Fatalf("%d: mismatch, got %d bytes", n, got.Len())
		}
	}
}

func TestListRenameMoveDelete(t *testing.T) {
	_, s := openFake(t)
	ctx := context.Background()
	a, _ := s.Mkdir(ctx, internal, ptp.HandleRoot, "A")
	b, _ := s.Mkdir(ctx, internal, ptp.HandleRoot, "B")
	fh, err := s.Upload(ctx, internal, a, "x.txt", 3, time.Now(), bytes.NewReader([]byte("abc")))
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.List(ctx, internal, ptp.HandleRoot)
	if err != nil || len(root) != 2 || !root[0].IsDir || root[0].Name != "A" {
		t.Fatalf("root %+v %v", root, err)
	}
	if err := s.Rename(ctx, fh, "y.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(ctx, fh, internal, b); err != nil {
		t.Fatal(err)
	}
	inB, _ := s.List(ctx, internal, b)
	if len(inB) != 1 || inB[0].Name != "y.txt" || inB[0].Size != 3 {
		t.Fatalf("B %+v", inB)
	}
	if err := s.Delete(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Object(ctx, fh); !IsResp(err, ptp.RespInvalidObjectHandle) {
		t.Fatalf("expected InvalidObjectHandle, got %v", err)
	}
	// The session must still work after an error response.
	if root, _ := s.List(ctx, internal, ptp.HandleRoot); len(root) != 1 {
		t.Fatalf("root after delete %+v", root)
	}
}

func TestReadAt(t *testing.T) {
	_, s := openFake(t)
	ctx := context.Background()
	data := randBytes(10000)
	h, _ := s.Upload(ctx, internal, ptp.HandleRoot, "r.bin", int64(len(data)), time.Now(), bytes.NewReader(data))
	p := make([]byte, 1000)
	n, err := s.ReadAt(ctx, h, p, 4000)
	if err != nil || n != 1000 || !bytes.Equal(p, data[4000:5000]) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, _ = s.ReadAt(ctx, h, p, 9500)
	if n != 500 || !bytes.Equal(p[:n], data[9500:]) {
		t.Fatalf("tail n=%d", n)
	}
}

func TestCancelledUploadBreaksSession(t *testing.T) {
	_, s := openFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Upload(ctx, internal, ptp.HandleRoot, "c.bin", 3*chunkSize, time.Now(), bytes.NewReader(randBytes(3*chunkSize)))
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := s.StorageIDs(context.Background()); err == nil {
		t.Fatal("session should be marked unusable after an aborted transfer")
	}
}

func TestEditWrite(t *testing.T) {
	_, s := openFake(t)
	ctx := context.Background()
	if !s.SupportsEdit() {
		t.Fatal("fake should support edit ops")
	}
	h, err := s.Upload(ctx, internal, ptp.HandleRoot, "e.bin", 0, time.Now(), bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WritePartial(ctx, h, 0, []byte("x")); err == nil {
		t.Fatal("writing without BeginEdit must fail")
	}
	data := randBytes(3*chunkSize + 11)
	if err := s.BeginEdit(ctx, h); err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(data); off += chunkSize {
		end := min(off+chunkSize, len(data))
		if err := s.WritePartial(ctx, h, int64(off), data[off:end]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Truncate(ctx, h, int64(len(data)-1)); err != nil {
		t.Fatal(err)
	}
	if err := s.EndEdit(ctx, h); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := s.Download(ctx, h, &got); err != nil || !bytes.Equal(got.Bytes(), data[:len(data)-1]) {
		t.Fatalf("content mismatch (%d bytes) %v", got.Len(), err)
	}
}
