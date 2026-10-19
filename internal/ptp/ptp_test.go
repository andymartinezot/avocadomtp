package ptp

import (
	"bytes"
	"testing"
	"time"
)

func TestEncodeOperation(t *testing.T) {
	got := EncodeOperation(TypeCommand, OpOpenSession, 0, 1)
	want := []byte{
		0x10, 0x00, 0x00, 0x00, // length 16
		0x01, 0x00, // command
		0x02, 0x10, // OpenSession
		0x00, 0x00, 0x00, 0x00, // tx 0
		0x01, 0x00, 0x00, 0x00, // session 1
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % X\nwant % X", got, want)
	}
	h, err := DecodeHeader(got)
	if err != nil || h.Length != 16 || h.Type != TypeCommand || h.Code != OpOpenSession {
		t.Fatalf("bad header %+v %v", h, err)
	}
	if ps := DecodeParams(got); len(ps) != 1 || ps[0] != 1 {
		t.Fatalf("params %v", ps)
	}
}

func TestString(t *testing.T) {
	var e Encoder
	e.String("Hi")
	want := []byte{0x03, 'H', 0, 'i', 0, 0, 0}
	if !bytes.Equal(e.B, want) {
		t.Fatalf("got % X want % X", e.B, want)
	}
	for _, s := range []string{"", "Internal shared storage", "año 🥑.jpg"} {
		var e Encoder
		e.String(s)
		if got := NewDecoder(e.B).String(); got != s {
			t.Fatalf("round trip %q -> %q", s, got)
		}
	}
}

func TestObjectInfoRoundTrip(t *testing.T) {
	mod := time.Date(2026, 10, 16, 13, 29, 5, 0, time.Local)
	in := &ObjectInfo{
		StorageID: 0x00010001, ObjectFormat: FormatAssociation, ParentObject: HandleRoot,
		AssociationType: AssocGenericFolder, Filename: "DCIM", DateModified: mod,
	}
	out, err := DecodeObjectInfo(EncodeObjectInfo(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.StorageID != in.StorageID || out.ObjectFormat != in.ObjectFormat || out.ParentObject != in.ParentObject ||
		out.AssociationType != in.AssociationType || out.Filename != in.Filename || !out.DateModified.Equal(mod) {
		t.Fatalf("mismatch: %+v", out)
	}
}

func TestStorageInfo(t *testing.T) {
	var e Encoder
	e.U16(3)
	e.U16(2)
	e.U16(0)
	e.U64(128 << 30)
	e.U64(64 << 30)
	e.U32(0xFFFFFFFF)
	e.String("SD card")
	e.String("")
	si, err := DecodeStorageInfo(e.B)
	if err != nil {
		t.Fatal(err)
	}
	if si.MaxCapacity != 128<<30 || si.FreeSpaceInBytes != 64<<30 || si.Description != "SD card" {
		t.Fatalf("%+v", si)
	}
}

func TestObjectPropList(t *testing.T) {
	var e Encoder
	e.U32(3)
	e.U32(7)
	e.U16(PropObjectFileName)
	e.U16(DTString)
	e.String("movie.mp4")
	e.U32(7)
	e.U16(PropObjectSize)
	e.U16(DTUint64)
	e.U64(5 << 30)
	e.U32(7)
	e.U16(0xDC03) // protection status, ignored
	e.U16(DTUint16)
	e.U16(0)
	pl, err := DecodeObjectPropList(e.B)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl) != 3 || pl[0].Str != "movie.mp4" || pl[1].Int != 5<<30 || pl[2].Handle != 7 {
		t.Fatalf("%+v", pl)
	}
}

func TestParseDate(t *testing.T) {
	cases := map[string]time.Time{
		"20261016T132905":      time.Date(2026, 10, 16, 13, 29, 5, 0, time.Local),
		"20261016T132905.0":    time.Date(2026, 10, 16, 13, 29, 5, 0, time.Local),
		"20261016T132905Z":     time.Date(2026, 10, 16, 13, 29, 5, 0, time.UTC),
		"20261016T132905+0200": time.Date(2026, 10, 16, 11, 29, 5, 0, time.UTC),
	}
	for s, want := range cases {
		if got := ParseDate(s); !got.Equal(want) {
			t.Errorf("%s: got %v want %v", s, got, want)
		}
	}
	if !ParseDate("garbage").IsZero() {
		t.Error("expected zero time")
	}
}

// A device-supplied element count larger than the data must fail cleanly,
// not panic or size a huge allocation.
func TestObjectPropListHostileCount(t *testing.T) {
	for _, b := range [][]byte{
		{0xff, 0xff, 0xff, 0xff},                   // 4 billion elements, no data
		{0x10, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6}, // 16 elements, 6 bytes
	} {
		if _, err := DecodeObjectPropList(b); err == nil {
			t.Errorf("% x: expected an error", b)
		}
	}
}

func TestDecodeHeaderRejectsShortLength(t *testing.T) {
	b := []byte{4, 0, 0, 0, 2, 0, 0x09, 0x10, 1, 0, 0, 0} // data container claiming 4 bytes
	if _, err := DecodeHeader(b); err == nil {
		t.Fatal("expected an error for a length below the header size")
	}
	b[0], b[1], b[2], b[3] = 0xff, 0xff, 0xff, 0xff // "unknown length" stays valid
	if _, err := DecodeHeader(b); err != nil {
		t.Fatalf("unknown length: %v", err)
	}
}
