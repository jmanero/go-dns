package dns

import (
	"encoding/binary"
	"net"

	"golang.org/x/net/dns/dnsmessage"
)

// Length header encoding/decoding for streams
var (
	EncodeLength = binary.BigEndian.PutUint16
	DecodeLength = binary.BigEndian.Uint16
)

// ResponseWriter sends a DNS response frame to the client
type ResponseWriter interface {
	// Builder creates a dnsmessage.Builder optimized for the underlying transport
	Builder(dnsmessage.Header) dnsmessage.Builder

	// Send a message directly to the underlying transport
	Send([]byte)
	// SendBuilder finalizes a builder and sends the result, with any post-processing
	// required for the transport, to the client. It is assumed that the builder was
	// prepared by ResponseWriter.Builder
	SendBuilder(*dnsmessage.Builder)
}

// PacketWriter implements ResponseWriter for net.PacketConn
type PacketWriter struct {
	net.PacketConn
	Addr net.Addr
}

var _ ResponseWriter = &PacketWriter{}

// Builder initializes a new dnsmessage.Builder for a UDP DNS transaction
func (wr *PacketWriter) Builder(header dnsmessage.Header) dnsmessage.Builder {
	// Start building at the beginning of the buffer
	return dnsmessage.NewBuilder(GetBuffer(4096, 0), header)
}

// SendBuilder is a helper that finalizes a dnsmessage.Builder and calls Send with the resulting datagram
func (wr *PacketWriter) SendBuilder(builder *dnsmessage.Builder) {
	msg, err := builder.Finish()
	if err != nil {
		panic(err)
	}

	wr.Send(msg)
	FreeBuffer(msg)
}

// Send a message to the peer that the request was received from
func (wr *PacketWriter) Send(msg []byte) {
	_, err := wr.WriteTo(msg, wr.Addr)
	if err != nil {
		panic(err)
	}
}

// StreamWriter implements ResponseWriter for net.Conn
type StreamWriter struct {
	net.Conn
}

var _ ResponseWriter = &StreamWriter{}

// Builder creates a new builder with a 2 byte length header
func (wr *StreamWriter) Builder(header dnsmessage.Header) dnsmessage.Builder {
	// Start building after the first 2 bytes of the slice
	return dnsmessage.NewBuilder(GetBuffer(4096, 2), header)
}

// SendBuilder finalizes a Builder and writes its length header before sending
// the resulting message. Builders MUST be created with a 2 byte header. Use
// StreamWriter.Builder(header) or something similar to `dnsmessage.NewBuilder(make([]byte, 2, 1024), header)`
func (wr *StreamWriter) SendBuilder(builder *dnsmessage.Builder) {
	msg, err := builder.Finish()
	if err != nil {
		panic(err)
	}

	// Write a length header to the first two bytes of the frame
	EncodeLength(msg, uint16(len(msg)-2))

	wr.Send(msg)
	FreeBuffer(msg)
}

// Send a message directly to the connection stream. The caller is responsible
// for prepending a length header to the message.
func (wr *StreamWriter) Send(frame []byte) {
	_, err := wr.Write(frame)
	if err != nil {
		panic(err)
	}
}
