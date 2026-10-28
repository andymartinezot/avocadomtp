package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
)

// ErrNotRunning is returned by Dial when no daemon listens on the socket.
var ErrNotRunning = errors.New("avocadomtp daemon is not running")

// Client talks to the daemon. It implements remote.Remote.
type Client struct {
	path string

	mu       sync.Mutex
	conn     net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	broken   error
	status   Response
	statusAt time.Time
}

var _ remote.Remote = (*Client)(nil)

// Dial connects to the daemon. It fails with ErrNotRunning if nothing listens.
func Dial(path string) (*Client, error) {
	c := &Client{path: path}
	if err := c.dial(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) dial() error {
	conn, err := net.DialTimeout("unix", c.path, 2*time.Second)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrNotRunning, err)
	}
	c.conn, c.broken = conn, nil
	c.r = bufio.NewReaderSize(conn, 256<<10)
	c.w = bufio.NewWriterSize(conn, 256<<10)
	return nil
}

// Status asks the daemon which phone is connected and its storages.
func (c *Client) Status(ctx context.Context) (*Response, error) {
	var resp Response
	if err := c.call(ctx, &Request{Op: OpStatus}, &resp, nil); err != nil {
		return nil, err
	}
	c.status, c.statusAt = resp, time.Now()
	return &resp, nil
}

// call sends req and reads one JSON response. If body is set, it runs after
// the response with the connection still locked (for streamed data).
func (c *Client) call(ctx context.Context, req *Request, resp *Response, body func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken != nil {
		return c.broken
	}
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()) })
	defer stop()

	err := writeJSON(c.w, req)
	if err == nil && req.Op == OpUpload && body != nil {
		err = body() // stream the file before reading the response
		body = nil
	}
	if err == nil {
		err = readJSON(c.r, resp)
	}
	if err == nil && resp.OK && body != nil {
		err = body()
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.broken = fmt.Errorf("daemon connection lost: %w", err)
		c.conn.Close()
		return err
	}
	return resp.err()
}

func (c *Client) cachedStatus(ctx context.Context) (*Response, error) {
	if time.Since(c.statusAt) < 2*time.Second && c.status.Connected {
		return &c.status, nil
	}
	resp, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !resp.Connected {
		return nil, &Error{Code: "ENODEV", Msg: "the avocadomtp daemon has no phone connected: " + resp.Error}
	}
	return resp, nil
}

func (c *Client) Device() remote.DeviceInfo {
	if c.status.Device != nil {
		return *c.status.Device
	}
	return remote.DeviceInfo{}
}

func (c *Client) StorageIDs(ctx context.Context) ([]uint32, error) {
	st, err := c.cachedStatus(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uint32, len(st.Storages))
	for i, s := range st.Storages {
		ids[i] = s.ID
	}
	return ids, nil
}

func (c *Client) StorageInfo(ctx context.Context, id uint32) (*ptp.StorageInfo, error) {
	st, err := c.cachedStatus(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range st.Storages {
		if s.ID == id {
			info := &ptp.StorageInfo{StorageType: s.Type, MaxCapacity: s.Capacity, FreeSpaceInBytes: s.Free,
				Description: s.Description, VolumeLabel: s.VolumeLabel}
			if s.ReadOnly {
				info.AccessCapability = 1
			}
			return info, nil
		}
	}
	return nil, &Error{Code: "ENODEV", Msg: fmt.Sprintf("storage %08X not found", id)}
}

func (c *Client) List(ctx context.Context, storage, parent uint32) ([]*mtp.Object, error) {
	var resp Response
	if err := c.call(ctx, &Request{Op: OpList, Storage: storage, Handle: parent}, &resp, nil); err != nil {
		return nil, err
	}
	out := make([]*mtp.Object, len(resp.Entries))
	for i, e := range resp.Entries {
		o := &mtp.Object{Handle: e.Handle, StorageID: e.Storage, Parent: e.Parent, Name: e.Name, Size: e.Size, IsDir: e.Dir}
		if e.MTime != 0 {
			o.Modified = time.Unix(e.MTime, 0)
		}
		out[i] = o
	}
	return out, nil
}

func (c *Client) Download(ctx context.Context, h uint32, w io.Writer) error {
	var resp Response
	var werr error
	err := c.call(ctx, &Request{Op: OpDownload, Handle: h}, &resp, func() error {
		fr := &frameReader{r: c.r}
		if _, err := io.Copy(writerFunc(func(b []byte) (int, error) {
			n, err := w.Write(b)
			if err != nil {
				werr = err
			}
			return n, err
		}), fr); err != nil {
			return err
		}
		var final Response
		if err := readJSON(c.r, &final); err != nil {
			return err
		}
		resp = final
		return nil
	})
	if werr != nil {
		return werr
	}
	if err != nil {
		return err
	}
	return resp.err()
}

func (c *Client) ReadAt(ctx context.Context, h uint32, p []byte, off int64) (int, error) {
	var resp Response
	n := 0
	err := c.call(ctx, &Request{Op: OpRead, Handle: h, Offset: off, Length: len(p)}, &resp, func() error {
		b, err := readFrame(c.r, nil)
		n = copy(p, b)
		return err
	})
	if err == nil && n < len(p) {
		err = io.EOF
	}
	return n, err
}

func (c *Client) Upload(ctx context.Context, storage, parent uint32, name string, size int64, mtime time.Time, r io.Reader) (uint32, error) {
	var resp Response
	req := &Request{Op: OpUpload, Storage: storage, Parent: parent, Name: name, Size: size, MTime: mtime.Unix()}
	err := c.call(ctx, req, &resp, func() error {
		fw := frameWriter{c.w}
		if _, err := io.CopyN(fw, r, size); err != nil {
			return fmt.Errorf("read local data: %w", err)
		}
		return fw.end()
	})
	return resp.Handle, err
}

func (c *Client) simple(ctx context.Context, req *Request) (*Response, error) {
	var resp Response
	err := c.call(ctx, req, &resp, nil)
	return &resp, err
}

func (c *Client) Mkdir(ctx context.Context, storage, parent uint32, name string) (uint32, error) {
	resp, err := c.simple(ctx, &Request{Op: OpMkdir, Storage: storage, Parent: parent, Name: name})
	return resp.Handle, err
}

func (c *Client) Delete(ctx context.Context, h uint32) error {
	_, err := c.simple(ctx, &Request{Op: OpDelete, Handle: h})
	return err
}

func (c *Client) Rename(ctx context.Context, h uint32, name string) error {
	_, err := c.simple(ctx, &Request{Op: OpRename, Handle: h, Name: name})
	return err
}

func (c *Client) Move(ctx context.Context, h, storage, parent uint32) error {
	_, err := c.simple(ctx, &Request{Op: OpMove, Handle: h, Storage: storage, Parent: parent})
	return err
}

// Healthy reconnects after a lost connection (e.g. a cancelled transfer).
func (c *Client) Healthy() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken == nil {
		return nil
	}
	return c.dial()
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
}

// Eject unmounts one storage until Remount or the next reconnect.
func (c *Client) Eject(ctx context.Context, storage uint32) error {
	_, err := c.simple(ctx, &Request{Op: OpEject, Storage: storage})
	return err
}

// RenameVolume names a storage's volume; an empty name restores the default.
func (c *Client) RenameVolume(ctx context.Context, storage uint32, name string) error {
	_, err := c.simple(ctx, &Request{Op: OpRenameVolume, Storage: storage, Name: name})
	return err
}

// Remount mounts every storage again, including ejected ones.
func (c *Client) Remount(ctx context.Context) error {
	_, err := c.simple(ctx, &Request{Op: OpMount})
	return err
}

// Watch streams activity snapshots to fn until ctx is cancelled or the daemon
// goes away. It uses its own connection.
func Watch(ctx context.Context, path string, fn func(activity.State)) error {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrNotRunning, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
	if err := writeJSON(w, &Request{Op: OpWatch}); err != nil {
		return err
	}
	var resp Response
	if err := readJSON(r, &resp); err != nil {
		return err
	}
	if err := resp.err(); err != nil {
		return err
	}
	for {
		var st activity.State
		if err := readJSON(r, &st); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		fn(st)
	}
}
