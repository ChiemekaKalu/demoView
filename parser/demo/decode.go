package demo

import (
	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"
)

// Decoder reads frames from a Reader and decompresses their payloads. Like
// the Reader, it holds one payload at a time and reuses its buffer.
type Decoder struct {
	r   *Reader
	buf []byte // decompressed payload storage, reused across frames
	err error  // sticky error from Next
}

// NewDecoder returns a Decoder that reads frames from r.
func NewDecoder(r *Reader) *Decoder { return &Decoder{r: r} }

// Next is like Reader.Next, except that the returned Payload is always
// decompressed. Compressed still reports how it was stored. A payload that
// isn't valid snappy, or that decompresses to more than MaxPayloadSize bytes,
// gives an error wrapping ErrBadDemo. After an error, every later call
// returns the same error.
//
// The returned Payload is overwritten by the next call to Next. Copy it to
// keep it.
func (d *Decoder) Next() (Frame, error) {
	if d.err != nil {
		return Frame{}, d.err
	}
	f, err := d.r.Next()
	if err == nil && f.Compressed {
		f.Payload, err = d.decompress(f)
	}
	if err != nil {
		d.err = err
		return Frame{}, err
	}
	return f, nil
}

// decompress decodes f's snappy payload into d.buf.
func (d *Decoder) decompress(f Frame) ([]byte, error) {
	n, err := snappy.DecodedLen(f.Payload)
	if err != nil {
		return nil, badFramef(f.Offset, "%s payload: snappy: %v", f.Cmd, err)
	}
	if n > MaxPayloadSize {
		return nil, badFramef(f.Offset, "%s payload decompresses to %d bytes, over limit %d", f.Cmd, n, MaxPayloadSize)
	}
	if cap(d.buf) < n {
		d.buf = make([]byte, n)
	}
	p, err := snappy.Decode(d.buf[:n], f.Payload)
	if err != nil {
		return nil, badFramef(f.Offset, "%s payload: snappy: %v", f.Cmd, err)
	}
	return p, nil
}

// unmarshalOpts decodes Valve's proto2 messages. AllowPartial skips the
// required-field check. The only required field, CDemoFileHeader's
// demo_file_stamp, repeats the magic that ReadHeader already checked, and
// the parser never reads it, so a demo that omits it is still usable.
// Malformed wire data is still an error.
var unmarshalOpts = proto.UnmarshalOptions{AllowPartial: true}

// Unmarshal decodes f's payload into m. The payload must be decompressed, as
// Decoder.Next returns it. A malformed payload gives an error wrapping
// ErrBadDemo.
func (f Frame) Unmarshal(m proto.Message) error {
	if err := unmarshalOpts.Unmarshal(f.Payload, m); err != nil {
		return badFramef(f.Offset, "%s payload: %v", f.Cmd, err)
	}
	return nil
}
