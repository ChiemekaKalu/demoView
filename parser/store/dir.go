package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Dir is a Store backed by a local directory. A key maps to the file at that
// relative path under the root.
type Dir struct {
	root string
}

// NewDir returns a Store rooted at root. The directory is created on the
// first Put if it does not exist.
func NewDir(root string) *Dir {
	return &Dir{root: root}
}

func (d *Dir) path(key string) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	return filepath.Join(d.root, filepath.FromSlash(key)), nil
}

// Open implements Store.
func (d *Dir) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, dirErr(key, err)
	}
	info, err := f.Stat()
	if err == nil && info.IsDir() {
		err = fs.ErrNotExist
	}
	if err != nil {
		f.Close()
		return nil, 0, dirErr(key, err)
	}
	return f, info.Size(), nil
}

// Stat implements Store.
func (d *Dir) Stat(_ context.Context, key string) (int64, error) {
	p, err := d.path(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		err = fs.ErrNotExist
	}
	if err != nil {
		return 0, dirErr(key, err)
	}
	return info.Size(), nil
}

// Put implements Store. It writes to a temporary file and renames it into
// place, so a reader never sees a partial object.
func (d *Dir) Put(_ context.Context, key string, data []byte) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return dirErr(key, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return dirErr(key, err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return dirErr(key, err)
	}
	return nil
}

func dirErr(key string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("store: %s: %w", key, err)
}
