// Package errcode maps MTP, USB and file errors to POSIX error codes.
package errcode

import (
	"context"
	"errors"
	"io/fs"
	"syscall"

	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/ptp"
)

// Errno returns the POSIX error number that best describes err.
func Errno(err error) syscall.Errno {
	var errno syscall.Errno
	var re *mtp.RespError
	switch {
	case errors.As(err, &errno):
		return errno
	case errors.Is(err, device.ErrNoDevice):
		return syscall.ENODEV
	case errors.Is(err, fs.ErrNotExist):
		return syscall.ENOENT
	case errors.Is(err, fs.ErrPermission):
		return syscall.EACCES
	case errors.Is(err, context.Canceled):
		return syscall.ECANCELED
	case errors.As(err, &re):
		switch re.Code {
		case ptp.RespInvalidObjectHandle, ptp.RespInvalidParentObject:
			return syscall.ENOENT
		case ptp.RespStoreFull:
			return syscall.ENOSPC
		case ptp.RespStoreReadOnly:
			return syscall.EROFS
		case ptp.RespObjectWriteProtected, ptp.RespAccessDenied:
			return syscall.EACCES
		case ptp.RespDeviceBusy:
			return syscall.EBUSY
		case ptp.RespOperationNotSupported:
			return syscall.ENOTSUP
		case ptp.RespInvalidStorageID, ptp.RespStoreNotAvailable:
			return syscall.ENODEV
		case ptp.RespInvalidParameter, ptp.RespParameterNotSupported:
			return syscall.EINVAL
		}
	}
	return syscall.EIO
}

var names = map[syscall.Errno]string{
	syscall.ENOENT: "ENOENT", syscall.ENOSPC: "ENOSPC", syscall.EROFS: "EROFS", syscall.EACCES: "EACCES",
	syscall.EBUSY: "EBUSY", syscall.ENOTSUP: "ENOTSUP", syscall.ENODEV: "ENODEV", syscall.EINVAL: "EINVAL",
	syscall.ECANCELED: "ECANCELED", syscall.EIO: "EIO", syscall.EEXIST: "EEXIST", syscall.ENOTEMPTY: "ENOTEMPTY",
	syscall.ENOTDIR: "ENOTDIR", syscall.EISDIR: "EISDIR",
}

// Name returns the symbolic name of an errno ("ENOENT"), or "EIO".
func Name(e syscall.Errno) string {
	if n, ok := names[e]; ok {
		return n
	}
	return "EIO"
}

// FromName is the inverse of Name.
func FromName(name string) syscall.Errno {
	for e, n := range names {
		if n == name {
			return e
		}
	}
	return syscall.EIO
}
