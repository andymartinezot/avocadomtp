// Package daemon runs a background process that owns the USB connection to
// the phone and serves file operations over a Unix socket, so the CLI and the
// FSKit extension (one volume per storage) can share one MTP session.
//
// # Wire protocol
//
// Every message is a frame: a 4-byte big-endian length followed by that many
// bytes. A connection carries any number of request/response exchanges, one
// at a time.
//
//   - A request is one JSON frame (Request).
//   - "upload" requests are followed by data frames holding the file content
//     and a zero-length frame that ends the stream.
//   - A response is one JSON frame (Response).
//   - A successful "read" response is followed by exactly one data frame
//     with Response.N bytes.
//   - A successful "download" response is followed by data frames, a
//     zero-length frame, and a second JSON frame (Response) with the final
//     status of the transfer.
//   - A successful "watch" response is followed by a stream of JSON frames,
//     each a complete activity.State snapshot (phone, mounts, transfers,
//     speeds), sent on every change and once a second while transferring.
//     The connection is dedicated to the stream; close it to stop.
//
// Errors carry a POSIX-style Code (ENOENT, ENOSPC, EACCES, EROFS, EBUSY,
// ENOTSUP, ENODEV, EINVAL, EIO) so file system clients can map them directly.
package daemon

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/remote"
)

// Operations.
const (
	OpStatus   = "status"
	OpList     = "list"
	OpStat     = "stat"
	OpRead     = "read"
	OpDownload = "download"
	OpUpload   = "upload"
	OpMkdir    = "mkdir"
	OpDelete   = "delete"
	OpRename   = "rename"
	OpMove     = "move"
	OpWatch    = "watch" // stream activity snapshots (menu bar)
	OpEject    = "eject" // unmount one storage (Request.Storage)
	OpMount    = "mount" // mount all storages again, including ejected ones
	// OpRenameVolume names a storage's volume (Request.Storage, Request.Name;
	// an empty name restores the default) and remounts it.
	OpRenameVolume = "renameVolume"
)

// Request is a client request. Handle 0xFFFFFFFF (with Storage) is a storage root.
type Request struct {
	Op      string `json:"op"`
	Storage uint32 `json:"storage,omitempty"`
	Handle  uint32 `json:"handle,omitempty"`
	Parent  uint32 `json:"parent,omitempty"`
	Name    string `json:"name,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Length  int    `json:"length,omitempty"`
	Size    int64  `json:"size,omitempty"`
	MTime   int64  `json:"mtime,omitempty"` // Unix seconds
}

// Response is the daemon's answer.
type Response struct {
	OK        bool               `json:"ok"`
	Error     string             `json:"error,omitempty"`
	Code      string             `json:"code,omitempty"`
	Connected bool               `json:"connected,omitempty"`
	Device    *remote.DeviceInfo `json:"device,omitempty"`
	Storages  []Storage          `json:"storages,omitempty"`
	Entries   []Entry            `json:"entries,omitempty"`
	Entry     *Entry             `json:"entry,omitempty"`
	Handle    uint32             `json:"handle,omitempty"`
	N         int                `json:"n,omitempty"`
	Stats     *Stats             `json:"stats,omitempty"`
	Mounts    []activity.Mount   `json:"mounts,omitempty"`
}

// Stats are cumulative transfer counters since the phone connected. Clients
// derive transfer speed from successive status calls.
type Stats struct {
	BytesIn  uint64 `json:"bytesIn"`  // received from the phone
	BytesOut uint64 `json:"bytesOut"` // sent to the phone
	Ops      uint64 `json:"ops"`      // MTP transactions
}

// Storage is a volume on the phone (internal memory, SD card).
type Storage struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"` // unique, slash-free: use as volume name
	Description string `json:"description"`
	VolumeLabel string `json:"volumeLabel,omitempty"`
	Type        uint16 `json:"type,omitempty"` // MTP storage type (ptp.StorageFixedRAM, …)
	Capacity    uint64 `json:"capacity"`
	Free        uint64 `json:"free"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

// Entry is a file or folder.
type Entry struct {
	Handle  uint32 `json:"handle"`
	Storage uint32 `json:"storage"`
	Parent  uint32 `json:"parent"`
	Name    string `json:"name"`
	Dir     bool   `json:"dir,omitempty"`
	Size    uint64 `json:"size"`
	MTime   int64  `json:"mtime,omitempty"`
}

const (
	maxFrame  = 32 << 20
	dataChunk = 1 << 20
	// MaxRead is the largest "read" the daemon serves in one request.
	MaxRead = 16 << 20
)

// DefaultSocketPath is $AVOCADOMTP_SOCKET or ~/Library/Application Support/AvocadoMTP/avocado.sock.
func DefaultSocketPath() string {
	if p := os.Getenv("AVOCADOMTP_SOCKET"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "AvocadoMTP", "avocadomtp.sock")
}

func writeFrame(w *bufio.Writer, b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

// readFrame reads one frame, reusing buf when it is large enough.
func readFrame(r *bufio.Reader, buf []byte) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, fmt.Errorf("daemon: frame too large (%d bytes)", n)
	}
	if cap(buf) < int(n) {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	_, err := io.ReadFull(r, buf)
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return buf, err
}

func writeJSON(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := writeFrame(w, b); err != nil {
		return err
	}
	return w.Flush()
}

func readJSON(r *bufio.Reader, v any) error {
	b, err := readFrame(r, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// frameWriter turns a byte stream into data frames.
type frameWriter struct{ w *bufio.Writer }

func (fw frameWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := min(len(p), dataChunk)
		if n == 0 {
			break
		}
		if err := writeFrame(fw.w, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// end writes the zero-length terminator and flushes.
func (fw frameWriter) end() error {
	if err := writeFrame(fw.w, nil); err != nil {
		return err
	}
	return fw.w.Flush()
}

// frameReader turns data frames back into a byte stream, stopping at the terminator.
type frameReader struct {
	r    *bufio.Reader
	buf  []byte
	cur  []byte
	done bool
}

func (fr *frameReader) Read(p []byte) (int, error) {
	for len(fr.cur) == 0 {
		if fr.done {
			return 0, io.EOF
		}
		b, err := readFrame(fr.r, fr.buf)
		if err != nil {
			return 0, err
		}
		fr.buf = b[:0]
		if len(b) == 0 {
			fr.done = true
			return 0, io.EOF
		}
		fr.cur = b
	}
	n := copy(p, fr.cur)
	fr.cur = fr.cur[n:]
	return n, nil
}

// drain consumes the rest of the stream so the connection stays in sync.
func (fr *frameReader) drain() error {
	_, err := io.Copy(io.Discard, fr)
	return err
}
