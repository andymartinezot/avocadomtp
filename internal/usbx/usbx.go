// Package usbx is a thin cgo wrapper around libusb that finds MTP interfaces
// and exposes the bulk/interrupt endpoints needed by the MTP transport.
package usbx

/*
#cgo CFLAGS: -I/opt/homebrew/include/libusb-1.0 -I/usr/local/include/libusb-1.0
#cgo LDFLAGS: -L/opt/homebrew/lib -L/usr/local/lib -lusb-1.0
#include <libusb.h>
#include <stdlib.h>

static const struct libusb_interface_descriptor *alt_at(const struct libusb_config_descriptor *c, int i, int a) {
	return &c->interface[i].altsetting[a];
}
static const struct libusb_endpoint_descriptor *ep_at(const struct libusb_interface_descriptor *d, int e) {
	return &d->endpoint[e];
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unsafe"
)

// Error wraps a libusb error code.
type Error int

func (e Error) Error() string { return "libusb: " + C.GoString(C.libusb_error_name(C.int(e))) }

var (
	ErrTimeout  = Error(C.LIBUSB_ERROR_TIMEOUT)
	ErrNoDevice = Error(C.LIBUSB_ERROR_NO_DEVICE)
	ErrAccess   = Error(C.LIBUSB_ERROR_ACCESS)
	ErrBusy     = Error(C.LIBUSB_ERROR_BUSY)
	ErrPipe     = Error(C.LIBUSB_ERROR_PIPE)
	ErrOther    = Error(C.LIBUSB_ERROR_OTHER)
)

// heldByAnotherProcess reports whether a failed claim means someone else has
// the interface. macOS 26 reports ptpcamerad's hold as OTHER, not ACCESS.
func heldByAnotherProcess(err error) bool {
	return errors.Is(err, ErrAccess) || errors.Is(err, ErrBusy) || errors.Is(err, ErrOther)
}

func check(rc C.int) error {
	if rc < 0 {
		return Error(rc)
	}
	return nil
}

// Context owns a libusb context.
type Context struct {
	ctx  *C.libusb_context
	refs []*C.libusb_device
}

func NewContext() (*Context, error) {
	var ctx *C.libusb_context
	if err := check(C.libusb_init(&ctx)); err != nil {
		return nil, err
	}
	return &Context{ctx: ctx}, nil
}

func (c *Context) Close() {
	for _, d := range c.refs {
		C.libusb_unref_device(d)
	}
	c.refs = nil
	C.libusb_exit(c.ctx)
}

// Candidate is a USB device exposing an MTP-capable interface.
type Candidate struct {
	Bus, Addr    uint8
	VID, PID     uint16
	Manufacturer string
	Product      string
	Serial       string
	Iface, Alt   int
	EpIn, EpOut  uint8
	EpInt        uint8
	MaxPacket    int
	dev          *C.libusb_device
}

func (c *Candidate) String() string {
	name := strings.TrimSpace(c.Manufacturer + " " + c.Product)
	if name == "" {
		name = "unknown device"
	}
	return fmt.Sprintf("%s [%04x:%04x] bus %d addr %d", name, c.VID, c.PID, c.Bus, c.Addr)
}

type ifaceInfo struct {
	num, alt                  int
	class, subclass, protocol uint8
	iInterface                uint8
	epIn, epOut, epInt        uint8
	maxPacket                 int
}

// FindMTP enumerates USB devices and returns those that expose an MTP/PTP interface.
func (c *Context) FindMTP() ([]*Candidate, error) {
	var list **C.libusb_device
	n := C.libusb_get_device_list(c.ctx, &list)
	if n < 0 {
		return nil, Error(n)
	}
	defer C.libusb_free_device_list(list, 1)
	devs := unsafe.Slice(list, int(n))

	var out []*Candidate
	for _, dev := range devs {
		var dd C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &dd) < 0 {
			continue
		}
		ifaces := interfaces(dev)
		if len(ifaces) == 0 {
			continue
		}
		cand := &Candidate{
			Bus: uint8(C.libusb_get_bus_number(dev)), Addr: uint8(C.libusb_get_device_address(dev)),
			VID: uint16(dd.idVendor), PID: uint16(dd.idProduct),
		}
		var h *C.libusb_device_handle
		if C.libusb_open(dev, &h) == 0 {
			cand.Manufacturer = stringDesc(h, uint8(dd.iManufacturer))
			cand.Product = stringDesc(h, uint8(dd.iProduct))
			cand.Serial = stringDesc(h, uint8(dd.iSerialNumber))
		}
		var pick *ifaceInfo
		for i := range ifaces {
			f := &ifaces[i]
			if f.class == 6 && f.subclass == 1 && f.protocol == 1 {
				pick = f // standard Still Image class (PTP or MTP)
				break
			}
			if f.class == 0xFF && h != nil && strings.Contains(strings.ToUpper(stringDesc(h, f.iInterface)), "MTP") {
				pick = f // vendor-class interface named "MTP" (Android MTP mode)
				break
			}
		}
		if h != nil {
			C.libusb_close(h)
		}
		if pick == nil {
			continue
		}
		cand.Iface, cand.Alt = pick.num, pick.alt
		cand.EpIn, cand.EpOut, cand.EpInt, cand.MaxPacket = pick.epIn, pick.epOut, pick.epInt, pick.maxPacket
		C.libusb_ref_device(dev)
		c.refs = append(c.refs, dev)
		cand.dev = dev
		out = append(out, cand)
	}
	return out, nil
}

// interfaces lists the interfaces of the active (or first) configuration that
// have the MTP endpoint shape: bulk IN + bulk OUT + interrupt IN.
func interfaces(dev *C.libusb_device) []ifaceInfo {
	var cfg *C.struct_libusb_config_descriptor
	if C.libusb_get_active_config_descriptor(dev, &cfg) < 0 {
		if C.libusb_get_config_descriptor(dev, 0, &cfg) < 0 {
			return nil
		}
	}
	defer C.libusb_free_config_descriptor(cfg)
	var out []ifaceInfo
	ifs := unsafe.Slice(cfg._interface, int(cfg.bNumInterfaces))
	for i := range ifs {
		for a := 0; a < int(ifs[i].num_altsetting); a++ {
			d := C.alt_at(cfg, C.int(i), C.int(a))
			f := ifaceInfo{
				num: int(d.bInterfaceNumber), alt: int(d.bAlternateSetting),
				class: uint8(d.bInterfaceClass), subclass: uint8(d.bInterfaceSubClass), protocol: uint8(d.bInterfaceProtocol),
				iInterface: uint8(d.iInterface),
			}
			for e := 0; e < int(d.bNumEndpoints); e++ {
				ep := C.ep_at(d, C.int(e))
				addr := uint8(ep.bEndpointAddress)
				in := addr&0x80 != 0
				switch uint8(ep.bmAttributes) & 0x03 {
				case C.LIBUSB_TRANSFER_TYPE_BULK:
					if in {
						f.epIn = addr
						f.maxPacket = int(ep.wMaxPacketSize)
					} else {
						f.epOut = addr
					}
				case C.LIBUSB_TRANSFER_TYPE_INTERRUPT:
					if in {
						f.epInt = addr
					}
				}
			}
			if f.epIn != 0 && f.epOut != 0 && f.epInt != 0 {
				out = append(out, f)
			}
		}
	}
	return out
}

func stringDesc(h *C.libusb_device_handle, idx uint8) string {
	if idx == 0 || h == nil {
		return ""
	}
	buf := make([]byte, 256)
	n := C.libusb_get_string_descriptor_ascii(h, C.uint8_t(idx), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if n < 0 {
		return ""
	}
	return strings.TrimSpace(string(buf[:n]))
}

// Device is an opened MTP interface.
type Device struct {
	h         *C.libusb_device_handle
	iface     int
	EpIn      uint8
	EpOut     uint8
	EpInt     uint8
	MaxPacket int
}

// Open opens the candidate and claims its MTP interface. On macOS the
// ptpcamerad daemon (Image Capture) usually holds the interface; when the claim
// is refused we stop it and retry.
func (c *Context) Open(cand *Candidate) (*Device, error) {
	var h *C.libusb_device_handle
	if err := check(C.libusb_open(cand.dev, &h)); err != nil {
		return nil, fmt.Errorf("open %s: %w", cand, err)
	}
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		err = check(C.libusb_claim_interface(h, C.int(cand.Iface)))
		if err == nil {
			break
		}
		if !heldByAnotherProcess(err) {
			break
		}
		// ptpcamerad runs as the current user and is respawned on demand by launchd.
		_ = exec.Command("killall", "-9", "ptpcamerad").Run()
		time.Sleep(time.Duration(150*(attempt+1)) * time.Millisecond)
	}
	if err != nil {
		C.libusb_close(h)
		if heldByAnotherProcess(err) {
			return nil, fmt.Errorf("claim MTP interface: %w (another app such as the avocadomtp daemon, Android File Transfer, OpenMTP or MacDroid may be using the phone)", err)
		}
		return nil, fmt.Errorf("claim MTP interface: %w", err)
	}
	if cand.Alt != 0 {
		C.libusb_set_interface_alt_setting(h, C.int(cand.Iface), C.int(cand.Alt))
	}
	mp := cand.MaxPacket
	if mp <= 0 {
		mp = 512
	}
	return &Device{h: h, iface: cand.Iface, EpIn: cand.EpIn, EpOut: cand.EpOut, EpInt: cand.EpInt, MaxPacket: mp}, nil
}

func ms(d time.Duration) C.uint { return C.uint(d / time.Millisecond) }

// BulkWrite writes b to the bulk OUT endpoint. A zero-length b sends a ZLP.
func (d *Device) BulkWrite(b []byte, timeout time.Duration) (int, error) {
	var n C.int
	var p *C.uchar
	if len(b) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&b[0]))
	}
	rc := C.libusb_bulk_transfer(d.h, C.uchar(d.EpOut), p, C.int(len(b)), &n, ms(timeout))
	return int(n), check(rc)
}

// BulkRead reads up to len(b) bytes from the bulk IN endpoint.
func (d *Device) BulkRead(b []byte, timeout time.Duration) (int, error) {
	var n C.int
	rc := C.libusb_bulk_transfer(d.h, C.uchar(d.EpIn), (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), &n, ms(timeout))
	return int(n), check(rc)
}

// InterruptRead reads an event packet from the interrupt endpoint.
func (d *Device) InterruptRead(b []byte, timeout time.Duration) (int, error) {
	var n C.int
	rc := C.libusb_interrupt_transfer(d.h, C.uchar(d.EpInt), (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), &n, ms(timeout))
	return int(n), check(rc)
}

// ClearHalt clears a stall condition on all endpoints.
func (d *Device) ClearHalt() {
	C.libusb_clear_halt(d.h, C.uchar(d.EpIn))
	C.libusb_clear_halt(d.h, C.uchar(d.EpOut))
	C.libusb_clear_halt(d.h, C.uchar(d.EpInt))
}

// CancelRequest sends the PTP class-specific "Cancel Request" for a transaction.
func (d *Device) CancelRequest(txID uint32) error {
	buf := []byte{0x01, 0x40, byte(txID), byte(txID >> 8), byte(txID >> 16), byte(txID >> 24)}
	rc := C.libusb_control_transfer(d.h, 0x21, 0x64, 0, C.uint16_t(d.iface),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.uint16_t(len(buf)), 2000)
	return check(rc)
}

func (d *Device) Close() {
	if d.h == nil {
		return
	}
	C.libusb_release_interface(d.h, C.int(d.iface))
	C.libusb_close(d.h)
	d.h = nil
}

// ResetRequest sends the PTP class-specific "Device Reset Request", which
// aborts any transaction the device is stuck in and closes its session.
func (d *Device) ResetRequest() error {
	rc := C.libusb_control_transfer(d.h, 0x21, 0x66, 0, C.uint16_t(d.iface), nil, 0, 2000)
	return check(rc)
}

// PortReset performs a USB port reset. The device re-enumerates, so the
// Device must be closed and opened again afterwards.
func (d *Device) PortReset() error {
	return check(C.libusb_reset_device(d.h))
}
