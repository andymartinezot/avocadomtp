package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/errcode"
)

// codeOf maps an error to a POSIX-style code.
func codeOf(err error) string { return errcode.Name(errcode.Errno(err)) }

// Error is an error reported by the daemon.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Unwrap lets errors.Is(err, fs.ErrNotExist) and friends work on the client side.
func (e *Error) Unwrap() error {
	switch e.Code {
	case "ENOENT":
		return fs.ErrNotExist
	case "ENODEV":
		return device.ErrNoDevice
	case "EACCES", "EROFS":
		return fs.ErrPermission
	case "ECANCELED":
		return context.Canceled
	}
	return syscall.Errno(errcode.FromName(e.Code))
}

func errResponse(err error) Response {
	return Response{OK: false, Error: err.Error(), Code: codeOf(err)}
}

func (r *Response) err() error {
	if r.OK {
		return nil
	}
	if r.Error == "" {
		r.Error = fmt.Sprintf("daemon error %s", r.Code)
	}
	return &Error{Code: r.Code, Msg: r.Error}
}
