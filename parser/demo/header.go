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

// ErrBadMagic is returned when the input does not start with Magic.
var ErrBadMagic = errors.New("demo: not a CS2 demo (bad magic)")

// Header is the fixed 16-byte preamble that precedes the command stream.
type Header struct {
	FileInfoOffset    int32 // byte offset of the trailing DEM_FileInfo command
	SpawnGroupsOffset int32
}

// ReadHeader reads and validates the demo preamble, leaving r positioned at
// the first command.
func ReadHeader(r io.Reader) (Header, error) {
	var buf [16]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
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
