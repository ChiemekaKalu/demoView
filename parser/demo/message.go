package demo

import (
	"fmt"
	"io"
	"math"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// MsgType identifies a message inside a packet. Ids come from several enums
// in package pb, among them NET_Messages, SVC_Messages, EBaseGameEvents and
// ECstrike15UserMessages, so compare against those:
// MsgType(pb.SVC_Messages_svc_PacketEntities).
type MsgType uint32

// msgTypeNames are the enums that MsgType.String looks in. Their ids don't
// overlap.
var msgTypeNames = []map[int32]string{
	pb.NET_Messages_name,
	pb.Bidirectional_Messages_name,
	pb.SVC_Messages_name,
	pb.EBaseGameEvents_name,
	pb.ECstrike15UserMessages_name,
}

// String returns the type's enum name, such as "svc_PacketEntities", or
// "MsgType(N)" if no enum in msgTypeNames defines it.
func (t MsgType) String() string {
	if t <= math.MaxInt32 {
		for _, names := range msgTypeNames {
			if s, ok := names[int32(t)]; ok {
				return s
			}
		}
	}
	return "MsgType(" + strconv.FormatUint(uint64(t), 10) + ")"
}

// Message is one message from a packet.
type Message struct {
	Type MsgType
	Body []byte // protobuf-encoded; valid only until the next call to Next
}

// Unmarshal decodes m's body into v. A malformed body gives an error
// wrapping ErrBadDemo.
func (m Message) Unmarshal(v proto.Message) error {
	if err := unmarshalOpts.Unmarshal(m.Body, v); err != nil {
		return fmt.Errorf("%w: %s body: %v", ErrBadDemo, m.Type, err)
	}
	return nil
}

// maxPadBits is the most bits a packet may have left after its last message.
const maxPadBits = 7

// MessageReader reads the messages in Packet, SignonPacket and FullPacket
// frames. A packet's data is a bit stream of messages, each a UBitVar type,
// a varint size and size bytes of body, padded to a whole byte at the end.
//
// The zero value is ready for Reset. Reusing one MessageReader for every
// packet reuses its body buffer.
type MessageReader struct {
	br    bitread.Reader
	buf   []byte  // body storage, reused across messages
	cmd   Command // the packet frame, for error messages
	off   int64
	count int // messages read from this packet
	err   error
}

// Reset prepares m to read the messages in f, which must be a decompressed
// Packet, SignonPacket or FullPacket frame. Reset ignores a FullPacket's
// string tables; unmarshal it into a pb.CDemoFullPacket to get them. A
// payload that doesn't decode gives an error wrapping ErrBadDemo. Any other
// command is a caller bug, and its error does not wrap ErrBadDemo.
func (m *MessageReader) Reset(f Frame) error {
	m.br.Reset(nil)
	m.cmd, m.off, m.count, m.err = f.Cmd, f.Offset, 0, nil
	var data []byte
	switch f.Cmd {
	case CmdPacket, CmdSignonPacket:
		var p pb.CDemoPacket
		m.err = f.Unmarshal(&p)
		data = p.GetData()
	case CmdFullPacket:
		var p pb.CDemoFullPacket
		m.err = f.Unmarshal(&p)
		data = p.GetPacket().GetData()
	default:
		m.err = fmt.Errorf("demo: frame at offset %d: %s frame has no messages", f.Offset, f.Cmd)
	}
	if m.err != nil {
		return m.err
	}
	m.br.Reset(data)
	return nil
}

// Next returns the next message. It returns io.EOF when at most 7 bits are
// left, which are padding; TrailingBits says how many. A message that runs
// past the end of the packet, which includes 8 or more bits left over that
// don't form one, gives an error wrapping ErrBadDemo. After an error, every
// later call returns the same error.
//
// The returned Body is overwritten by the next call to Next. Copy it to keep
// it.
func (m *MessageReader) Next() (Message, error) {
	if m.err != nil {
		return Message{}, m.err
	}
	if m.br.BitsLeft() <= maxPadBits {
		m.err = io.EOF
		return Message{}, io.EOF
	}
	start, left := m.br.Position(), m.br.BitsLeft()
	typ := m.br.ReadUBitVar()
	size := m.br.ReadVarUint32()
	if err := m.br.Err(); err != nil {
		return Message{}, m.failf(start, "bad header with %d bits left: %v", left, err)
	}
	if uint64(size)*8 > uint64(m.br.BitsLeft()) {
		return Message{}, m.failf(start, "type %d: %d-byte body overruns the packet (%d bits left)", typ, size, m.br.BitsLeft())
	}
	if cap(m.buf) < int(size) {
		m.buf = make([]byte, size)
	}
	body := m.buf[:size]
	m.br.ReadBytesInto(body) // cannot fail: the size was checked above
	m.count++
	return Message{Type: MsgType(typ), Body: body}, nil
}

// failf records and returns an ErrBadDemo error for the message at bit start.
func (m *MessageReader) failf(start int, format string, args ...any) error {
	m.err = badFramef(m.off, "%s message %d at bit %d: %s", m.cmd, m.count, start, fmt.Sprintf(format, args...))
	return m.err
}

// TrailingBits returns the number of unread bits in the packet. Once Next has
// returned io.EOF, they are the packet's padding: at most 7.
func (m *MessageReader) TrailingBits() int { return m.br.BitsLeft() }
