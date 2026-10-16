package ptp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"
)

// HeaderSize is the size of a container header: length u32, type u16, code u16, transaction u32.
const HeaderSize = 12

// Header is a decoded container header.
type Header struct {
	Length uint32
	Type   uint16
	Code   uint16
	TxID   uint32
}

// EncodeHeader writes a header into the first 12 bytes of b.
func EncodeHeader(b []byte, h Header) {
	binary.LittleEndian.PutUint32(b[0:], h.Length)
	binary.LittleEndian.PutUint16(b[4:], h.Type)
	binary.LittleEndian.PutUint16(b[6:], h.Code)
	binary.LittleEndian.PutUint32(b[8:], h.TxID)
}

// DecodeHeader parses a container header.
func DecodeHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("ptp: short container (%d bytes)", len(b))
	}
	return Header{
		Length: binary.LittleEndian.Uint32(b[0:]),
		Type:   binary.LittleEndian.Uint16(b[4:]),
		Code:   binary.LittleEndian.Uint16(b[6:]),
		TxID:   binary.LittleEndian.Uint32(b[8:]),
	}, nil
}

// EncodeOperation builds a command (or response) container with up to 5 u32 params.
func EncodeOperation(typ, code uint16, txID uint32, params ...uint32) []byte {
	b := make([]byte, HeaderSize+4*len(params))
	EncodeHeader(b, Header{Length: uint32(len(b)), Type: typ, Code: code, TxID: txID})
	for i, p := range params {
		binary.LittleEndian.PutUint32(b[HeaderSize+4*i:], p)
	}
	return b
}

// DecodeParams returns the u32 parameters of a command/response/event container.
func DecodeParams(b []byte) []uint32 {
	var ps []uint32
	for off := HeaderSize; off+4 <= len(b); off += 4 {
		ps = append(ps, binary.LittleEndian.Uint32(b[off:]))
	}
	return ps
}

// ErrShort is returned when a dataset ends unexpectedly.
var ErrShort = errors.New("ptp: dataset truncated")

// Decoder reads little-endian PTP datatypes from a byte slice. The first error sticks.
type Decoder struct {
	b   []byte
	off int
	Err error
}

func NewDecoder(b []byte) *Decoder { return &Decoder{b: b} }

func (d *Decoder) take(n int) []byte {
	if d.Err != nil {
		return nil
	}
	if d.off+n > len(d.b) {
		d.Err = ErrShort
		return nil
	}
	s := d.b[d.off : d.off+n]
	d.off += n
	return s
}

func (d *Decoder) Remaining() int { return len(d.b) - d.off }

func (d *Decoder) U8() uint8 {
	if s := d.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (d *Decoder) U16() uint16 {
	if s := d.take(2); s != nil {
		return binary.LittleEndian.Uint16(s)
	}
	return 0
}

func (d *Decoder) U32() uint32 {
	if s := d.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (d *Decoder) U64() uint64 {
	if s := d.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}

func (d *Decoder) Skip(n int) { d.take(n) }

// String reads a PTP string: u8 char count (including the NUL) then UTF-16LE code units.
func (d *Decoder) String() string {
	n := int(d.U8())
	if n == 0 {
		return ""
	}
	raw := d.take(2 * n)
	if raw == nil {
		return ""
	}
	u := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		c := binary.LittleEndian.Uint16(raw[2*i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

func (d *Decoder) U16Array() []uint16 {
	n := d.U32()
	if d.Err != nil || int(n)*2 > d.Remaining() {
		d.Err = ErrShort
		return nil
	}
	a := make([]uint16, n)
	for i := range a {
		a[i] = d.U16()
	}
	return a
}

func (d *Decoder) U32Array() []uint32 {
	n := d.U32()
	if d.Err != nil || int(n)*4 > d.Remaining() {
		d.Err = ErrShort
		return nil
	}
	a := make([]uint32, n)
	for i := range a {
		a[i] = d.U32()
	}
	return a
}

// Encoder appends little-endian PTP datatypes to a buffer.
type Encoder struct{ B []byte }

func (e *Encoder) U8(v uint8)   { e.B = append(e.B, v) }
func (e *Encoder) U16(v uint16) { e.B = binary.LittleEndian.AppendUint16(e.B, v) }
func (e *Encoder) U32(v uint32) { e.B = binary.LittleEndian.AppendUint32(e.B, v) }
func (e *Encoder) U64(v uint64) { e.B = binary.LittleEndian.AppendUint64(e.B, v) }

// String writes a PTP string. Strings longer than 254 UTF-16 units are truncated.
func (e *Encoder) String(s string) {
	if s == "" {
		e.U8(0)
		return
	}
	u := utf16.Encode([]rune(s))
	if len(u) > 254 {
		u = u[:254]
	}
	e.U8(uint8(len(u) + 1))
	for _, c := range u {
		e.U16(c)
	}
	e.U16(0)
}

const dateLayout = "20060102T150405"

// ParseDate parses a PTP datetime string ("YYYYMMDDThhmmss[.s][Z|±hhmm]").
// Times without a zone are interpreted as local time. Returns zero time on failure.
func ParseDate(s string) time.Time {
	if len(s) < len(dateLayout) {
		return time.Time{}
	}
	base, rest := s[:len(dateLayout)], s[len(dateLayout):]
	if len(rest) > 0 && rest[0] == '.' {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
	}
	loc := time.Local
	switch {
	case rest == "Z":
		loc = time.UTC
	case len(rest) == 5 && (rest[0] == '+' || rest[0] == '-'):
		if z, err := time.Parse("-0700", rest); err == nil {
			loc = z.Location()
		}
	}
	t, err := time.ParseInLocation(dateLayout, base, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

// FormatDate formats a time as a PTP datetime string in local time.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(dateLayout)
}
