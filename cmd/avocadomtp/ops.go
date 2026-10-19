package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

func (c *client) info(ctx context.Context, w io.Writer) error {
	di := c.r.Device()
	fmt.Fprintf(w, "Device:   %s %s\n", printable(di.Manufacturer), printable(di.Model))
	fmt.Fprintf(w, "Version:  %s\n", printable(di.Version))
	fmt.Fprintf(w, "Serial:   %s\n", printable(di.Serial))
	fmt.Fprintf(w, "USB:      %s\n", printable(di.USB))
	fmt.Fprintf(w, "MTP ext:  %s\n", printable(di.Extension))
	fmt.Fprintf(w, "Via:      %s\n", c.via)
	storages, err := c.fs.Storages(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "Storages:")
	for _, s := range storages {
		fmt.Fprintf(w, "  /%-28s %9s free of %s\n", s.Name, human(int64(s.Info.FreeSpaceInBytes)), human(int64(s.Info.MaxCapacity)))
	}
	return nil
}

func (c *client) ls(ctx context.Context, w io.Writer, p string, long bool) error {
	e, err := c.fs.Stat(ctx, p)
	if err != nil {
		return err
	}
	entries := []*vfs.Entry{e}
	if e.IsDir {
		if entries, err = c.fs.ReadDirEntry(ctx, e); err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name) })
	}
	if !long {
		for _, x := range entries {
			if x.IsDir {
				fmt.Fprintln(w, x.Name+"/")
			} else {
				fmt.Fprintln(w, x.Name)
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, x := range entries {
		switch {
		case x.Storage != nil:
			fmt.Fprintf(tw, "storage\t%s free / %s\t\t%s/\n", human(int64(x.Storage.Info.FreeSpaceInBytes)), human(int64(x.Storage.Info.MaxCapacity)), x.Name)
		case x.IsDir:
			fmt.Fprintf(tw, "dir\t-\t%s\t%s/\n", date(x), x.Name)
		default:
			fmt.Fprintf(tw, "file\t%s\t%s\t%s\n", human(int64(x.Size)), date(x), x.Name)
		}
	}
	return tw.Flush()
}

// printable replaces control characters in text that came from the device,
// so a hostile phone can't send terminal escape sequences. (File and storage
// names are already checked by vfs.ValidName.)
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}

// childPath joins a device-supplied name onto a local folder, refusing
// anything that would land outside it (vfs already hides such names; this
// is the last line before writing to the Mac).
func childPath(dir, name string) (string, error) {
	p := filepath.Join(dir, name)
	if !vfs.ValidName(name) || filepath.Dir(p) != filepath.Clean(dir) {
		return "", fmt.Errorf("refusing to write %q outside %s", name, dir)
	}
	return p, nil
}

func date(e *vfs.Entry) string {
	if e.Modified.IsZero() {
		return "-"
	}
	return e.Modified.Format("2006-01-02 15:04")
}

// pull copies a remote file or folder to local. If local is an existing
// directory the item is placed inside it.
func (c *client) pull(ctx context.Context, remote, local string, recursive bool) error {
	e, err := c.fs.Stat(ctx, remote)
	if err != nil {
		return err
	}
	if e.IsRoot() {
		return errors.New("pick a storage or folder to pull, not /")
	}
	if local == "" {
		local = "."
	}
	if fi, err := os.Stat(local); err == nil && fi.IsDir() {
		if local, err = childPath(local, e.Name); err != nil {
			return err
		}
	}
	return c.pullEntry(ctx, e, local, recursive)
}

func (c *client) pullEntry(ctx context.Context, e *vfs.Entry, local string, recursive bool) error {
	if e.IsDir {
		if !recursive {
			return fmt.Errorf("%s is a folder (use -r)", e.Path)
		}
		if err := os.MkdirAll(local, 0o755); err != nil {
			return err
		}
		children, err := c.fs.ReadDirEntry(ctx, e)
		if err != nil {
			return err
		}
		for _, ch := range children {
			dst, err := childPath(local, ch.Name)
			if err != nil {
				return err
			}
			if err := c.pullEntry(ctx, ch, dst, true); err != nil {
				return err
			}
		}
		return nil
	}

	tmp := local + ".avocadomtp-part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	pr := newProgress(e.Path, int64(e.Size))
	err = c.r.Download(ctx, e.Handle, pr.writer(f))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("pull %s: %w", e.Path, err)
	}
	if err := os.Rename(tmp, local); err != nil {
		os.Remove(tmp)
		return err
	}
	if !e.Modified.IsZero() {
		_ = os.Chtimes(local, e.Modified, e.Modified)
	}
	pr.finish("pulled")
	return nil
}

// skipLocal ignores macOS metadata files that should never land on the phone.
func skipLocal(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// push copies local files or folders into the remote folder dir.
func (c *client) push(ctx context.Context, locals []string, dir string, recursive bool) error {
	target, err := c.fs.Stat(ctx, dir)
	if err != nil {
		return err
	}
	if !target.IsDir || target.IsRoot() {
		return fmt.Errorf("%s: destination must be a folder inside a storage", target.Path)
	}
	for _, l := range locals {
		if err := c.pushPath(ctx, filepath.Clean(l), target, recursive); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) child(ctx context.Context, dir *vfs.Entry, name string) (*vfs.Entry, error) {
	children, err := c.fs.ReadDirEntry(ctx, dir)
	if err != nil {
		return nil, err
	}
	for _, ch := range children {
		if ch.Name == name {
			return ch, nil
		}
	}
	return nil, nil
}

func (c *client) pushPath(ctx context.Context, local string, dir *vfs.Entry, recursive bool) error {
	fi, err := os.Stat(local)
	if err != nil {
		return err
	}
	name := fi.Name()
	existing, err := c.child(ctx, dir, name)
	if err != nil {
		return err
	}
	remotePath := path.Join(dir.Path, name)

	if fi.IsDir() {
		if !recursive {
			return fmt.Errorf("%s is a folder (use -r)", local)
		}
		sub := existing
		if sub != nil && !sub.IsDir {
			return fmt.Errorf("%s exists and is a file", remotePath)
		}
		if sub == nil {
			h, err := c.r.Mkdir(ctx, dir.StorageID, dir.Handle, name)
			c.fs.Invalidate(dir.StorageID, dir.Handle)
			if err != nil {
				return fmt.Errorf("mkdir %s: %w", remotePath, err)
			}
			sub = &vfs.Entry{Path: remotePath, Name: name, StorageID: dir.StorageID, Handle: h, IsDir: true}
		}
		ents, err := os.ReadDir(local)
		if err != nil {
			return err
		}
		for _, de := range ents {
			if skipLocal(de.Name()) {
				continue
			}
			if err := c.pushPath(ctx, filepath.Join(local, de.Name()), sub, true); err != nil {
				return err
			}
		}
		return nil
	}

	if existing != nil {
		if existing.IsDir {
			return fmt.Errorf("%s exists and is a folder", remotePath)
		}
		// MTP cannot overwrite in place: replace the old file.
		if err := c.r.Delete(ctx, existing.Handle); err != nil {
			return fmt.Errorf("replace %s: %w", remotePath, err)
		}
	}
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	pr := newProgress(remotePath, fi.Size())
	_, err = c.r.Upload(ctx, dir.StorageID, dir.Handle, name, fi.Size(), fi.ModTime(), pr.reader(f))
	c.fs.Invalidate(dir.StorageID, dir.Handle)
	if err != nil {
		return fmt.Errorf("push %s: %w", remotePath, err)
	}
	pr.finish("pushed")
	return nil
}

func (c *client) rm(ctx context.Context, w io.Writer, p string, recursive bool) error {
	e, err := c.fs.Stat(ctx, p)
	if err != nil {
		return err
	}
	if e.IsRoot() || e.Storage != nil {
		return fmt.Errorf("refusing to delete %s", e.Path)
	}
	if e.IsDir && !recursive {
		return fmt.Errorf("%s is a folder (use -r)", e.Path)
	}
	if err := c.r.Delete(ctx, e.Handle); err != nil {
		return fmt.Errorf("rm %s: %w", e.Path, err)
	}
	c.invalidateParent(ctx, e.Path)
	if e.IsDir {
		c.fs.Reset() // cached listings below it are gone too
	}
	fmt.Fprintf(w, "removed %s\n", e.Path)
	return nil
}

func (c *client) mkdir(ctx context.Context, p string, parents bool) error {
	p = vfs.Clean(p)
	if e, err := c.fs.Stat(ctx, p); err == nil {
		if parents && e.IsDir {
			return nil
		}
		return fmt.Errorf("%s already exists", p)
	} else if !isNotExist(err) {
		return err
	}
	par, err := c.parentOf(ctx, p)
	if isNotExist(err) && parents && path.Dir(p) != "/" {
		if err := c.mkdir(ctx, path.Dir(p), true); err != nil {
			return err
		}
		par, err = c.parentOf(ctx, p)
	}
	if err != nil {
		return err
	}
	if par.IsRoot() {
		return errors.New("cannot create a storage; create folders inside one")
	}
	_, err = c.r.Mkdir(ctx, par.StorageID, par.Handle, path.Base(p))
	c.fs.Invalidate(par.StorageID, par.Handle)
	return err
}

// mv renames and/or moves src. If dst is an existing folder, src goes inside it.
func (c *client) mv(ctx context.Context, src, dst string) error {
	s, err := c.fs.Stat(ctx, src)
	if err != nil {
		return err
	}
	if s.IsRoot() || s.Storage != nil {
		return fmt.Errorf("cannot move %s", s.Path)
	}
	srcDir, err := c.parentOf(ctx, s.Path)
	if err != nil {
		return err
	}
	var dstDir *vfs.Entry
	newName := s.Name
	if d, err := c.fs.Stat(ctx, dst); err == nil {
		if !d.IsDir || d.IsRoot() {
			return fmt.Errorf("%s already exists", d.Path)
		}
		dstDir = d
	} else if isNotExist(err) {
		if dstDir, err = c.parentOf(ctx, dst); err != nil {
			return err
		}
		newName = path.Base(vfs.Clean(dst))
	} else {
		return err
	}
	if dstDir.IsRoot() {
		return errors.New("destination must be inside a storage")
	}
	defer c.fs.Invalidate(srcDir.StorageID, srcDir.Handle)
	defer c.fs.Invalidate(dstDir.StorageID, dstDir.Handle)
	if dstDir.StorageID != srcDir.StorageID || dstDir.Handle != srcDir.Handle {
		if err := c.r.Move(ctx, s.Handle, dstDir.StorageID, dstDir.Handle); err != nil {
			return fmt.Errorf("move %s: %w", s.Path, err)
		}
	}
	if newName != s.Name {
		if err := c.r.Rename(ctx, s.Handle, newName); err != nil {
			return fmt.Errorf("rename %s: %w", s.Path, err)
		}
	}
	return nil
}
