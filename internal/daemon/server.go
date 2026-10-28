package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/bufpool"
	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
	"github.com/andymartinezot/avocadomtp/internal/volnames"
)

// Phone is what the server needs from a connected device (*device.Device).
type Phone interface {
	remote.Remote
	Object(ctx context.Context, h uint32) (*mtp.Object, error)
	Err() error
	Stats() mtp.Stats
}

// Server owns the phone and serves clients.
type Server struct {
	Open func() (Phone, error) // connects to the phone
	Log  *log.Logger
	// Changes, if set, signals USB devices arriving or leaving (hotplug).
	// The server then checks for the phone only when something changes;
	// without it, it polls every Poll interval (default 3s).
	Changes <-chan struct{}
	Poll    time.Duration
	kick    chan struct{}
	// OnConnect and OnDisconnect run (one at a time, in order) when the phone
	// comes and goes, e.g. to mount and unmount Finder volumes.
	OnConnect    func()
	OnDisconnect func()
	// Hub receives the phone state and serves "watch" streams (may be nil).
	Hub *activity.Hub
	// Eject and Remount implement the "eject" and "mount" operations (may be nil).
	Eject   func(storageID uint32) error
	Remount func() error
	// Renamed remounts a storage after its name changed (may be nil).
	Renamed func(storageID uint32) error
	events  chan bool

	renameMu sync.Mutex   // one volume rename at a time
	mu       sync.RWMutex // write-locked only to swap the phone
	phone    Phone
	closing  bool
	lastErr  error
	lastTry  time.Time
	activity time.Time
	actMu    sync.Mutex
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	}
}

// Listen creates the Unix socket at path, refusing to start if another daemon answers there.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("another avocadomtp daemon is already running at %s", path)
	}
	_ = os.Remove(path) // stale socket from a crashed daemon
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

// Serve accepts clients until ctx is cancelled, then releases the phone.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s.Poll == 0 {
		s.Poll = 3 * time.Second
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	s.events = make(chan bool, 64)
	s.kick = make(chan struct{}, 1)
	go s.watch(ctx)
	go s.runEvents(ctx)
	defer func() {
		// Unmount before letting go of the phone, then refuse to reconnect so
		// requests still in flight fail instead of reopening the device.
		if s.OnDisconnect != nil {
			s.OnDisconnect()
		}
		s.mu.Lock()
		s.closing = true
		s.mu.Unlock()
		s.drop(nil)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) emit(connected bool) {
	if s.events != nil {
		select {
		case s.events <- connected:
		default:
		}
	}
}

func (s *Server) runEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case up := <-s.events:
			if up && s.OnConnect != nil {
				s.OnConnect()
			} else if !up && s.OnDisconnect != nil {
				s.OnDisconnect()
			}
		}
	}
}

// watch connects to the phone when it appears and notices when it goes away.
func (s *Server) watch(ctx context.Context) {
	if s.Changes == nil {
		s.pollLoop(ctx)
		return
	}
	// Event driven: no USB traffic while nothing changes, so the phone's MTP
	// service can sleep. A slow safety check covers missed events.
	safety := time.NewTicker(time.Minute)
	defer safety.Stop()
	s.check(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.Changes:
			time.Sleep(700 * time.Millisecond) // let the device finish enumerating
			drain(s.Changes)
		case <-s.kick:
		case <-safety.C:
			s.mu.RLock()
			connected := s.phone != nil
			s.mu.RUnlock()
			if connected {
				continue // in-flight requests and hotplug events notice failures
			}
		}
		s.check(ctx)
	}
}

func drain(c <-chan struct{}) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

// pollLoop is the fallback when hotplug notifications are unavailable.
func (s *Server) pollLoop(ctx context.Context) {
	t := time.NewTicker(s.Poll)
	defer t.Stop()
	for {
		if s.idle(s.Poll) {
			s.check(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// check connects to a phone that appeared, or drops one that went away.
func (s *Server) check(ctx context.Context) {
	p, release, err := s.acquire()
	if err != nil {
		return
	}
	_, perr := p.StorageIDs(ctx)
	release()
	if perr != nil && p.Err() != nil {
		s.drop(p)
	}
}

func (s *Server) touch() {
	s.actMu.Lock()
	s.activity = time.Now()
	s.actMu.Unlock()
}

func (s *Server) idle(d time.Duration) bool {
	s.actMu.Lock()
	defer s.actMu.Unlock()
	return time.Since(s.activity) >= d
}

// acquire returns the connected phone (connecting if needed). The caller must
// call release when done; while held, the phone is not swapped out.
func (s *Server) acquire() (Phone, func(), error) {
	s.mu.RLock()
	if s.phone != nil {
		return s.phone, s.mu.RUnlock, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, nil, errors.New("avocadomtp daemon is shutting down")
	}
	if s.phone == nil {
		if time.Since(s.lastTry) < time.Second && s.lastErr != nil {
			err := s.lastErr
			s.mu.Unlock()
			return nil, nil, err
		}
		s.lastTry = time.Now()
		p, err := s.Open()
		if err != nil {
			if s.lastErr == nil || err.Error() != s.lastErr.Error() {
				s.logf("phone not available: %v", firstLine(err))
				s.Hub.SetPhone(false, nil, firstLine(err), errors.Is(err, device.ErrNotResponding))
			}
			s.lastErr = err
			s.mu.Unlock()
			return nil, nil, err
		}
		s.phone, s.lastErr = p, nil
		d := p.Device()
		s.logf("connected: %s %s (serial %s)", d.Manufacturer, d.Model, d.Serial)
		s.Hub.SetPhone(true, &d, "", false)
		s.emit(true)
	}
	s.mu.Unlock()
	return s.acquire()
}

// drop closes p (or whatever phone is connected, if p is nil).
func (s *Server) drop(p Phone) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phone == nil || (p != nil && s.phone != p) {
		return
	}
	if err := s.phone.Err(); err != nil {
		s.logf("disconnected: %v", err)
	} else {
		s.logf("released phone")
	}
	s.phone.Close()
	s.phone = nil
	s.Hub.SetPhone(false, nil, "", false)
	s.emit(false)
	if !s.closing && s.kick != nil {
		go func() {
			time.Sleep(2 * time.Second)
			select {
			case s.kick <- struct{}{}:
			default:
			}
		}()
	}
}

func firstLine(err error) string {
	msg := err.Error()
	for i, r := range msg {
		if r == '\n' {
			return msg[:i]
		}
	}
	return msg
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 256<<10)
	w := bufio.NewWriterSize(conn, 256<<10)
	for {
		var req Request
		if err := readJSON(r, &req); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.logf("client: %v", err)
			}
			return
		}
		if err := s.handle(ctx, &req, r, w); err != nil {
			return // connection-level failure: the stream is out of sync
		}
	}
}

// handle serves one request. A returned error means the connection must be closed.
func (s *Server) handle(ctx context.Context, req *Request, r *bufio.Reader, w *bufio.Writer) error {
	s.touch()
	defer s.touch()

	if req.Op == OpWatch {
		return s.watchStream(ctx, r, w)
	}
	var upload *frameReader
	if req.Op == OpUpload {
		upload = &frameReader{r: r}
	}

	p, release, err := s.acquire()
	if err != nil {
		if req.Op == OpStatus {
			return writeJSON(w, Response{OK: true, Connected: false, Error: firstLine(err)})
		}
		if upload != nil {
			if derr := upload.drain(); derr != nil {
				return derr
			}
		}
		return writeJSON(w, errResponse(err))
	}
	resp, connErr := s.dispatch(ctx, p, req, w, upload)
	release()
	if p.Err() != nil {
		s.drop(p) // USB error or aborted transfer: reconnect on the next request
	}
	if connErr != nil {
		return connErr
	}
	if resp != nil {
		return writeJSON(w, *resp)
	}
	return nil
}

// dispatch runs an operation. It returns the response to send (nil if it was
// already written) or a connection-level error.
func (s *Server) dispatch(ctx context.Context, p Phone, req *Request, w *bufio.Writer, upload *frameReader) (*Response, error) {
	fail := func(err error) (*Response, error) {
		r := errResponse(err)
		return &r, nil
	}
	ok := &Response{OK: true}

	switch req.Op {
	case OpStatus:
		storages, err := storagesOf(ctx, p)
		if err != nil {
			return fail(err)
		}
		d := p.Device()
		st := p.Stats()
		return &Response{OK: true, Connected: true, Device: &d, Storages: storages,
			Stats:  &Stats{BytesIn: st.BytesIn, BytesOut: st.BytesOut, Ops: st.Ops},
			Mounts: s.Hub.Snapshot().Mounts}, nil

	case OpEject:
		if s.Eject == nil {
			return fail(&Error{Code: "ENOTSUP", Msg: "this daemon doesn't mount volumes"})
		}
		if err := s.Eject(req.Storage); err != nil {
			return fail(err)
		}
		return ok, nil

	case OpMount:
		if s.Remount == nil {
			return fail(&Error{Code: "ENOTSUP", Msg: "this daemon doesn't mount volumes"})
		}
		if err := s.Remount(); err != nil {
			return fail(err)
		}
		return ok, nil

	case OpRenameVolume:
		if err := s.renameVolume(ctx, p, req.Storage, req.Name); err != nil {
			return fail(err)
		}
		return ok, nil

	case OpList:
		objs, err := p.List(ctx, req.Storage, req.Handle)
		if err != nil {
			return fail(err)
		}
		ok.Entries = make([]Entry, len(objs))
		for i, o := range objs {
			ok.Entries[i] = entryOf(o)
		}
		return ok, nil

	case OpStat:
		o, err := p.Object(ctx, req.Handle)
		if err != nil {
			return fail(err)
		}
		e := entryOf(o)
		ok.Entry = &e
		return ok, nil

	case OpRead:
		if req.Length < 0 || req.Length > MaxRead || req.Offset < 0 {
			return fail(&Error{Code: "EINVAL", Msg: fmt.Sprintf("read length must be 0..%d", MaxRead)})
		}
		buf := bufpool.Get(req.Length)
		defer bufpool.Put(buf)
		n, err := p.ReadAt(ctx, req.Handle, buf, req.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return fail(err)
		}
		if err := writeJSON(w, Response{OK: true, N: n}); err != nil {
			return nil, err
		}
		if err := writeFrame(w, buf[:n]); err != nil {
			return nil, err
		}
		return nil, w.Flush()

	case OpDownload:
		if err := writeJSON(w, *ok); err != nil {
			return nil, err
		}
		var xfer *activity.Transfer
		if o, err := p.Object(ctx, req.Handle); err == nil {
			// Fetched again from the device, so not covered by the listing's checks.
			name := o.Name
			if !vfs.ValidName(name) {
				name = vfs.Printable(name)
			}
			xfer = s.Hub.Begin(name, activity.FromPhone, int64(o.Size))
		}
		fw := frameWriter{w}
		var werr error
		err := download(ctx, p, req.Handle, writerFunc(func(b []byte) (int, error) {
			n, err := fw.Write(b)
			xfer.Add(n)
			if err != nil {
				werr = err
			}
			return n, err
		}))
		xfer.End(errors.Join(err, werr))
		if werr != nil {
			return nil, werr // client went away
		}
		if err := fw.end(); err != nil {
			return nil, err
		}
		if err != nil {
			return fail(err)
		}
		return ok, nil

	case OpUpload:
		if req.Size < 0 {
			return nil, fmt.Errorf("upload: negative size %d", req.Size)
		}
		xfer := s.Hub.Begin(req.Name, activity.ToPhone, req.Size)
		h, err := uploadChunked(ctx, p, req, countReader{upload, xfer})
		xfer.End(err)
		if derr := upload.drain(); derr != nil {
			return nil, derr
		}
		if err != nil {
			return fail(err)
		}
		ok.Handle = h
		return ok, nil

	case OpMkdir:
		h, err := p.Mkdir(ctx, req.Storage, req.Parent, req.Name)
		if err != nil {
			return fail(err)
		}
		ok.Handle = h
		return ok, nil

	case OpDelete:
		if err := p.Delete(ctx, req.Handle); err != nil {
			return fail(err)
		}
		return ok, nil

	case OpRename:
		if err := p.Rename(ctx, req.Handle, req.Name); err != nil {
			return fail(err)
		}
		return ok, nil

	case OpMove:
		if err := p.Move(ctx, req.Handle, req.Storage, req.Parent); err != nil {
			return fail(err)
		}
		return ok, nil
	}
	return fail(&Error{Code: "EINVAL", Msg: fmt.Sprintf("unknown operation %q", req.Op)})
}

// transferChunk is the size of each partial read or write. Other clients'
// requests run between chunks, so a long copy does not freeze browsing
// (~0.2 s per chunk at USB 2 speed).
const transferChunk = 8 << 20

// download streams an object in chunks with GetPartialObject64, falling back
// to a single GetObject transaction on devices without the Android extension.
func download(ctx context.Context, p Phone, h uint32, w io.Writer) error {
	buf := bufpool.Get(transferChunk)
	defer bufpool.Put(buf)
	var off int64
	for {
		n, err := p.ReadAt(ctx, h, buf, off)
		if off == 0 && mtp.IsResp(err, ptp.RespOperationNotSupported) {
			return p.Download(ctx, h, w)
		}
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			off += int64(n)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// editor is implemented by sessions that support Android's edit operations.
type editor interface {
	SupportsEdit() bool
	BeginEdit(ctx context.Context, h uint32) error
	WritePartial(ctx context.Context, h uint32, off int64, p []byte) error
	EndEdit(ctx context.Context, h uint32) error
}

// uploadChunked creates an empty file and fills it with partial writes so other
// requests can run in between. Small files, and devices without the Android
// edit extension, use a single SendObject transaction.
func uploadChunked(ctx context.Context, p Phone, req *Request, r io.Reader) (uint32, error) {
	mtime := time.Unix(req.MTime, 0)
	ed, ok := p.(editor)
	if !ok || !ed.SupportsEdit() || req.Size <= transferChunk {
		return p.Upload(ctx, req.Storage, req.Parent, req.Name, req.Size, mtime, r)
	}
	h, err := p.Upload(ctx, req.Storage, req.Parent, req.Name, 0, mtime, bytes.NewReader(nil))
	if err != nil {
		return 0, err
	}
	if err := ed.BeginEdit(ctx, h); err != nil {
		var re *mtp.RespError
		if !errors.As(err, &re) {
			return 0, err
		}
		_ = p.Delete(ctx, h) // the stream is untouched: fall back to one transaction
		return p.Upload(ctx, req.Storage, req.Parent, req.Name, req.Size, mtime, r)
	}
	abort := func(err error) (uint32, error) {
		if p.Err() == nil {
			_ = ed.EndEdit(context.Background(), h)
			_ = p.Delete(context.Background(), h) // don't leave a truncated file behind
		}
		return 0, err
	}
	buf := bufpool.Get(transferChunk)
	defer bufpool.Put(buf)
	for off := int64(0); off < req.Size; {
		n, err := io.ReadFull(r, buf[:min(int64(len(buf)), req.Size-off)])
		if err != nil {
			return abort(fmt.Errorf("read upload data: %w", err))
		}
		if err := ed.WritePartial(ctx, h, off, buf[:n]); err != nil {
			return abort(err)
		}
		off += int64(n)
	}
	if err := ed.EndEdit(ctx, h); err != nil {
		return abort(err)
	}
	return h, nil
}

// renameVolume gives a storage a name of the user's choice (name "" goes back
// to the default) and remounts it under that name. The name is stored on the
// Mac only. It's refused while a transfer runs on the volume, and undone if
// the volume can't be remounted (e.g. files are open on it).
func (s *Server) renameVolume(ctx context.Context, p Phone, id uint32, name string) error {
	if name != "" {
		if err := vfs.CheckVolumeName(name); err != nil {
			return withErrno(err, syscall.EINVAL)
		}
	}
	s.renameMu.Lock()
	defer s.renameMu.Unlock()
	before := remote.NewFS(p)
	sts, err := before.Storages(ctx)
	if err != nil {
		return err
	}
	var target *vfs.Storage
	for _, st := range sts {
		if st.ID == id {
			target = st
		}
	}
	if target == nil {
		return withErrno(fmt.Errorf("no storage %08X on the phone", id), syscall.ENOENT)
	}
	// The new name must leave every other volume's name as it is: one that
	// changed would still be mounted under its old name.
	after := remote.NewFS(p)
	if after.Custom == nil {
		after.Custom = map[uint32]string{}
	}
	after.Custom[id] = name
	renamed, err := after.Storages(ctx)
	if err != nil {
		return err
	}
	if err := checkRename(sts, renamed, id, name); err != nil {
		return err
	}
	for _, t := range s.Hub.Snapshot().Transfers {
		if t.State == "active" && (t.Path == target.Name || strings.HasPrefix(t.Path, target.Name+"/")) {
			return withErrno(fmt.Errorf("%s is busy copying files; try again when it's done", target.Name), syscall.EBUSY)
		}
	}
	d := p.Device()
	key := volnames.Key(d.Serial, d.Model)
	old := volnames.Load(key)[id]
	if old == name {
		return nil
	}
	if err := volnames.Set(key, id, name); err != nil {
		return err
	}
	if s.Renamed != nil {
		if err := s.Renamed(id); err != nil {
			_ = volnames.Set(key, id, old)
			if !errors.Is(err, syscall.EBUSY) {
				_ = s.Renamed(id) // unmounted but not mounted again: back under the old name
			}
			return err
		}
	}
	if name == "" {
		s.logf("volume %s: back to the phone's name", vfs.Printable(target.Name))
	} else {
		s.logf("renamed volume %s to %s", vfs.Printable(target.Name), vfs.Printable(name))
	}
	return nil
}

// checkRename compares the volume names before and after giving storage id
// the name (by storage ID: the phone answered twice and may have changed).
// Every other volume must keep its name, and the target must get exactly the
// name asked for; a storage that came or went means "try again".
func checkRename(before, after []*vfs.Storage, id uint32, name string) error {
	prev := make(map[uint32]string, len(before))
	for _, st := range before {
		prev[st.ID] = st.Name
	}
	if len(after) != len(before) {
		return withErrno(errors.New("the phone's storages changed; try again"), syscall.EBUSY)
	}
	for _, st := range after {
		old, ok := prev[st.ID]
		switch {
		case !ok:
			return withErrno(errors.New("the phone's storages changed; try again"), syscall.EBUSY)
		case st.ID != id && st.Name != old, st.ID == id && name != "" && st.Name != name:
			return withErrno(fmt.Errorf("another volume is called that (%s)", clash(before, id, st.Name, name)), syscall.EEXIST)
		}
	}
	return nil
}

// clash names the volume a rename collides with, for the error message.
func clash(sts []*vfs.Storage, id uint32, got, want string) string {
	for _, st := range sts {
		if st.ID != id && (vfs.SameName(st.Name, want) || vfs.SameName(st.Name, got)) {
			return st.Name
		}
	}
	return got
}

func storagesOf(ctx context.Context, p Phone) ([]Storage, error) {
	sts, err := remote.NewFS(p).Storages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Storage, len(sts))
	for i, st := range sts {
		out[i] = Storage{
			ID: st.ID, Name: st.Name, Description: st.Info.Description, VolumeLabel: st.Info.VolumeLabel, Type: st.Info.StorageType,
			Capacity: st.Info.MaxCapacity, Free: st.Info.FreeSpaceInBytes, ReadOnly: st.Info.AccessCapability != 0,
		}
	}
	return out, nil
}

func entryOf(o *mtp.Object) Entry {
	e := Entry{Handle: o.Handle, Storage: o.StorageID, Parent: o.Parent, Name: o.Name, Dir: o.IsDir, Size: o.Size}
	if !o.Modified.IsZero() {
		e.MTime = o.Modified.Unix()
	}
	return e
}

// watchStream answers a "watch" request with a stream of activity snapshots
// until the client disconnects.
func (s *Server) watchStream(ctx context.Context, r *bufio.Reader, w *bufio.Writer) error {
	if err := writeJSON(w, Response{OK: true}); err != nil {
		return err
	}
	snaps, cancel := s.Hub.Subscribe()
	defer cancel()
	gone := make(chan struct{})
	go func() { // the client sends nothing more; EOF means it went away
		_, _ = r.ReadByte()
		close(gone)
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gone:
			return io.EOF
		case st := <-snaps:
			if err := writeJSON(w, st); err != nil {
				return err
			}
		}
	}
}

// countReader reports bytes read to a transfer.
type countReader struct {
	r io.Reader
	t *activity.Transfer
}

func (c countReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.t.Add(n)
	return n, err
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
