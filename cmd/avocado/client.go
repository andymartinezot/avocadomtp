package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/usbx"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

const noDeviceHelp = `no MTP device found.
  • Unlock the phone and connect it with a data-capable USB cable.
  • In the "USB" notification on the phone choose "File transfer" (not "Charging only").
  • Quit Android File Transfer, OpenMTP or MacDroid if they are running.`

// client is a connected phone.
type client struct {
	serial string
	usb    *usbx.Context
	dev    *usbx.Device
	cand   *usbx.Candidate
	s      *mtp.Session
	fs     *vfs.FS
}

// findDevice picks the device matching serial (or the only/first one).
func findDevice(u *usbx.Context, serial string) (*usbx.Candidate, error) {
	cands, err := u.FindMTP()
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, errors.New(noDeviceHelp)
	}
	if serial == "" {
		return cands[0], nil
	}
	for _, c := range cands {
		if c.Serial == serial {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no MTP device with serial %q (see `avocado devices`)", serial)
}

func connect(serial string) (*client, error) {
	c := &client{serial: serial}
	if err := c.open(); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) open() error {
	u, err := usbx.NewContext()
	if err != nil {
		return err
	}
	c.usb = u
	cand, err := findDevice(u, c.serial)
	if err != nil {
		return err
	}
	dev, err := u.Open(cand)
	if err != nil {
		return err
	}
	c.cand, c.dev = cand, dev
	s, err := mtp.Open(dev, dev.MaxPacket)
	if err != nil {
		return fmt.Errorf("start MTP session with %s: %w (is the phone unlocked?)", cand, err)
	}
	c.s = s
	c.fs = vfs.New(s)
	return nil
}

func (c *client) close() {
	if c.s != nil {
		_ = c.s.Close()
		c.s = nil
	}
	if c.dev != nil {
		c.dev.Close()
		c.dev = nil
	}
	if c.usb != nil {
		c.usb.Close()
		c.usb = nil
	}
}

// healthy reconnects if the session became unusable after an aborted transfer.
func (c *client) healthy() error {
	if c.s != nil && c.s.Err() == nil {
		return nil
	}
	c.close()
	return c.open()
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
