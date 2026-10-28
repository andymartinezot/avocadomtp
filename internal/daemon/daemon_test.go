package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/mtp/mtptest"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

const internal = 0x00010001

// fakePhone adapts a session over the fake device to the Phone interface.
type fakePhone struct{ *mtp.Session }

func (fakePhone) Device() remote.DeviceInfo {
	return remote.DeviceInfo{Manufacturer: "Fake", Model: "Phone"}
}
func (fakePhone) Healthy() error { return nil }
func (fakePhone) Close()         {}

type harness struct {
	srv     *Server
	socket  string
	mu      sync.Mutex
	present bool
	openErr error // returned by Open while set
	opens   int
	fake    *mtptest.Fake
}

func start(t *testing.T, present bool) *harness { return startWith(t, present, nil) }

func startWith(t *testing.T, present bool, changes <-chan struct{}) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "avd") // short path: Unix sockets are limited to 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h := &harness{socket: filepath.Join(dir, "s"), present: present}
	fake := mtptest.New(t)
	h.fake = fake
	srv := &Server{Poll: 50 * time.Millisecond, Changes: changes, Hub: activity.NewHub(), Open: func() (Phone, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.openErr != nil {
			return nil, h.openErr
		}
		if !h.present {
			return nil, device.ErrNoDevice
		}
		h.opens++
		fake.Reset()
		s, err := mtp.Open(fake, fake.MPS)
		if err != nil {
			return nil, err
		}
		return fakePhone{s}, nil
	}}
	h.srv = srv
	ln, err := Listen(h.socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return h
}

func dial(t *testing.T, h *harness) *Client {
	t.Helper()
	c, err := Dial(h.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(b)
	return b
}

func TestStatusAndFileOps(t *testing.T) {
	h := start(t, true)
	c := dial(t, h)
	ctx := context.Background()

	st, err := c.Status(ctx)
	if err != nil || !st.Connected || len(st.Storages) != 2 || st.Storages[1].Name != "SD card" || st.Device.Model != "Phone" {
		t.Fatalf("status %+v %v", st, err)
	}
	if _, err := Listen(h.socket); err == nil {
		t.Fatal("a second daemon must refuse to start on the same socket")
	}

	dir, err := c.Mkdir(ctx, internal, ptp.HandleRoot, "Music")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 1, dataChunk, 3*dataChunk + 7, 2*transferChunk + 5} {
		data := randBytes(n)
		hd, err := c.Upload(ctx, internal, dir, "song.bin", int64(n), time.Unix(1_700_000_000, 0), bytes.NewReader(data))
		if err != nil {
			t.Fatalf("upload %d: %v", n, err)
		}
		var got bytes.Buffer
		if err := c.Download(ctx, hd, &got); err != nil {
			t.Fatalf("download %d: %v", n, err)
		}
		if !bytes.Equal(got.Bytes(), data) {
			t.Fatalf("size %d: mismatch (%d bytes back)", n, got.Len())
		}
		if n > 100 {
			p := make([]byte, 100)
			if k, err := c.ReadAt(ctx, hd, p, int64(n-50)); k != 50 || !bytes.Equal(p[:50], data[n-50:]) || err == nil {
				t.Fatalf("ReadAt tail: n=%d err=%v", k, err)
			}
		}
		if err := c.Delete(ctx, hd); err != nil {
			t.Fatal(err)
		}
	}

	hd, _ := c.Upload(ctx, internal, dir, "a.txt", 3, time.Now(), bytes.NewReader([]byte("abc")))
	if err := c.Rename(ctx, hd, "b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move(ctx, hd, internal, ptp.HandleRoot); err != nil {
		t.Fatal(err)
	}
	root, err := c.List(ctx, internal, ptp.HandleRoot)
	if err != nil || len(root) != 2 || root[1].Name != "b.txt" || root[1].Size != 3 || !root[0].IsDir {
		t.Fatalf("root %+v %v", root, err)
	}
	if err := c.Delete(ctx, 9999); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ENOENT, got %v", err)
	}
	// The connection must still work after an error response.
	if _, err := c.List(ctx, internal, dir); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentClients(t *testing.T) {
	h := start(t, true)
	ctx := context.Background()
	a, b := dial(t, h), dial(t, h)
	data := randBytes(5 * dataChunk)
	hd, err := a.Upload(ctx, internal, ptp.HandleRoot, "big.bin", int64(len(data)), time.Now(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			if err := a.Download(ctx, hd, &buf); err != nil || !bytes.Equal(buf.Bytes(), data) {
				errs <- errors.Join(err, errors.New("download mismatch"))
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := b.List(ctx, internal, ptp.HandleRoot); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCancelledUploadRecovers(t *testing.T) {
	h := start(t, true)
	c := dial(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	// The reader cancels the request after the first megabyte has been streamed.
	r := &cancelAfter{r: bytes.NewReader(randBytes(8 * dataChunk)), n: dataChunk, cancel: cancel}
	if _, err := c.Upload(ctx, internal, ptp.HandleRoot, "x.bin", 8*dataChunk, time.Now(), r); err == nil {
		t.Fatal("expected cancellation error")
	}
	if err := c.Healthy(); err != nil {
		t.Fatal(err)
	}
	// The daemon reconnects to the phone and serves new requests.
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := c.List(context.Background(), internal, ptp.HandleRoot)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not recover: %v", err)
		}
		c.Healthy()
		time.Sleep(50 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.opens < 2 {
		t.Fatalf("expected the daemon to reopen the session, opens=%d", h.opens)
	}
}

type cancelAfter struct {
	r      *bytes.Reader
	n      int
	read   int
	cancel func()
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	if c.read >= c.n {
		c.cancel()
		time.Sleep(20 * time.Millisecond)
	}
	n, err := c.r.Read(p)
	c.read += n
	return n, err
}

func TestNoPhoneThenPlugIn(t *testing.T) {
	h := start(t, false)
	c := dial(t, h)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || st.Connected {
		t.Fatalf("expected disconnected status, got %+v %v", st, err)
	}
	if _, err := c.List(ctx, internal, ptp.HandleRoot); !errors.Is(err, device.ErrNoDevice) {
		t.Fatalf("want ENODEV, got %v", err)
	}
	h.mu.Lock()
	h.present = true
	h.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if st, err := c.Status(ctx); err == nil && st.Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not pick up the phone")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHotplugDriven(t *testing.T) {
	changes := make(chan struct{}, 1)
	h := startWith(t, false, changes)
	c := dial(t, h)
	ctx := context.Background()
	time.Sleep(200 * time.Millisecond)
	h.mu.Lock()
	h.present = true
	opensBefore := h.opens
	h.mu.Unlock()
	// Without an event, the server must not poll for the phone.
	time.Sleep(300 * time.Millisecond)
	h.mu.Lock()
	polled := h.opens != opensBefore
	h.mu.Unlock()
	if polled {
		t.Fatal("server polled for the phone without a hotplug event")
	}
	changes <- struct{}{}
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		opened := h.opens > opensBefore
		h.mu.Unlock()
		if opened {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hotplug event did not connect the phone")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st, err := c.Status(ctx); err != nil || !st.Connected || st.Stats == nil {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestWatchStreamsTransfers(t *testing.T) {
	h := start(t, true)
	c := dial(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snaps := make(chan activity.State, 64)
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, h.socket, func(st activity.State) {
			select {
			case snaps <- st:
			default:
			}
		})
	}()
	// The stream reports the phone connecting.
	deadline := time.After(3 * time.Second)
	for connected := false; !connected; {
		select {
		case st := <-snaps:
			connected = st.Connected && st.Device != nil && st.Device.Model == "Phone"
		case <-deadline:
			t.Fatal("watch never reported the phone")
		}
	}
	// A CLI upload shows up as a transfer to the phone and finishes.
	data := randBytes(3 * transferChunk)
	if _, err := c.Upload(context.Background(), internal, ptp.HandleRoot, "big.bin", int64(len(data)), time.Now(), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	deadline = time.After(3 * time.Second)
	for {
		select {
		case st := <-snaps:
			for _, tr := range st.Transfers {
				if tr.Path == "big.bin" && tr.Direction == activity.ToPhone && tr.State == "done" && tr.Bytes == int64(len(data)) {
					cancel()
					if err := <-done; err != nil {
						t.Fatalf("watch ended with %v", err)
					}
					return
				}
			}
		case <-deadline:
			t.Fatal("upload never reported as a finished transfer")
		}
	}
}

func TestEjectAndMountOps(t *testing.T) {
	h := start(t, true)
	c := dial(t, h)
	ctx := context.Background()
	if err := c.Eject(ctx, internal); !errors.Is(err, syscall.ENOTSUP) {
		t.Fatalf("eject without mounts: %v", err)
	}
	var ejected uint32
	remounted := false
	h.srv.Eject = func(id uint32) error { ejected = id; return nil }
	h.srv.Remount = func() error { remounted = true; return nil }
	if err := c.Eject(ctx, internal); err != nil || ejected != internal {
		t.Fatalf("eject: %v (got %08X)", err, ejected)
	}
	if err := c.Remount(ctx); err != nil || !remounted {
		t.Fatalf("mount: %v", err)
	}
}

func TestRenameVolume(t *testing.T) {
	t.Setenv("AVOCADOMTP_VOLUME_NAMES", filepath.Join(t.TempDir(), "names.json"))
	h := start(t, true)
	c := dial(t, h)
	ctx := context.Background()
	var renamed []uint32
	var refuse error
	h.srv.Renamed = func(id uint32) error { renamed = append(renamed, id); return refuse }
	name := func() string {
		st, err := c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st.Storages[0].Name
	}

	if err := c.RenameVolume(ctx, internal, "Games"); err != nil || name() != "Games" || len(renamed) != 1 {
		t.Fatalf("rename: %v, name %q, remounts %v", err, name(), renamed)
	}
	for bad, code := range map[string]syscall.Errno{"sd CARD": syscall.EEXIST, "../x": syscall.EINVAL, ".x": syscall.EINVAL} {
		if err := c.RenameVolume(ctx, internal, bad); !errors.Is(err, code) {
			t.Errorf("rename to %q: %v, want %v", bad, err, code)
		}
	}
	if err := c.RenameVolume(ctx, 0x00990001, "X"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unknown storage: %v", err)
	}

	// A volume that can't be unmounted keeps its old name.
	refuse = syscall.EBUSY
	if err := c.RenameVolume(ctx, internal, "Other"); !errors.Is(err, syscall.EBUSY) || name() != "Games" {
		t.Fatalf("busy remount: %v, name %q", err, name())
	}
	refuse = nil

	// So does one with a copy running on it.
	tr := h.srv.Hub.Begin("Games/a.bin", activity.ToPhone, 10)
	if err := c.RenameVolume(ctx, internal, "Other"); !errors.Is(err, syscall.EBUSY) {
		t.Fatalf("rename during a transfer: %v", err)
	}
	tr.End(nil)

	if err := c.RenameVolume(ctx, internal, ""); err != nil || name() != "Phone" {
		t.Fatalf("reset: %v, name %q", err, name())
	}

	// A reset that would rename another volume is refused: that volume is
	// still mounted under its old name.
	if err := c.RenameVolume(ctx, internal, "Games"); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameVolume(ctx, 0x00020001, "Phone"); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameVolume(ctx, internal, ""); !errors.Is(err, syscall.EEXIST) || name() != "Games" {
		t.Fatalf("clashing reset: %v, name %q", err, name())
	}
}

func TestAsleepPhone(t *testing.T) {
	h := start(t, false) // nothing to connect to until openErr is set
	h.mu.Lock()
	h.openErr = fmt.Errorf("Phone is not responding: %w", device.ErrNotResponding)
	h.mu.Unlock()
	c := dial(t, h)
	ctx := context.Background()
	if st, _ := c.Status(ctx); st.Connected {
		t.Fatal("must not be connected")
	}
	if snap := h.srv.Hub.Snapshot(); !snap.Asleep || snap.Connected {
		t.Fatalf("snapshot %+v, want asleep", snap)
	}
	h.mu.Lock()
	h.openErr, h.present = nil, true
	h.mu.Unlock()
	time.Sleep(1100 * time.Millisecond) // past the retry back-off
	if st, err := c.Status(ctx); err != nil || !st.Connected {
		t.Fatalf("status after waking: %+v %v", st, err)
	}
	if snap := h.srv.Hub.Snapshot(); snap.Asleep || !snap.Connected {
		t.Fatalf("snapshot %+v, want connected", snap)
	}
}

// The CLI names volumes from what the daemon reports; it must agree with the
// daemon (and so with the Finder volumes) for a phone in any language.
func TestClientNamesMatchDaemon(t *testing.T) {
	h := start(t, false)
	h.fake.SetStorageDescription(internal, "Almacenamiento interno compartido")
	h.mu.Lock()
	h.present = true
	h.mu.Unlock()
	c := dial(t, h)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Storages[0].Name != "Phone" {
		t.Fatalf("daemon named the internal storage %q", st.Storages[0].Name)
	}
	sts, err := remote.NewFS(c).Storages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range sts {
		if s.Name != st.Storages[i].Name {
			t.Errorf("client name %q, daemon name %q", s.Name, st.Storages[i].Name)
		}
	}
}

func TestCheckRename(t *testing.T) {
	vols := func(names ...string) []*vfs.Storage {
		out := make([]*vfs.Storage, len(names))
		for i, n := range names {
			out[i] = &vfs.Storage{ID: uint32(i + 1), Name: n}
		}
		return out
	}
	for _, tc := range []struct {
		name          string
		before, after []*vfs.Storage
		want          error // nil, EEXIST or EBUSY
	}{
		{"rename", vols("Phone", "SD"), vols("Games", "SD"), nil},
		{"other volume renamed", vols("Phone", "SD"), vols("SD", "SD (2)"), syscall.EEXIST},
		{"target suffixed", vols("SD", "Phone"), vols("SD", "Phone (2)"), syscall.EEXIST},
		{"storage appeared", vols("Phone", "SD"), vols("Games", "SD", "USB"), syscall.EBUSY},
		{"storage replaced", vols("Phone", "SD"), []*vfs.Storage{{ID: 1, Name: "Games"}, {ID: 9, Name: "SD"}}, syscall.EBUSY},
		{"storage gone", vols("Phone", "SD"), vols("Games"), syscall.EBUSY},
	} {
		want := map[string]string{"rename": "Games", "other volume renamed": "SD", "target suffixed": "Phone"}[tc.name]
		if want == "" {
			want = "Games"
		}
		err := checkRename(tc.before, tc.after, 1, want)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestNoPhoneIsNotAnError(t *testing.T) {
	h := start(t, false)
	c := dial(t, h)
	if st, _ := c.Status(context.Background()); st.Connected {
		t.Fatal("must not be connected")
	}
	if snap := h.srv.Hub.Snapshot(); snap.Error != "" || snap.Asleep {
		t.Fatalf("snapshot %+v: no phone must carry no error", snap)
	}
}

func TestPhoneErrorIsShown(t *testing.T) {
	h := start(t, false)
	h.mu.Lock()
	h.openErr = errors.New("claim MTP interface: LIBUSB_ERROR_ACCESS")
	h.mu.Unlock()
	c := dial(t, h)
	c.Status(context.Background())
	if snap := h.srv.Hub.Snapshot(); snap.Error == "" || snap.Asleep || snap.Connected {
		t.Fatalf("snapshot %+v: a phone that can't be used must carry its error", snap)
	}
}
