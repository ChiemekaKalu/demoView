package demo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Command is a demo command type (EDemoCommands in demo.proto).
type Command uint32

// Demo command types.
const (
	CmdStop Command = iota
	CmdFileHeader
	CmdFileInfo
	CmdSyncTick
	CmdSendTables
	CmdClassInfo
	CmdStringTables
	CmdPacket
	CmdSignonPacket
	CmdConsoleCmd
	CmdCustomData
	CmdCustomDataCallbacks
	CmdUserCmd
	CmdFullPacket
	CmdSaveGame
	CmdSpawnGroups
	CmdAnimationData
	CmdAnimationHeader
	CmdRecovery
)

var commandNames = [...]string{
	CmdStop:                "Stop",
	CmdFileHeader:          "FileHeader",
	CmdFileInfo:            "FileInfo",
	CmdSyncTick:            "SyncTick",
	CmdSendTables:          "SendTables",
	CmdClassInfo:           "ClassInfo",
	CmdStringTables:        "StringTables",
	CmdPacket:              "Packet",
	CmdSignonPacket:        "SignonPacket",
	CmdConsoleCmd:          "ConsoleCmd",
	CmdCustomData:          "CustomData",
	CmdCustomDataCallbacks: "CustomDataCallbacks",
	CmdUserCmd:             "UserCmd",
	CmdFullPacket:          "FullPacket",
	CmdSaveGame:            "SaveGame",
	CmdSpawnGroups:         "SpawnGroups",
	CmdAnimationData:       "AnimationData",
	CmdAnimationHeader:     "AnimationHeader",
	CmdRecovery:            "Recovery",
}

// String returns the command's name, or "Command(N)" for an unknown value.
func (c Command) String() string {
	if c < Command(len(commandNames)) {
		return commandNames[c]
	}
	return "Command(" + strconv.FormatUint(uint64(c), 10) + ")"
}

// MaxPayloadSize caps a frame's payload. A larger size is treated as
// corruption rather than allocated.
const MaxPayloadSize = 64 << 20

const (
	compressedFlag = 0x40     // command bit marking a snappy payload (DEM_IsCompressed)
	maxVarintLen   = 5        // longest encoding of a 32-bit varint
	readBufSize    = 64 << 10 // input buffer size
)

var errVarintTooLong = errors.New("varint too long")

// Frame is one command in the demo stream.
type Frame struct {
	Cmd        Command
	Compressed bool   // Payload is snappy-compressed
	Tick       int32  // -1 for signon frames
	Offset     int64  // byte offset of the frame from the start of the input
	Payload    []byte // raw payload, valid only until the next call to Next
}

// Reader streams frames from a demo. It holds at most one frame's payload in
// memory at a time.
type Reader struct {
	br  *bufio.Reader
	hdr Header
	off int64  // bytes consumed from the start of the input
	buf []byte // payload storage, reused across frames
	err error  // sticky error from Next
}

// NewReader reads and validates the demo header from r and returns a Reader
// positioned at the first frame. It buffers r itself.
func NewReader(r io.Reader) (*Reader, error) {
	br := bufio.NewReaderSize(r, readBufSize)
	h, err := ReadHeader(br)
	if err != nil {
		return nil, err
	}
	return &Reader{br: br, hdr: h, off: headerSize}, nil
}

// Header returns the demo preamble read by NewReader.
func (r *Reader) Header() Header { return r.hdr }

// Next returns the next frame, including Stop and anything after it. It
// returns io.EOF only when the input ends cleanly between frames. Malformed
// or truncated frames give errors wrapping ErrBadDemo; errors from the
// underlying reader are wrapped as they are. After an error, every later call
// returns the same error.
//
// The returned Payload shares memory with the Reader and is overwritten by
// the next call to Next. Copy it to keep it.
func (r *Reader) Next() (Frame, error) {
	if r.err != nil {
		return Frame{}, r.err
	}
	f, err := r.next()
	if err != nil {
		r.err = err
		return Frame{}, err
	}
	return f, nil
}

func (r *Reader) next() (Frame, error) {
	f := Frame{Offset: r.off}
	cmd, err := r.varint()
	if err == io.EOF && r.off == f.Offset {
		return Frame{}, io.EOF
	}
	if err != nil {
		return Frame{}, fieldErr(f.Offset, "command", err)
	}
	tick, err := r.varint()
	if err != nil {
		return Frame{}, fieldErr(f.Offset, "tick", err)
	}
	size, err := r.varint()
	if err != nil {
		return Frame{}, fieldErr(f.Offset, "size", err)
	}
	if size > MaxPayloadSize {
		return Frame{}, badFramef(f.Offset, "payload size %d exceeds limit %d", size, MaxPayloadSize)
	}

	if cap(r.buf) < int(size) {
		r.buf = make([]byte, size)
	}
	f.Payload = r.buf[:size]
	n, err := readFull(r.br, f.Payload)
	r.off += int64(n)
	switch {
	case err == io.EOF:
		return Frame{}, badFramef(f.Offset, "truncated payload (%d of %d bytes)", n, size)
	case err != nil:
		return Frame{}, ioErr(f.Offset, err)
	}

	f.Cmd = Command(cmd &^ compressedFlag)
	f.Compressed = cmd&compressedFlag != 0
	f.Tick = int32(tick)
	return f, nil
}

// varint reads a protobuf-style base-128 varint of at most 5 bytes. Bits past
// the 32nd are dropped. It returns the underlying reader's error unchanged.
func (r *Reader) varint() (uint32, error) {
	var x uint32
	for i := range maxVarintLen {
		b, err := r.br.ReadByte()
		if err != nil {
			return 0, err
		}
		r.off++
		x |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return x, nil
		}
	}
	return 0, errVarintTooLong
}

// fieldErr classifies an error from reading one of a frame's varint fields.
func fieldErr(off int64, field string, err error) error {
	switch {
	case err == io.EOF:
		return badFramef(off, "truncated %s", field)
	case errors.Is(err, errVarintTooLong):
		return badFramef(off, "%s varint longer than %d bytes", field, maxVarintLen)
	}
	return ioErr(off, err)
}

// badFramef returns an ErrBadDemo error for the frame at off.
func badFramef(off int64, format string, args ...any) error {
	return fmt.Errorf("%w: frame at offset %d: %s", ErrBadDemo, off, fmt.Sprintf(format, args...))
}

// ioErr wraps a failure of the underlying reader. It is not ErrBadDemo.
func ioErr(off int64, err error) error {
	return fmt.Errorf("demo: reading frame at offset %d: %w", off, err)
}
