package main

import (
	"context"
	"errors"
	"io/fs"
	"path"

	"github.com/andymartinezot/avocadomtp/internal/daemon"
	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

// client is a connected phone, reached through the daemon or directly over USB.
type client struct {
	r   remote.Remote
	fs  *vfs.FS
	via string
}

// connect prefers a running daemon (which owns the USB connection) and falls
// back to opening the phone directly.
func connect(serial string, direct bool) (*client, error) {
	if !direct {
		dc, err := daemon.Dial(daemon.DefaultSocketPath())
		if err == nil {
			st, err := dc.Status(context.Background())
			if err != nil {
				dc.Close()
				return nil, err
			}
			if !st.Connected {
				dc.Close()
				return nil, errors.New("the avocadomtp daemon is running but has no phone (" + st.Error + ")")
			}
			return newClient(dc, "daemon"), nil
		}
		if !errors.Is(err, daemon.ErrNotRunning) {
			return nil, err
		}
	}
	d, err := device.Open(serial)
	if err != nil {
		return nil, err
	}
	return newClient(d, "USB (direct)"), nil
}

func newClient(r remote.Remote, via string) *client {
	v := vfs.New(r)
	v.DeviceName = r.Device().Model
	return &client{r: r, fs: v, via: via}
}

func (c *client) close() {
	if c.r != nil {
		c.r.Close()
		c.r = nil
	}
}

// healthy reconnects if the session became unusable after an aborted transfer.
func (c *client) healthy() error {
	if err := c.r.Healthy(); err != nil {
		return err
	}
	return nil
}

// parentOf resolves the directory containing p.
func (c *client) parentOf(ctx context.Context, p string) (*vfs.Entry, error) {
	return c.fs.Stat(ctx, path.Dir(vfs.Clean(p)))
}

func (c *client) invalidateParent(ctx context.Context, p string) {
	if par, err := c.parentOf(ctx, p); err == nil {
		c.fs.Invalidate(par.StorageID, par.Handle)
	}
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
