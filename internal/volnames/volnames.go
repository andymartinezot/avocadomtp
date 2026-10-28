// Package volnames stores the names the user gives a phone's volumes. They
// live on the Mac, keyed by phone and storage ID; nothing is written to the
// phone. The daemon is the only writer; everything that names volumes reads.
package volnames

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// maxFile bounds what Load reads: the file is ours, but keep it cheap.
const maxFile = 1 << 20

var mu sync.Mutex // serializes read-modify-write within this process

// Path is $AVOCADOMTP_VOLUME_NAMES or ~/Library/Application Support/AvocadoMTP/volume-names.json.
func Path() string {
	if p := os.Getenv("AVOCADOMTP_VOLUME_NAMES"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "AvocadoMTP", "volume-names.json")
}

// Key identifies a phone: its serial, or its model when it reports none.
func Key(serial, model string) string {
	if serial != "" {
		return "serial:" + serial
	}
	return "model:" + model
}

// file is phone key → storage ID (decimal) → name.
type file map[string]map[string]string

func read() (file, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return file{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("%s is too large", Path())
	}
	f := file{}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("read %s: %w", Path(), err)
	}
	return f, nil
}

// Load returns the custom names of one phone's storages. A missing or
// unreadable file means no custom names.
func Load(phone string) map[uint32]string {
	mu.Lock()
	defer mu.Unlock()
	f, err := read()
	if err != nil {
		return nil
	}
	out := map[uint32]string{}
	for id, name := range f[phone] {
		if n, err := strconv.ParseUint(id, 10, 32); err == nil {
			out[uint32(n)] = name
		}
	}
	return out
}

// Set stores a name for a storage; an empty name removes it (back to the
// default). The caller validates the name (vfs.CheckVolumeName).
func Set(phone string, storage uint32, name string) error {
	mu.Lock()
	defer mu.Unlock()
	f, err := read()
	if err != nil {
		return err
	}
	id := strconv.FormatUint(uint64(storage), 10)
	if name == "" {
		delete(f[phone], id)
		if len(f[phone]) == 0 {
			delete(f, phone)
		}
	} else {
		if f[phone] == nil {
			f[phone] = map[string]string{}
		}
		f[phone][id] = name
	}
	if len(f) == 0 {
		if err := os.Remove(Path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	tmp := Path() + ".new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
