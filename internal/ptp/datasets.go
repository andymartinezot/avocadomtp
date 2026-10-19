package ptp

import (
	"fmt"
	"time"
)

// DeviceInfo is the GetDeviceInfo dataset.
type DeviceInfo struct {
	StandardVersion      uint16
	VendorExtensionID    uint32
	VendorExtensionVer   uint16
	VendorExtensionDesc  string
	FunctionalMode       uint16
	OperationsSupported  []uint16
	EventsSupported      []uint16
	DevicePropsSupported []uint16
	CaptureFormats       []uint16
	PlaybackFormats      []uint16
	Manufacturer         string
	Model                string
	DeviceVersion        string
	SerialNumber         string
}

func DecodeDeviceInfo(b []byte) (*DeviceInfo, error) {
	d := NewDecoder(b)
	di := &DeviceInfo{
		StandardVersion:      d.U16(),
		VendorExtensionID:    d.U32(),
		VendorExtensionVer:   d.U16(),
		VendorExtensionDesc:  d.String(),
		FunctionalMode:       d.U16(),
		OperationsSupported:  d.U16Array(),
		EventsSupported:      d.U16Array(),
		DevicePropsSupported: d.U16Array(),
		CaptureFormats:       d.U16Array(),
		PlaybackFormats:      d.U16Array(),
		Manufacturer:         d.String(),
		Model:                d.String(),
		DeviceVersion:        d.String(),
		SerialNumber:         d.String(),
	}
	if d.Err != nil {
		return nil, fmt.Errorf("DeviceInfo: %w", d.Err)
	}
	return di, nil
}

// Supports reports whether the device advertises an operation.
func (di *DeviceInfo) Supports(op uint16) bool {
	for _, o := range di.OperationsSupported {
		if o == op {
			return true
		}
	}
	return false
}

// StorageInfo is the GetStorageInfo dataset.
type StorageInfo struct {
	StorageType        uint16
	FilesystemType     uint16
	AccessCapability   uint16
	MaxCapacity        uint64
	FreeSpaceInBytes   uint64
	FreeSpaceInObjects uint32
	Description        string
	VolumeLabel        string
}

func DecodeStorageInfo(b []byte) (*StorageInfo, error) {
	d := NewDecoder(b)
	si := &StorageInfo{
		StorageType:        d.U16(),
		FilesystemType:     d.U16(),
		AccessCapability:   d.U16(),
		MaxCapacity:        d.U64(),
		FreeSpaceInBytes:   d.U64(),
		FreeSpaceInObjects: d.U32(),
		Description:        d.String(),
		VolumeLabel:        d.String(),
	}
	if d.Err != nil {
		return nil, fmt.Errorf("StorageInfo: %w", d.Err)
	}
	return si, nil
}

// ObjectInfo is the GetObjectInfo / SendObjectInfo dataset.
type ObjectInfo struct {
	StorageID           uint32
	ObjectFormat        uint16
	ProtectionStatus    uint16
	CompressedSize      uint32
	ThumbFormat         uint16
	ThumbCompressedSize uint32
	ThumbPixWidth       uint32
	ThumbPixHeight      uint32
	ImagePixWidth       uint32
	ImagePixHeight      uint32
	ImageBitDepth       uint32
	ParentObject        uint32
	AssociationType     uint16
	AssociationDesc     uint32
	SequenceNumber      uint32
	Filename            string
	DateCreated         time.Time
	DateModified        time.Time
	Keywords            string
}

func DecodeObjectInfo(b []byte) (*ObjectInfo, error) {
	d := NewDecoder(b)
	oi := &ObjectInfo{
		StorageID:           d.U32(),
		ObjectFormat:        d.U16(),
		ProtectionStatus:    d.U16(),
		CompressedSize:      d.U32(),
		ThumbFormat:         d.U16(),
		ThumbCompressedSize: d.U32(),
		ThumbPixWidth:       d.U32(),
		ThumbPixHeight:      d.U32(),
		ImagePixWidth:       d.U32(),
		ImagePixHeight:      d.U32(),
		ImageBitDepth:       d.U32(),
		ParentObject:        d.U32(),
		AssociationType:     d.U16(),
		AssociationDesc:     d.U32(),
		SequenceNumber:      d.U32(),
		Filename:            d.String(),
	}
	oi.DateCreated = ParseDate(d.String())
	oi.DateModified = ParseDate(d.String())
	oi.Keywords = d.String()
	// Some devices omit the trailing strings; only the fields up to Filename are required.
	if d.Err != nil && oi.Filename == "" {
		return nil, fmt.Errorf("ObjectInfo: %w", d.Err)
	}
	return oi, nil
}

func EncodeObjectInfo(oi *ObjectInfo) []byte {
	var e Encoder
	e.U32(oi.StorageID)
	e.U16(oi.ObjectFormat)
	e.U16(oi.ProtectionStatus)
	e.U32(oi.CompressedSize)
	e.U16(oi.ThumbFormat)
	e.U32(oi.ThumbCompressedSize)
	e.U32(oi.ThumbPixWidth)
	e.U32(oi.ThumbPixHeight)
	e.U32(oi.ImagePixWidth)
	e.U32(oi.ImagePixHeight)
	e.U32(oi.ImageBitDepth)
	e.U32(oi.ParentObject)
	e.U16(oi.AssociationType)
	e.U32(oi.AssociationDesc)
	e.U32(oi.SequenceNumber)
	e.String(oi.Filename)
	e.String(FormatDate(oi.DateCreated))
	e.String(FormatDate(oi.DateModified))
	e.String(oi.Keywords)
	return e.B
}

// PropValue is one element of an ObjectPropList.
type PropValue struct {
	Handle   uint32
	Code     uint16
	DataType uint16
	Int      uint64 // integer types (128-bit types keep the low 64 bits)
	Str      string // DTString
}

// DecodeObjectPropList parses a GetObjectPropList dataset.
func DecodeObjectPropList(b []byte) ([]PropValue, error) {
	d := NewDecoder(b)
	n := d.U32()
	// Every element takes at least 8 bytes (handle, code, type): a larger count
	// than the data can hold is malformed, and must not size the allocation.
	if d.Err != nil || uint64(n)*8 > uint64(d.Remaining()) {
		return nil, fmt.Errorf("ObjectPropList: %w", ErrShort)
	}
	out := make([]PropValue, 0, min(n, 4096)) // grows as real data arrives
	for i := uint32(0); i < n && d.Err == nil; i++ {
		pv := PropValue{Handle: d.U32(), Code: d.U16(), DataType: d.U16()}
		decodeValue(d, &pv)
		out = append(out, pv)
	}
	if d.Err != nil {
		return nil, fmt.Errorf("ObjectPropList: %w", d.Err)
	}
	return out, nil
}

// DecodeValue decodes a single typed value (as returned by GetObjectPropValue).
func DecodeValue(b []byte, dataType uint16) (PropValue, error) {
	d := NewDecoder(b)
	pv := PropValue{DataType: dataType}
	decodeValue(d, &pv)
	return pv, d.Err
}

func decodeValue(d *Decoder, pv *PropValue) {
	switch t := pv.DataType; {
	case t == DTString:
		pv.Str = d.String()
	case t == DTInt8 || t == DTUint8:
		pv.Int = uint64(d.U8())
	case t == DTInt16 || t == DTUint16:
		pv.Int = uint64(d.U16())
	case t == DTInt32 || t == DTUint32:
		pv.Int = uint64(d.U32())
	case t == DTInt64 || t == DTUint64:
		pv.Int = d.U64()
	case t == DTInt128 || t == DTUint128:
		pv.Int = d.U64()
		d.Skip(8)
	case t&DTArray != 0:
		n := int(d.U32())
		size := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 2, 5: 4, 6: 4, 7: 8, 8: 8, 9: 16, 10: 16}[t&^DTArray]
		if size == 0 {
			d.Err = fmt.Errorf("ptp: unknown array element type 0x%04X", t)
			return
		}
		d.Skip(n * size)
	default:
		d.Err = fmt.Errorf("ptp: unknown data type 0x%04X", t)
	}
}
