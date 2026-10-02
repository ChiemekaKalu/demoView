// Package demo reads the CS2 (Source 2) demo container format.
package demo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Magic is the 8-byte signature at the start of every CS2 demo.
var Magic = [8]byte{'P', 'B', 'D', 'E', 'M', 'S', '2', 0}

// ErrBadDemo is wrapped by every error caused by malformed input, as opposed
// to a failure of the underlying reader. Test for it with errors.Is.
var ErrBadDemo = errors.New("demo: invalid demo")

// ErrBadMagic is returned when the input does not start with Magic. It wraps
// ErrBadDemo.
var ErrBadMagic = fmt.Errorf("%w: bad magic (not a CS2 demo)", ErrBadDemo)

// headerSize is the length of the preamble in bytes.
const headerSize = 16

// Header is the fixed 16-byte preamble that precedes the command stream.
type Header struct {
	FileInfoOffset    int32 // byte offset of the trailing DEM_FileInfo command
	SpawnGroupsOffset int32
}

// ReadHeader reads and validates the demo preamble, leaving r positioned at
// the first command. Input shorter than the preamble is an ErrBadDemo error.
func ReadHeader(r io.Reader) (Header, error) {
	var buf [headerSize]byte
	n, err := readFull(r, buf[:])
	switch {
	case err == io.EOF:
		return Header{}, fmt.Errorf("%w: truncated header (%d of %d bytes)", ErrBadDemo, n, headerSize)
	case err != nil:
		return Header{}, fmt.Errorf("demo: reading header: %w", err)
	}
	if [8]byte(buf[:8]) != Magic {
		return Header{}, ErrBadMagic
	}
	return Header{
		FileInfoOffset:    int32(binary.LittleEndian.Uint32(buf[8:12])),
		SpawnGroupsOffset: int32(binary.LittleEndian.Uint32(buf[12:16])),
	}, nil
}

// readFull is like io.ReadFull, except that running out of input reports
// io.EOF even after a partial read. That keeps the end of the input distinct
// from an io.ErrUnexpectedEOF raised by r itself, which is an I/O failure.
func readFull(r io.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil && n < len(buf) {
			return n, err
		}
	}
	return n, nil
}
