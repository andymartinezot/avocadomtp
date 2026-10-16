// Package mtp implements an MTP initiator (the "computer" side) on top of a
// USB bulk transport.
package mtp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

// Transport is the USB side of a session. *usbx.Device satisfies it via an adapter.
type Transport interface {
	BulkWrite(b []byte, timeout time.Duration) (int, error)
	BulkRead(b []byte, timeout time.Duration) (int, error)
	ClearHalt()
	CancelRequest(txID uint32) error
}

// RespError is a non-OK MTP response.
type RespError struct {
	Op   uint16
	Code uint16
}

func (e *RespError) Error() string {
	return fmt.Sprintf("mtp: operation 0x%04X failed: %s", e.Op, ptp.RespName(e.Code))
}

// IsResp reports whether err is an MTP response error with the given code.
func IsResp(err error, code uint16) bool {
	var re *RespError
	return errors.As(err, &re) && re.Code == code
}

const (
	chunkSize   = 1 << 20 // multiple of every USB max packet size
	cmdTimeout  = 10 * time.Second
	dataTimeout = 60 * time.Second
	respTimeout = 120 * time.Second // the phone may flush a large file before answering
)

// Session is an open MTP session. All operations are serialized.
type Session struct {
	mu      sync.Mutex
	t       Transport
	mps     int
	tx      uint32
	buf     []byte
	pending []byte // bytes read past the end of a data phase (start of the response)
	broken  error  // set when the transport is out of sync; the session must be reopened
	Info    *ptp.DeviceInfo
}

// Open starts a session on the transport and fetches the device info.
func Open(t Transport, maxPacket int) (*Session, error) {
	if maxPacket <= 0 {
		maxPacket = 512
	}
	s := &Session{t: t, mps: maxPacket, buf: make([]byte, chunkSize)}

	// DeviceInfo may be requested outside a session (transaction 0).
	var info []byte
	if err := s.transactTx(context.Background(), 0, ptp.OpGetDeviceInfo, nil, nil, 0, writerTo(&info), nil); err != nil {
		return nil, fmt.Errorf("GetDeviceInfo: %w", err)
	}
	di, err := ptp.DecodeDeviceInfo(info)
	if err != nil {
		return nil, err
	}
	s.Info = di

	err = s.transactTx(context.Background(), 0, ptp.OpOpenSession, []uint32{1}, nil, 0, nil, nil)
	if err != nil && !IsResp(err, ptp.RespSessionAlreadyOpen) {
		return nil, fmt.Errorf("OpenSession: %w", err)
	}
	s.tx = 0 // the first transaction inside the session is 1
	return s, nil
}

// Err returns non-nil once the session is out of sync with the device (after
// an aborted transfer or USB error) and must be reopened.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}

// Close ends the session (the transport is owned by the caller).
func (s *Session) Close() error {
	if s.broken != nil {
		return nil
	}
	_, err := s.Transact(context.Background(), ptp.OpCloseSession, nil, nil, 0, nil)
	return err
}

// Transact runs one operation. dataOut (with its length) is sent as the data
// phase; dataIn, if non-nil, receives the device's data phase. It returns the
// response parameters.
func (s *Session) Transact(ctx context.Context, op uint16, params []uint32, dataOut io.Reader, outLen int64, dataIn io.Writer) ([]uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return nil, s.broken
	}
	s.tx++
	var resp []uint32
	err := s.transactTx(ctx, s.tx, op, params, dataOut, outLen, dataIn, &resp)
	return resp, err
}

func (s *Session) fail(err error) error {
	var re *RespError
	if err != nil && !errors.As(err, &re) {
		s.broken = fmt.Errorf("mtp session unusable after error: %w", err)
		s.pending = nil
	}
	return err
}

func (s *Session) transactTx(ctx context.Context, tx uint32, op uint16, params []uint32, dataOut io.Reader, outLen int64, dataIn io.Writer, respOut *[]uint32) error {
	cmd := ptp.EncodeOperation(ptp.TypeCommand, op, tx, params...)
	if _, err := s.t.BulkWrite(cmd, cmdTimeout); err != nil {
		return s.fail(fmt.Errorf("send command: %w", err))
	}

	if dataOut != nil {
		if err := s.sendData(ctx, tx, op, dataOut, outLen); err != nil {
			return s.fail(err)
		}
	}

	// Read the first container after the command: either a data phase or the response.
	first, n, err := s.readFirst(cmdTimeoutFor(dataIn))
	if err != nil {
		return s.fail(err)
	}
	h, err := ptp.DecodeHeader(first[:n])
	if err != nil {
		return s.fail(err)
	}
	if h.Type == ptp.TypeData {
		if err := s.recvData(ctx, tx, h, first[:n], dataIn); err != nil {
			return s.fail(err)
		}
		if first, n, err = s.readFirst(respTimeout); err != nil {
			return s.fail(err)
		}
		if h, err = ptp.DecodeHeader(first[:n]); err != nil {
			return s.fail(err)
		}
	}
	if h.Type != ptp.TypeResponse {
		return s.fail(fmt.Errorf("mtp: expected response, got container type %d", h.Type))
	}
	if h.TxID != tx {
		return s.fail(fmt.Errorf("mtp: response for transaction %d, expected %d", h.TxID, tx))
	}
	if respOut != nil {
		*respOut = ptp.DecodeParams(first[:min(n, int(h.Length))])
	}
	if h.Code != ptp.RespOK {
		return &RespError{Op: op, Code: h.Code}
	}
	return nil
}

func cmdTimeoutFor(dataIn io.Writer) time.Duration {
	if dataIn != nil {
		return dataTimeout
	}
	return respTimeout
}

// readFirst returns the next container start, taking leftover bytes first and
// skipping zero-length packets that terminate a previous phase.
func (s *Session) readFirst(timeout time.Duration) ([]byte, int, error) {
	if len(s.pending) > 0 {
		n := copy(s.buf, s.pending)
		s.pending = nil
		return s.buf, n, nil
	}
	for tries := 0; tries < 4; tries++ {
		n, err := s.t.BulkRead(s.buf, timeout)
		if err != nil {
			return nil, 0, fmt.Errorf("read: %w", err)
		}
		if n >= ptp.HeaderSize {
			return s.buf, n, nil
		}
		if n != 0 {
			return nil, 0, fmt.Errorf("mtp: short container (%d bytes)", n)
		}
	}
	return nil, 0, errors.New("mtp: device sent only empty packets")
}

func (s *Session) recvData(ctx context.Context, tx uint32, h ptp.Header, first []byte, w io.Writer) error {
	if h.TxID != tx {
		return fmt.Errorf("mtp: data for transaction %d, expected %d", h.TxID, tx)
	}
	if w == nil {
		w = io.Discard
	}
	unknown := h.Length == 0xFFFFFFFF // >4 GiB: read until a short transfer
	remaining := int64(h.Length) - ptp.HeaderSize

	chunk := first[ptp.HeaderSize:]
	lastShort := len(first) < len(s.buf)
	for {
		if !unknown && int64(len(chunk)) > remaining {
			s.pending = append([]byte(nil), chunk[remaining:]...)
			chunk = chunk[:remaining]
		}
		if len(chunk) > 0 {
			if _, err := w.Write(chunk); err != nil {
				s.drain()
				return fmt.Errorf("write local data: %w", err)
			}
		}
		if unknown {
			if lastShort {
				return nil
			}
		} else {
			remaining -= int64(len(chunk))
			if remaining <= 0 {
				return nil
			}
		}
		if err := ctx.Err(); err != nil {
			_ = s.t.CancelRequest(tx)
			s.drain()
			return err
		}
		n, err := s.t.BulkRead(s.buf, dataTimeout)
		if err != nil {
			return fmt.Errorf("read data: %w", err)
		}
		if n == 0 && !unknown {
			continue // stray ZLP
		}
		chunk = s.buf[:n]
		lastShort = n < len(s.buf)
	}
}

// drain discards incoming bulk data after an aborted transfer.
func (s *Session) drain() {
	for i := 0; i < 1<<14; i++ {
		if n, err := s.t.BulkRead(s.buf, 300*time.Millisecond); err != nil || n == 0 {
			break
		}
	}
	s.t.ClearHalt()
}

func (s *Session) sendData(ctx context.Context, tx uint32, op uint16, r io.Reader, size int64) error {
	total := size + ptp.HeaderSize
	length := uint32(0xFFFFFFFF)
	if total < 0xFFFFFFFF {
		length = uint32(total)
	}
	buf := s.buf
	ptp.EncodeHeader(buf, ptp.Header{Length: length, Type: ptp.TypeData, Code: op, TxID: tx})

	// Every transfer except the last must be a multiple of the max packet
	// size, otherwise the device sees a short packet and ends the phase early.
	off := ptp.HeaderSize
	var sent int64
	for {
		want := min(int64(len(buf)-off), size-sent)
		if want > 0 {
			if _, err := io.ReadFull(r, buf[off:off+int(want)]); err != nil {
				_ = s.t.CancelRequest(tx)
				s.t.ClearHalt()
				return fmt.Errorf("read local data: %w", err)
			}
		}
		n := off + int(want)
		if _, err := s.t.BulkWrite(buf[:n], dataTimeout); err != nil {
			return fmt.Errorf("send data: %w", err)
		}
		sent += want
		off = 0
		if sent >= size {
			if n%s.mps == 0 {
				if _, err := s.t.BulkWrite(nil, dataTimeout); err != nil { // zero-length packet ends the phase
					return fmt.Errorf("send ZLP: %w", err)
				}
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			_ = s.t.CancelRequest(tx)
			s.t.ClearHalt()
			return err
		}
	}
}

// writerTo returns a writer that appends to *dst.
func writerTo(dst *[]byte) io.Writer { return (*sliceWriter)(dst) }

type sliceWriter []byte

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w = append(*w, p...)
	return len(p), nil
}

func u32(p []uint32, i int) uint32 {
	if i < len(p) {
		return p[i]
	}
	return 0
}
