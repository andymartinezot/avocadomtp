package daemon

import (
	"bytes"
	"context"
	"errors"
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
	opens   int
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
	srv := &Server{Poll: 50 * time.Millisecond, Changes: changes, Hub: activity.NewHub(), Open: func() (Phone, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
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
