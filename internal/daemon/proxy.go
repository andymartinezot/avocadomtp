package daemon

import (
	"context"
	"io"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
)

// Remote returns a remote.Remote that shares the server's phone connection,
// for in-process users such as the Finder mounts. Transfers are chunked like
// socket requests, so mounts and CLI clients interleave.
func (s *Server) Remote() remote.Remote { return phoneRemote{s} }

type phoneRemote struct{ s *Server }

func (r phoneRemote) with(fn func(Phone) error) error {
	p, release, err := r.s.acquire()
	if err != nil {
		return err
	}
	r.s.touch()
	err = fn(p)
	release()
	r.s.touch()
	if p.Err() != nil {
		r.s.drop(p)
	}
	return err
}

func (r phoneRemote) Device() remote.DeviceInfo {
	var d remote.DeviceInfo
	_ = r.with(func(p Phone) error { d = p.Device(); return nil })
	return d
}

func (r phoneRemote) StorageIDs(ctx context.Context) (ids []uint32, err error) {
	err = r.with(func(p Phone) error { ids, err = p.StorageIDs(ctx); return err })
	return
}

func (r phoneRemote) StorageInfo(ctx context.Context, id uint32) (info *ptp.StorageInfo, err error) {
	err = r.with(func(p Phone) error { info, err = p.StorageInfo(ctx, id); return err })
	return
}

func (r phoneRemote) List(ctx context.Context, storage, parent uint32) (objs []*mtp.Object, err error) {
	err = r.with(func(p Phone) error { objs, err = p.List(ctx, storage, parent); return err })
	return
}

func (r phoneRemote) Download(ctx context.Context, h uint32, w io.Writer) error {
	return r.with(func(p Phone) error { return download(ctx, p, h, w) })
}

func (r phoneRemote) ReadAt(ctx context.Context, h uint32, b []byte, off int64) (n int, err error) {
	err = r.with(func(p Phone) error { n, err = p.ReadAt(ctx, h, b, off); return err })
	return
}

func (r phoneRemote) Upload(ctx context.Context, storage, parent uint32, name string, size int64, mtime time.Time, rd io.Reader) (h uint32, err error) {
	req := &Request{Op: OpUpload, Storage: storage, Parent: parent, Name: name, Size: size, MTime: mtime.Unix()}
	err = r.with(func(p Phone) error { h, err = uploadChunked(ctx, p, req, rd); return err })
	return
}

func (r phoneRemote) Mkdir(ctx context.Context, storage, parent uint32, name string) (h uint32, err error) {
	err = r.with(func(p Phone) error { h, err = p.Mkdir(ctx, storage, parent, name); return err })
	return
}

func (r phoneRemote) Delete(ctx context.Context, h uint32) error {
	return r.with(func(p Phone) error { return p.Delete(ctx, h) })
}

func (r phoneRemote) Rename(ctx context.Context, h uint32, name string) error {
	return r.with(func(p Phone) error { return p.Rename(ctx, h, name) })
}

func (r phoneRemote) Move(ctx context.Context, h, storage, parent uint32) error {
	return r.with(func(p Phone) error { return p.Move(ctx, h, storage, parent) })
}

func (r phoneRemote) Healthy() error { return nil }
func (r phoneRemote) Close()         {}
