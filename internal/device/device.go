// Package device opens an MTP session with a USB-connected phone.
package device

import (
	"errors"
	"fmt"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
	"github.com/andymartinezot/avocadomtp/internal/usbx"
)

// ErrNoDevice is returned when no MTP device is connected.
var ErrNoDevice = errors.New(`no MTP device found.
  • Unlock the phone and connect it with a data-capable USB cable.
  • In the "USB" notification on the phone choose "File transfer" (not "Charging only").
  • Quit Android File Transfer, OpenMTP or MacDroid if they are running.`)

// Device is an open MTP session over USB. It implements remote.Remote.
type Device struct {
	*mtp.Session
	serial string
	usb    *usbx.Context
	dev    *usbx.Device
	cand   *usbx.Candidate

	portReset bool // a port reset is in progress (avoid recursing)
}

var _ remote.Remote = (*Device)(nil)

// Open connects to the device with the given USB serial, or the first one found.
func Open(serial string) (*Device, error) {
	d := &Device{serial: serial}
	if err := d.open(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Device) open() error {
	u, err := usbx.NewContext()
	if err != nil {
		return err
	}
	d.usb = u
	cand, err := find(u, d.serial)
	if err != nil {
		return err
	}
	dev, err := u.Open(cand)
	if err != nil {
		return err
	}
	d.cand, d.dev = cand, dev
	s, err := mtp.Open(dev, dev.MaxPacket)
	if err != nil && stuck(err) {
		// The device may be stuck in an earlier transaction: abort it and retry.
		dev.ClearHalt()
		_ = dev.ResetRequest()
		time.Sleep(500 * time.Millisecond)
		s, err = mtp.Open(dev, dev.MaxPacket)
	}
	if err != nil && stuck(err) && !d.portReset {
		// Last resort: reset the USB port and start over once.
		_ = dev.PortReset()
		d.Close()
		d.portReset = true
		time.Sleep(time.Second)
		err = d.open()
		d.portReset = false
		return err
	}
	if err != nil {
		if stuck(err) {
			return &notResponding{fmt.Errorf("%s is not responding: %w (wake the phone up; if it stays stuck, unplug and reconnect it)", cand, err)}
		}
		return fmt.Errorf("start MTP session with %s: %w (is the phone unlocked?)", cand, err)
	}
	d.Session = s
	return nil
}

// ErrNotResponding matches (errors.Is) an open that failed because the phone
// is connected but doesn't answer, typically asleep after a long idle time.
var ErrNotResponding = errors.New("the phone is not responding")

type notResponding struct{ err error }

func (e *notResponding) Error() string   { return e.err.Error() }
func (e *notResponding) Unwrap() []error { return []error{e.err, ErrNotResponding} }

// stuck reports USB errors that mean the device stopped responding.
func stuck(err error) bool {
	return errors.Is(err, usbx.ErrTimeout) || errors.Is(err, usbx.ErrPipe)
}

func find(u *usbx.Context, serial string) (*usbx.Candidate, error) {
	cands, err := u.FindMTP()
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, ErrNoDevice
	}
	if serial == "" {
		return cands[0], nil
	}
	for _, c := range cands {
		if c.Serial == serial {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no MTP device with serial %q (see `avocadomtp devices`): %w", serial, ErrNoDevice)
}

// List enumerates connected MTP devices without claiming them.
func List() ([]*usbx.Candidate, error) {
	u, err := usbx.NewContext()
	if err != nil {
		return nil, err
	}
	defer u.Close()
	return u.FindMTP()
}

// Device describes the connected phone.
func (d *Device) Device() remote.DeviceInfo {
	di := d.Session.Info
	return remote.DeviceInfo{
		Manufacturer: di.Manufacturer, Model: di.Model, Version: di.DeviceVersion,
		Serial: di.SerialNumber, USB: d.cand.String(), Extension: di.VendorExtensionDesc,
	}
}

// Healthy reopens the session if it became unusable after an aborted transfer.
func (d *Device) Healthy() error {
	if d.Session != nil && d.Session.Err() == nil {
		return nil
	}
	d.Close()
	return d.open()
}

// Close ends the session and releases the USB interface.
func (d *Device) Close() {
	if d.Session != nil {
		_ = d.Session.Close()
		d.Session = nil
	}
	if d.dev != nil {
		d.dev.Close()
		d.dev = nil
	}
	if d.usb != nil {
		d.usb.Close()
		d.usb = nil
	}
}
