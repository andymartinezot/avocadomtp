// Package mtptest provides an in-memory MTP device speaking the USB bulk framing.
package mtptest

import (
	"errors"
	"testing"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

// Fake is an in-memory MTP responder speaking the USB bulk framing. It
// satisfies mtp.Transport.
type Fake struct {
	t   testing.TB
	MPS int

	out [][]byte // queued IN transfers

	// data-out phase in progress
	inData  bool
	dataBuf []byte
	cmdOp   uint16
	cmdTx   uint32
	cmdPar  []uint32

	storages   map[uint32]string
	objs       map[uint32]*fobj
	next       uint32
	sendTarget uint32

	UnknownLen bool

	editing map[uint32]bool // send GetObject data with the 0xFFFFFFFF length sentinel
}

type fobj struct {
	h, storage, parent uint32
	name               string
	dir                bool
	data               []byte
}

var errFakeTimeout = errors.New("fake: timeout")

// New returns a fake phone with "Internal shared storage" and "SD card".
func New(t testing.TB) *Fake {
	return &Fake{
		t: t, MPS: 512, next: 1,
		storages: map[uint32]string{0x00010001: "Internal shared storage", 0x00020001: "SD card"},
		objs:     map[uint32]*fobj{},
	}
}

// Reset drops any half-finished transfer, like a USB reset before a new session.
// SetStorageDescription changes what the device calls a storage.
func (f *Fake) SetStorageDescription(id uint32, desc string) { f.storages[id] = desc }

func (f *Fake) Reset() {
	f.out, f.inData, f.dataBuf = nil, false, nil
}

func (f *Fake) ClearHalt()                 {}
func (f *Fake) CancelRequest(uint32) error { return nil }

func (f *Fake) BulkRead(b []byte, _ time.Duration) (int, error) {
	if len(f.out) == 0 {
		return 0, errFakeTimeout
	}
	front := f.out[0]
	n := copy(b, front)
	if n == len(front) {
		f.out = f.out[1:]
	} else {
		f.out[0] = front[n:]
	}
	return n, nil
}

func (f *Fake) BulkWrite(b []byte, _ time.Duration) (int, error) {
	if f.inData {
		f.dataBuf = append(f.dataBuf, b...)
		h, _ := ptp.DecodeHeader(f.dataBuf)
		done := false
		if h.Length == 0xFFFFFFFF {
			done = len(b)%f.MPS != 0 || len(b) == 0
		} else {
			done = len(f.dataBuf) >= int(h.Length)
			if !done && len(b)%f.MPS != 0 {
				f.t.Errorf("short packet mid data phase (write of %d bytes)", len(b))
			}
		}
		if done {
			f.inData = false
			f.handle(f.cmdOp, f.cmdTx, f.cmdPar, f.dataBuf[ptp.HeaderSize:])
		}
		return len(b), nil
	}
	if len(b) == 0 {
		return 0, nil // ZLP terminating a data phase
	}
	h, err := ptp.DecodeHeader(b)
	if err != nil || h.Type != ptp.TypeCommand {
		f.t.Fatalf("fake: expected command, got % X", b)
	}
	op, tx, par := h.Code, h.TxID, ptp.DecodeParams(b)
	switch op {
	case ptp.OpSendObjectInfo, ptp.OpSendObject, ptp.OpSetObjectPropValue, ptp.OpSendPartialObject:
		f.inData, f.dataBuf, f.cmdOp, f.cmdTx, f.cmdPar = true, nil, op, tx, par
	default:
		f.handle(op, tx, par, nil)
	}
	return len(b), nil
}

func (f *Fake) queue(c []byte) {
	f.out = append(f.out, c)
	if len(c)%f.MPS == 0 {
		f.out = append(f.out, []byte{})
	}
}

func (f *Fake) data(op uint16, tx uint32, payload []byte) {
	c := make([]byte, ptp.HeaderSize+len(payload))
	l := uint32(len(c))
	if f.UnknownLen && op == ptp.OpGetObject {
		l = 0xFFFFFFFF
	}
	ptp.EncodeHeader(c, ptp.Header{Length: l, Type: ptp.TypeData, Code: op, TxID: tx})
	copy(c[ptp.HeaderSize:], payload)
	f.queue(c)
}

func (f *Fake) resp(code uint16, tx uint32, params ...uint32) {
	f.queue(ptp.EncodeOperation(ptp.TypeResponse, code, tx, params...))
}

func (f *Fake) handle(op uint16, tx uint32, p []uint32, in []byte) {
	ok := func(params ...uint32) { f.resp(ptp.RespOK, tx, params...) }
	obj := func(h uint32) *fobj {
		o := f.objs[h]
		if o == nil {
			f.resp(ptp.RespInvalidObjectHandle, tx)
		}
		return o
	}
	switch op {
	case ptp.OpGetDeviceInfo:
		var e ptp.Encoder
		e.U16(100)
		e.U32(6)
		e.U16(100)
		e.String("android.com: 1.0;")
		e.U16(0)
		ops := []uint16{ptp.OpGetDeviceInfo, ptp.OpOpenSession, ptp.OpGetStorageIDs, ptp.OpGetObjectHandles,
			ptp.OpGetObjectInfo, ptp.OpGetObject, ptp.OpSendObjectInfo, ptp.OpSendObject, ptp.OpGetPartialObject64,
			ptp.OpBeginEditObject, ptp.OpSendPartialObject, ptp.OpTruncateObject, ptp.OpEndEditObject}
		e.U32(uint32(len(ops)))
		for _, o := range ops {
			e.U16(o)
		}
		for i := 0; i < 4; i++ {
			e.U32(0)
		}
		e.String("Fake")
		e.String("Phone")
		e.String("1.0")
		e.String("SERIAL")
		f.data(op, tx, e.B)
		ok()
	case ptp.OpOpenSession, ptp.OpCloseSession:
		ok()
	case ptp.OpGetStorageIDs:
		var e ptp.Encoder
		e.U32(3)
		e.U32(0x00010001)
		e.U32(0x00020001)
		e.U32(0x00030000) // empty slot, must be skipped
		f.data(op, tx, e.B)
		ok()
	case ptp.OpGetStorageInfo:
		var e ptp.Encoder
		if p[0]>>16 == 2 {
			e.U16(ptp.StorageRemovableRAM) // the SD card
		} else {
			e.U16(ptp.StorageFixedRAM)
		}
		e.U16(2)
		e.U16(0)
		e.U64(64 << 30)
		e.U64(32 << 30)
		e.U32(0xFFFFFFFF)
		e.String(f.storages[p[0]])
		e.String("")
		f.data(op, tx, e.B)
		ok()
	case ptp.OpGetObjectHandles:
		var hs []uint32
		for h := uint32(1); h < f.next; h++ {
			if o := f.objs[h]; o != nil && o.storage == p[0] && o.parent == p[2] {
				hs = append(hs, h)
			}
		}
		var e ptp.Encoder
		e.U32(uint32(len(hs)))
		for _, h := range hs {
			e.U32(h)
		}
		f.data(op, tx, e.B)
		ok()
	case ptp.OpGetObjectInfo:
		o := obj(p[0])
		if o == nil {
			return
		}
		oi := &ptp.ObjectInfo{StorageID: o.storage, ObjectFormat: ptp.FormatUndefined, ParentObject: o.parent,
			CompressedSize: uint32(len(o.data)), Filename: o.name, DateModified: time.Unix(1_700_000_000, 0)}
		if o.dir {
			oi.ObjectFormat, oi.AssociationType = ptp.FormatAssociation, ptp.AssocGenericFolder
		}
		f.data(op, tx, ptp.EncodeObjectInfo(oi))
		ok()
	case ptp.OpGetObject:
		if o := obj(p[0]); o != nil {
			f.data(op, tx, o.data)
			ok()
		}
	case ptp.OpGetPartialObject64:
		o := obj(p[0])
		if o == nil {
			return
		}
		off := int(uint64(p[1]) | uint64(p[2])<<32)
		end := min(off+int(p[3]), len(o.data))
		f.data(op, tx, o.data[off:end])
		ok(uint32(end - off))
	case ptp.OpSendObjectInfo:
		oi, err := ptp.DecodeObjectInfo(in)
		if err != nil {
			f.t.Fatal(err)
		}
		h := f.next
		f.next++
		f.objs[h] = &fobj{h: h, storage: p[0], parent: p[1], name: oi.Filename, dir: oi.ObjectFormat == ptp.FormatAssociation}
		if !f.objs[h].dir {
			f.sendTarget = h
		}
		ok(p[0], p[1], h)
	case ptp.OpSendObject:
		f.objs[f.sendTarget].data = append([]byte(nil), in...)
		ok()
	case ptp.OpBeginEditObject:
		if o := obj(p[0]); o != nil {
			if f.editing == nil {
				f.editing = map[uint32]bool{}
			}
			f.editing[o.h] = true
			ok()
		}
	case ptp.OpSendPartialObject, ptp.OpTruncateObject:
		o := obj(p[0])
		if o == nil {
			return
		}
		if !f.editing[o.h] {
			f.resp(ptp.RespGeneralError, tx)
			return
		}
		off := int(uint64(p[1]) | uint64(p[2])<<32)
		if op == ptp.OpTruncateObject {
			if off < len(o.data) {
				o.data = o.data[:off]
			} else {
				o.data = append(o.data, make([]byte, off-len(o.data))...)
			}
			ok()
			return
		}
		if end := off + len(in); end > len(o.data) {
			o.data = append(o.data, make([]byte, end-len(o.data))...)
		}
		copy(o.data[off:], in)
		ok(uint32(len(in)))
	case ptp.OpEndEditObject:
		if o := obj(p[0]); o != nil {
			delete(f.editing, o.h)
			ok()
		}
	case ptp.OpSetObjectPropValue:
		if o := obj(p[0]); o != nil {
			o.name = ptp.NewDecoder(in).String()
			ok()
		}
	case ptp.OpMoveObject:
		if o := obj(p[0]); o != nil {
			o.storage, o.parent = p[1], p[2]
			ok()
		}
	case ptp.OpDeleteObject:
		if o := obj(p[0]); o != nil {
			f.deleteTree(o.h)
			ok()
		}
	default:
		f.resp(ptp.RespOperationNotSupported, tx)
	}
}

func (f *Fake) deleteTree(h uint32) {
	for c, o := range f.objs {
		if o.parent == h {
			f.deleteTree(c)
		}
	}
	delete(f.objs, h)
}
