package mtp

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

// Object is a file or folder on the device.
type Object struct {
	Handle    uint32
	StorageID uint32
	Parent    uint32
	Name      string
	Size      uint64
	IsDir     bool
	Format    uint16
	Modified  time.Time
}

func (s *Session) get(ctx context.Context, op uint16, params ...uint32) ([]byte, error) {
	var b []byte
	_, err := s.Transact(ctx, op, params, nil, 0, writerTo(&b))
	return b, err
}

// StorageIDs lists the storages (internal memory, SD card, ...). Storages that
// are present but unavailable (e.g. an empty card slot) have a zero low word.
func (s *Session) StorageIDs(ctx context.Context) ([]uint32, error) {
	b, err := s.get(ctx, ptp.OpGetStorageIDs)
	if err != nil {
		return nil, err
	}
	d := ptp.NewDecoder(b)
	all := d.U32Array()
	if d.Err != nil {
		return nil, d.Err
	}
	var ids []uint32
	for _, id := range all {
		if id&0xFFFF != 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Session) StorageInfo(ctx context.Context, id uint32) (*ptp.StorageInfo, error) {
	b, err := s.get(ctx, ptp.OpGetStorageInfo, id)
	if err != nil {
		return nil, err
	}
	return ptp.DecodeStorageInfo(b)
}

// ObjectHandles lists the direct children of parent (ptp.HandleRoot for the storage root).
func (s *Session) ObjectHandles(ctx context.Context, storage, parent uint32) ([]uint32, error) {
	b, err := s.get(ctx, ptp.OpGetObjectHandles, storage, 0, parent)
	if err != nil {
		return nil, err
	}
	d := ptp.NewDecoder(b)
	hs := d.U32Array()
	return hs, d.Err
}

// Object fetches the metadata of a single object.
func (s *Session) Object(ctx context.Context, h uint32) (*Object, error) {
	b, err := s.get(ctx, ptp.OpGetObjectInfo, h)
	if err != nil {
		return nil, err
	}
	oi, err := ptp.DecodeObjectInfo(b)
	if err != nil {
		return nil, err
	}
	o := &Object{
		Handle: h, StorageID: oi.StorageID, Parent: oi.ParentObject, Name: oi.Filename,
		Size: uint64(oi.CompressedSize), IsDir: oi.ObjectFormat == ptp.FormatAssociation,
		Format: oi.ObjectFormat, Modified: oi.DateModified,
	}
	if o.Modified.IsZero() {
		o.Modified = oi.DateCreated
	}
	if !o.IsDir && oi.CompressedSize == ptp.SizeUnknown32 {
		if sz, err := s.ObjectSize(ctx, h); err == nil {
			o.Size = sz
		}
	}
	return o, nil
}

// ObjectSize returns the 64-bit size property (needed for files >= 4 GiB).
func (s *Session) ObjectSize(ctx context.Context, h uint32) (uint64, error) {
	b, err := s.get(ctx, ptp.OpGetObjectPropValue, h, uint32(ptp.PropObjectSize))
	if err != nil {
		return 0, err
	}
	v, err := ptp.DecodeValue(b, ptp.DTUint64)
	return v.Int, err
}

// List returns the children of parent in storage.
func (s *Session) List(ctx context.Context, storage, parent uint32) ([]*Object, error) {
	// GetObjectPropList fetches a whole folder in one round trip. Its handling of
	// the storage root differs between devices, so it is only used for sub folders.
	if parent != ptp.HandleRoot && s.Info.Supports(ptp.OpGetObjectPropList) {
		if objs, err := s.listPropList(ctx, storage, parent); err == nil {
			return objs, nil
		}
	}
	hs, err := s.ObjectHandles(ctx, storage, parent)
	if err != nil {
		return nil, err
	}
	objs := make([]*Object, 0, len(hs))
	for _, h := range hs {
		o, err := s.Object(ctx, h)
		if err != nil {
			if IsResp(err, ptp.RespInvalidObjectHandle) {
				continue // deleted meanwhile
			}
			return nil, err
		}
		objs = append(objs, o)
	}
	return objs, nil
}

func (s *Session) listPropList(ctx context.Context, storage, parent uint32) ([]*Object, error) {
	b, err := s.get(ctx, ptp.OpGetObjectPropList, parent, 0, 0xFFFFFFFF, 0, 1)
	if err != nil {
		return nil, err
	}
	props, err := ptp.DecodeObjectPropList(b)
	if err != nil {
		return nil, err
	}
	byHandle := map[uint32]*Object{}
	var order []*Object
	for _, p := range props {
		o := byHandle[p.Handle]
		if o == nil {
			o = &Object{Handle: p.Handle, StorageID: storage, Parent: parent}
			byHandle[p.Handle] = o
			order = append(order, o)
		}
		switch p.Code {
		case ptp.PropStorageID:
			o.StorageID = uint32(p.Int)
		case ptp.PropObjectFormat:
			o.Format = uint16(p.Int)
			o.IsDir = o.Format == ptp.FormatAssociation
		case ptp.PropObjectSize:
			o.Size = p.Int
		case ptp.PropObjectFileName:
			o.Name = p.Str
		case ptp.PropParentObject:
			o.Parent = uint32(p.Int)
		case ptp.PropDateModified:
			o.Modified = ptp.ParseDate(p.Str)
		case ptp.PropDateCreated:
			if o.Modified.IsZero() {
				o.Modified = ptp.ParseDate(p.Str)
			}
		}
	}
	out := order[:0]
	for _, o := range order {
		if o.Handle != parent && o.Parent == parent && o.Name != "" {
			out = append(out, o)
		}
	}
	return out, nil
}

// Download streams the whole object into w.
func (s *Session) Download(ctx context.Context, h uint32, w io.Writer) error {
	_, err := s.Transact(ctx, ptp.OpGetObject, []uint32{h}, nil, 0, w)
	return err
}

// ReadAt reads up to len(p) bytes at offset off (Android GetPartialObject64).
func (s *Session) ReadAt(ctx context.Context, h uint32, p []byte, off int64) (int, error) {
	if !s.Info.Supports(ptp.OpGetPartialObject64) {
		return 0, &RespError{Op: ptp.OpGetPartialObject64, Code: ptp.RespOperationNotSupported}
	}
	buf := bytes.NewBuffer(p[:0])
	_, err := s.Transact(ctx, ptp.OpGetPartialObject64,
		[]uint32{h, uint32(off), uint32(uint64(off) >> 32), uint32(len(p))}, nil, 0, buf)
	n := copy(p, buf.Bytes())
	if err == nil && n < len(p) {
		err = io.EOF
	}
	return n, err
}

// Upload creates a file named name in parent and streams size bytes from r.
func (s *Session) Upload(ctx context.Context, storage, parent uint32, name string, size int64, mtime time.Time, r io.Reader) (uint32, error) {
	oi := &ptp.ObjectInfo{
		StorageID: storage, ObjectFormat: ptp.FormatUndefined, ParentObject: parent,
		CompressedSize: uint32(min(size, int64(ptp.SizeUnknown32))), Filename: name, DateModified: mtime,
	}
	h, err := s.sendObjectInfo(ctx, storage, parent, oi)
	if err != nil {
		return 0, err
	}
	if _, err := s.Transact(ctx, ptp.OpSendObject, nil, r, size, nil); err != nil {
		return h, err
	}
	return h, nil
}

// Mkdir creates a folder and returns its handle.
func (s *Session) Mkdir(ctx context.Context, storage, parent uint32, name string) (uint32, error) {
	oi := &ptp.ObjectInfo{
		StorageID: storage, ObjectFormat: ptp.FormatAssociation, ParentObject: parent,
		AssociationType: ptp.AssocGenericFolder, Filename: name, DateModified: time.Now(),
	}
	return s.sendObjectInfo(ctx, storage, parent, oi)
}

func (s *Session) sendObjectInfo(ctx context.Context, storage, parent uint32, oi *ptp.ObjectInfo) (uint32, error) {
	ds := ptp.EncodeObjectInfo(oi)
	resp, err := s.Transact(ctx, ptp.OpSendObjectInfo, []uint32{storage, parent}, bytes.NewReader(ds), int64(len(ds)), nil)
	if err != nil {
		return 0, err
	}
	return u32(resp, 2), nil
}

// Delete removes an object. Android deletes folders recursively.
func (s *Session) Delete(ctx context.Context, h uint32) error {
	_, err := s.Transact(ctx, ptp.OpDeleteObject, []uint32{h, 0}, nil, 0, nil)
	return err
}

// Rename changes an object's file name.
func (s *Session) Rename(ctx context.Context, h uint32, name string) error {
	var e ptp.Encoder
	e.String(name)
	_, err := s.Transact(ctx, ptp.OpSetObjectPropValue, []uint32{h, uint32(ptp.PropObjectFileName)},
		bytes.NewReader(e.B), int64(len(e.B)), nil)
	return err
}

// Move moves an object to a new parent (possibly in another storage).
func (s *Session) Move(ctx context.Context, h, storage, parent uint32) error {
	_, err := s.Transact(ctx, ptp.OpMoveObject, []uint32{h, storage, parent}, nil, 0, nil)
	return err
}
