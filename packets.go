package quictracker

import (
	"bytes"
	"encoding/binary"
	"io"
	"github.com/davecgh/go-spew/spew"
	"unsafe"
	"fmt"
	"encoding/hex"
)

type Packet interface {
	Header() Header
	ShouldBeAcknowledged() bool // Indicates whether or not the packet type should be acknowledged by the mean of sending an ack
	EncodeHeader() []byte
	EncodePayload() []byte
	Encode([]byte) []byte
	Pointer() unsafe.Pointer
	PNSpace() PNSpace
	EncryptionLevel() EncryptionLevel
	ShortString() string
}

type abstractPacket struct {
	header Header
}
func (p abstractPacket) Header() Header {
	return p.header
}
func (p abstractPacket) EncodeHeader() []byte {
	return p.header.Encode()
}
func (p abstractPacket) Encode(payload []byte) []byte {
	buffer := new(bytes.Buffer)
	buffer.Write(p.EncodeHeader())
	buffer.Write(payload)
	return buffer.Bytes()
}
func (p abstractPacket) ShortString() string {
	return fmt.Sprintf("{type=%s, number=%d}", p.header.PacketType().String(), p.header.PacketNumber())
}

type VersionNegotiationPacket struct {
	abstractPacket
	UnusedField uint8
	Version        uint32
	DestinationCID ConnectionID
	SourceCID      ConnectionID
	SupportedVersions []SupportedVersion
}
type SupportedVersion uint32
func (v SupportedVersion) String() string {
	return hex.EncodeToString(Uint32ToBEBytes(uint32(v)))
}
func (p *VersionNegotiationPacket) ShouldBeAcknowledged() bool { return false }
func (p *VersionNegotiationPacket) EncodePayload() []byte {
	buffer := new(bytes.Buffer)
	buffer.WriteByte(p.UnusedField & 0x80)
	binary.Write(buffer, binary.BigEndian, p.Version)
	buffer.WriteByte((p.DestinationCID.CIDL() << 4) | p.SourceCID.CIDL())
	binary.Write(buffer, binary.BigEndian, p.DestinationCID)
	binary.Write(buffer, binary.BigEndian, p.SourceCID)
	for _, version := range p.SupportedVersions {
		binary.Write(buffer, binary.BigEndian, version)
	}
	return buffer.Bytes()
}
func (p *VersionNegotiationPacket) Pointer() unsafe.Pointer {
	return unsafe.Pointer(p)
}
func (p *VersionNegotiationPacket) PNSpace() PNSpace                 { return PNSpaceNoSpace }
func (p *VersionNegotiationPacket) EncryptionLevel() EncryptionLevel { return EncryptionLevelNone }
func ReadVersionNegotationPacket(buffer *bytes.Reader) *VersionNegotiationPacket {
	p := new(VersionNegotiationPacket)
	b, err := buffer.ReadByte()
	if err != nil {
		panic(err)
	}
	p.UnusedField = b & 0x7f
	binary.Read(buffer, binary.BigEndian, &p.Version)
	CIDL, _ := buffer.ReadByte()
	DCIL := 3 + ((CIDL & 0xf0) >> 4)
	SCIL := 3 + (CIDL & 0xf)
	p.DestinationCID = make([]byte, DCIL, DCIL)
	binary.Read(buffer, binary.BigEndian, &p.DestinationCID)
	p.SourceCID = make([]byte, SCIL, SCIL)
	binary.Read(buffer, binary.BigEndian, &p.SourceCID)
	for {
		var version uint32
		err := binary.Read(buffer, binary.BigEndian, &version)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		} else if err != nil {
			panic(err)
		}
		p.SupportedVersions = append(p.SupportedVersions, SupportedVersion(version))
	}
	return p
}
func NewVersionNegotiationPacket(unusedField uint8, version uint32, versions []SupportedVersion, conn *Connection) *VersionNegotiationPacket {
	p := new(VersionNegotiationPacket)
	p.UnusedField = unusedField
	p.DestinationCID = conn.DestinationCID
	p.SourceCID = conn.SourceCID
	p.Version = version
	p.SupportedVersions = versions
	return p
}

type Framer interface {
	Packet
	GetFrames() []Frame
	AddFrame(frame Frame)
	GetRetransmittableFrames() []Frame
	Contains(frameType FrameType) bool
	OnlyContains(frameType FrameType) bool
	GetFirst(frameType FrameType) Frame
	GetAll(frameType FrameType) []Frame
}
type FramePacket struct {
	abstractPacket
	Frames []Frame
}
func (p *FramePacket) GetFrames() []Frame {
	return p.Frames
}
func (p *FramePacket) AddFrame(frame Frame) {
	p.Frames = append(p.Frames, frame)
}
func (p *FramePacket) GetRetransmittableFrames() []Frame {
	var frames []Frame
	for _, frame := range p.Frames {
		if frame.shouldBeRetransmitted() {
			frames = append(frames, frame)
		}
	}
	return frames
}
func (p *FramePacket) Pointer() unsafe.Pointer {
	return unsafe.Pointer(p)
}
func (p *FramePacket) Contains(frameType FrameType) bool {
	for _, f := range p.Frames {
		if f.FrameType() == frameType {
			return true
		}
	}
	return false
}
func (p *FramePacket) OnlyContains(frameType FrameType) bool {
	for _, f := range p.Frames {
		if f.FrameType() != frameType {
			return false
		}
	}
	return true
}
func (p *FramePacket) GetFirst(frameType FrameType) Frame {
	for _, f := range p.Frames {
		if f.FrameType() == frameType {
			return f
		}
	}
	return nil
}
func (p *FramePacket) GetAll(frameType FrameType) []Frame {
	var frames []Frame
	for _, f := range p.Frames {
		if f.FrameType() == frameType {
			frames = append(frames, f)
		}
	}
	return frames
}
func (p *FramePacket) ShouldBeAcknowledged() bool {
	for _, frame := range p.Frames {
		switch frame.FrameType() {
		case AckType, PaddingFrameType, ConnectionCloseType, ApplicationCloseType:
		default:
			return true
		}
	}
	return false
}
func (p *FramePacket) EncodePayload() []byte {
	buffer := new(bytes.Buffer)
	for _, frame := range p.Frames {
		frame.writeTo(buffer)
	}
	return buffer.Bytes()
}

type InitialPacket struct {
	FramePacket
}
func (p *InitialPacket) GetRetransmittableFrames() []Frame {
	var frames []Frame
	for _, frame := range p.Frames {
		if frame.shouldBeRetransmitted() || frame.FrameType() == PaddingFrameType {
			frames = append(frames, frame)
		}
	}
	return frames
}
func (p *InitialPacket) PNSpace() PNSpace { return PNSpaceInitial }
func (p *InitialPacket) EncryptionLevel() EncryptionLevel { return EncryptionLevelInitial }
func ReadInitialPacket(buffer *bytes.Reader, conn *Connection) *InitialPacket {
	p := new(InitialPacket)
	p.header = ReadLongHeader(buffer, conn)
	for {
		frame, err := NewFrame(buffer, conn)
		if err != nil {
			spew.Dump(p)
			panic(err)
		}
		if frame == nil {
			break
		}
		if cf, ok := frame.(*CryptoFrame); ok {
			conn.CryptoStreams.Get(p.PNSpace()).addToRead(&StreamFrame{Offset: cf.Offset, Length: cf.Length, StreamData: cf.CryptoData})
		}
		p.Frames = append(p.Frames, frame)
	}
	return p
}
func NewInitialPacket(conn *Connection) *InitialPacket {
	p := new(InitialPacket)
	p.header = NewLongHeader(Initial, conn, PNSpaceInitial)
	if len(conn.Token) > 0 {
		p.header.(*LongHeader).Token = conn.Token
		p.header.(*LongHeader).TokenLength = NewVarInt(uint64(len(conn.Token)))
	}
	return p
}

type RetryPacket struct {
	abstractPacket
	OriginalDestinationCID ConnectionID
	RetryToken []byte
}
func ReadRetryPacket(buffer *bytes.Reader, conn *Connection) *RetryPacket {
	p := new(RetryPacket)
	h := ReadLongHeader(buffer, conn)  // TODO: This should not be a full-length long header. Retry header ?
	p.header = h
	OCIDL := h.lowerBits & 0x0f
	if OCIDL > 0 {
		OCIDL += 3
	}
	p.OriginalDestinationCID = make([]byte, OCIDL)
	buffer.Read(p.OriginalDestinationCID)
	p.RetryToken = make([]byte, buffer.Len())
	buffer.Read(p.RetryToken)
	return p
}
func (p *RetryPacket) GetRetransmittableFrames() []Frame { return nil }
func (p *RetryPacket) Pointer() unsafe.Pointer { return unsafe.Pointer(p) }
func (p *RetryPacket) PNSpace() PNSpace { return PNSpaceNoSpace }
func (p *RetryPacket) EncryptionLevel() EncryptionLevel { return EncryptionLevelNone }
func (p *RetryPacket) ShouldBeAcknowledged() bool { return false }
func (p *RetryPacket) EncodePayload() []byte {
	buffer := new(bytes.Buffer)
	buffer.WriteByte(byte(len(p.OriginalDestinationCID)))
	buffer.Write(p.OriginalDestinationCID)
	buffer.Write(p.RetryToken)
	return buffer.Bytes()
}

type HandshakePacket struct {
	FramePacket
}
func (p *HandshakePacket) PNSpace() PNSpace { return PNSpaceHandshake }
func (p *HandshakePacket) EncryptionLevel() EncryptionLevel { return EncryptionLevelHandshake }
func ReadHandshakePacket(buffer *bytes.Reader, conn *Connection) *HandshakePacket {
	p := new(HandshakePacket)
	p.header = ReadLongHeader(buffer, conn)
	for {
		frame, err := NewFrame(buffer, conn)
		if err != nil {
			spew.Dump(p)
			panic(err)
		}
		if frame == nil {
			break
		}
		if cf, ok := frame.(*CryptoFrame); ok {
			conn.CryptoStreams.Get(p.PNSpace()).addToRead(&StreamFrame{Offset: cf.Offset, Length: cf.Length, StreamData: cf.CryptoData})
		}
		p.Frames = append(p.Frames, frame)
	}
	return p
}
func NewHandshakePacket(conn *Connection) *HandshakePacket {
	p := new(HandshakePacket)
	p.header = NewLongHeader(Handshake, conn, PNSpaceHandshake)
	return p
}

type ProtectedPacket struct {
	FramePacket
}
func (p *ProtectedPacket) PNSpace() PNSpace { return PNSpaceAppData }
func (p *ProtectedPacket) EncryptionLevel() EncryptionLevel { return EncryptionLevel1RTT }
func ReadProtectedPacket(buffer *bytes.Reader, conn *Connection) *ProtectedPacket {
	p := new(ProtectedPacket)
	p.header = ReadHeader(buffer, conn)
	for {
		frame, err := NewFrame(buffer, conn)
		if err != nil {
			spew.Dump(p)
			panic(err)
		}
		if frame == nil {
			break
		}
		if cf, ok := frame.(*CryptoFrame); ok {
			conn.CryptoStreams.Get(p.PNSpace()).addToRead(&StreamFrame{Offset: cf.Offset, Length: cf.Length, StreamData: cf.CryptoData})
		}
		p.Frames = append(p.Frames, frame)
	}
	return p
}
func NewProtectedPacket(conn *Connection) *ProtectedPacket {
	p := new(ProtectedPacket)
	p.header = NewShortHeader(conn)
	return p
}

type ZeroRTTProtectedPacket struct {
	FramePacket
}
func (p *ZeroRTTProtectedPacket) PNSpace() PNSpace { return PNSpaceAppData }
func (p *ZeroRTTProtectedPacket) EncryptionLevel() EncryptionLevel { return EncryptionLevel0RTT }
func NewZeroRTTProtectedPacket(conn *Connection) *ZeroRTTProtectedPacket {
	p := new(ZeroRTTProtectedPacket)
	p.header = NewLongHeader(ZeroRTTProtected, conn, PNSpaceAppData)
	return p
}