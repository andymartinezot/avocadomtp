package mtp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

func TestDatasetSizeLimit(t *testing.T) {
	var b []byte
	w := writerTo(&b)
	if _, err := w.Write(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, maxDataset)); !errors.Is(err, ErrDatasetTooLarge) {
		t.Fatalf("expected ErrDatasetTooLarge, got %v", err)
	}
	if len(b) != 1024 {
		t.Fatalf("buffer grew past the limit: %d", len(b))
	}
}

// scripted is a Transport that returns the given IN packets in order, then
// empty packets forever.
type scripted struct{ in [][]byte }

func (s *scripted) BulkWrite(b []byte, _ time.Duration) (int, error) { return len(b), nil }
func (s *scripted) BulkRead(b []byte, _ time.Duration) (int, error) {
	if len(s.in) == 0 {
		return 0, nil
	}
	n := copy(b, s.in[0])
	s.in = s.in[1:]
	return n, nil
}
func (s *scripted) ClearHalt()                 {}
func (s *scripted) CancelRequest(uint32) error { return nil }

func dataHeader(length uint32) ptp.Header {
	return ptp.Header{Length: length, Type: ptp.TypeData, Code: ptp.OpGetObject, TxID: 1}
}

// A stray empty packet mid-phase must not repeat the previous chunk, and a
// device that only sends empty packets must not keep the session spinning.
func TestRecvDataStrayZLPs(t *testing.T) {
	first := make([]byte, 512)
	ptp.EncodeHeader(first, dataHeader(ptp.HeaderSize+600))
	copy(first[ptp.HeaderSize:], bytes.Repeat([]byte("A"), 500))
	tr := &scripted{in: [][]byte{{}, bytes.Repeat([]byte("B"), 100)}}
	s := &Session{t: tr, buf: make([]byte, 512)}
	var got bytes.Buffer
	if err := s.recvData(context.Background(), 1, dataHeader(ptp.HeaderSize+600), first, &got); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("A", 500) + strings.Repeat("B", 100); got.String() != want {
		t.Fatalf("got %d bytes (%q…), want 500×A + 100×B", got.Len(), got.String()[:min(20, got.Len())])
	}

	s = &Session{t: &scripted{}, buf: make([]byte, 512)} // only empty packets after the first
	err := s.recvData(context.Background(), 1, dataHeader(ptp.HeaderSize+600), first, io.Discard)
	if err == nil {
		t.Fatal("expected an error for a phase of empty packets")
	}
}

func TestSendDataRejectsNegativeSize(t *testing.T) {
	s := &Session{t: &scripted{}, buf: make([]byte, 512)}
	if err := s.sendData(context.Background(), 1, ptp.OpSendObject, strings.NewReader(""), -100); err == nil {
		t.Fatal("expected an error for a negative size")
	}
}

func TestFillWriterRejectsOverlong(t *testing.T) {
	w := &fillWriter{p: make([]byte, 4)}
	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("de")); !errors.Is(err, errOverlong) {
		t.Fatalf("expected errOverlong, got %v", err)
	}
	if string(w.p) != "abcd" || w.n != 4 {
		t.Fatalf("buffer %q n=%d", w.p, w.n)
	}
}
