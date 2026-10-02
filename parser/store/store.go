// Package store reads demos from and writes parse output to object storage:
// R2 over the S3 API in production, or a local directory for development
// and tests. Keys are slash-separated, as in the spec's R2 layout, for
// example "demos/{match_id}.dem" and "parsed/{match_id}/match.json".
package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// ErrNotFound is wrapped by every error for a key that does not exist.
// Test for it with errors.Is.
var ErrNotFound = errors.New("store: object not found")

// ErrInvalidKey is wrapped by every error for a malformed key.
var ErrInvalidKey = errors.New("store: invalid key")

// Store is object storage addressed by key.
type Store interface {
	// Open streams the object at key and reports its size in bytes. The
	// caller must close the reader. Nothing is buffered beyond what the
	// caller reads.
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)

	// Stat reports the size of the object at key without reading it.
	Stat(ctx context.Context, key string) (int64, error)

	// Put writes data to key, replacing any existing object.
	Put(ctx context.Context, key string, data []byte) error
}

// checkKey accepts unrooted, slash-separated keys without empty, "." or ".."
// elements. Backslashes and colons are refused too, so a key can never
// escape a Dir root on Windows.
func checkKey(key string) error {
	if key == "." || !fs.ValidPath(key) || strings.ContainsAny(key, `\:`) {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return nil
}

// contentType is the MIME type stored with an object, chosen by extension.
func contentType(key string) string {
	if path.Ext(key) == ".json" {
		return "application/json"
	}
	return "application/octet-stream"
}
