// Package remote defines the file operations shared by a direct USB session
// and a connection to the avocadomtp daemon.
package remote

import (
	"context"
	"io"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/vfs"
	"github.com/andymartinezot/avocadomtp/internal/volnames"
)

// DeviceInfo describes the connected phone.
type DeviceInfo struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Version      string `json:"version"`
	Serial       string `json:"serial"`
	USB          string `json:"usb"`
	Extension    string `json:"extension"`
}

// Remote is a phone reachable either directly over USB or through the daemon.
type Remote interface {
	vfs.Backend // StorageIDs, StorageInfo, List

	Device() DeviceInfo
	Download(ctx context.Context, h uint32, w io.Writer) error
	ReadAt(ctx context.Context, h uint32, p []byte, off int64) (int, error)
	Upload(ctx context.Context, storage, parent uint32, name string, size int64, mtime time.Time, r io.Reader) (uint32, error)
	Mkdir(ctx context.Context, storage, parent uint32, name string) (uint32, error)
	Delete(ctx context.Context, h uint32) error
	Rename(ctx context.Context, h uint32, name string) error
	Move(ctx context.Context, h, storage, parent uint32) error

	// Healthy restores the connection after an aborted transfer.
	Healthy() error
	Close()
}

// NewFS returns a path resolver for r that names volumes the way every part
// of AvocadoMTP shows them: the user's names first, then the phone's.
func NewFS(r Remote) *vfs.FS {
	d := r.Device()
	v := vfs.New(r)
	v.DeviceName = d.Model
	v.Custom = volnames.Load(volnames.Key(d.Serial, d.Model))
	return v
}
