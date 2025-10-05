package dns_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"testing"

	"github.com/jmanero/go-dns"
	"github.com/jmanero/go-logging"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/net/dns/dnsmessage"
)

func GenerateQuery(id uint16, qs ...dnsmessage.Question) []byte {
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: id,
	})

	err := builder.StartQuestions()
	if err != nil {
		panic(err)
	}

	for _, q := range qs {
		err = builder.Question(q)
		if err != nil {
			panic(err)
		}
	}

	buf, err := builder.Finish()
	if err != nil {
		panic(err)
	}

	return buf
}

type DatagramTester struct {
	net.PacketConn
	datagrams []Datagram
}

type Datagram struct {
	from net.Addr
	buf  []byte
}

func (dt *DatagramTester) AddDatagram(from net.Addr, buf []byte) {
	dt.datagrams = append(dt.datagrams, Datagram{from, buf})
}

func (*DatagramTester) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IP{10, 0, 1, 1}, Port: 53}
}

func (dt *DatagramTester) ReadFrom(buf []byte) (int, net.Addr, error) {
	if len(dt.datagrams) == 0 {
		return 0, nil, net.ErrClosed
	}

	var dgram Datagram
	dgram, dt.datagrams = dt.datagrams[0], dt.datagrams[1:]

	return copy(buf, dgram.buf), dgram.from, nil
}

func (dt *DatagramTester) Close() error { return nil }

type StreamTester struct {
	net.Conn
	chunks [][]byte
}

func (*StreamTester) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IP{1, 2, 3, 4}, Port: 4242}
}

func (*StreamTester) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IP{10, 0, 1, 1}, Port: 53}
}

func (st *StreamTester) Read(buf []byte) (n int, _ error) {
	if len(st.chunks) == 0 {
		return 0, io.EOF
	}

	n = copy(buf, st.chunks[0])
	if n == len(st.chunks[0]) {
		st.chunks = st.chunks[1:]
	} else if n == 0 {
		return n, fmt.Errorf("Read 0")
	} else {
		st.chunks[0] = st.chunks[0][n:]
	}

	return
}

func (st *StreamTester) Close() error { return nil }

func GenerateFrame(id uint16, qs ...dnsmessage.Question) []byte {
	buf := GenerateQuery(id, qs...)

	var head [2]byte
	buf = append(head[:], buf...)
	binary.BigEndian.PutUint16(buf, uint16(len(buf)-2))

	return buf
}

func TestDatagram(t *testing.T) {
	ctx := logging.New(
		context.Background(),
		zapcore.NewCore(
			zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
			zapcore.AddSync(os.Stderr), zapcore.DebugLevel,
		))

	var tester DatagramTester
	var queries []uint16

	tester.AddDatagram(&net.UDPAddr{IP: net.IP{1, 2, 3, 4}, Port: 5367}, GenerateQuery(42))
	queries = append(queries, 42)

	tester.AddDatagram(&net.UDPAddr{IP: net.IP{1, 2, 3, 4}, Port: 5367}, GenerateFrame(1234,
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeNS, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassCHAOS},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
	))

	queries = append(queries, 1234)

	tester.AddDatagram(&net.UDPAddr{IP: net.IP{1, 2, 3, 4}, Port: 5367}, GenerateFrame(5678, dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}))
	queries = append(queries, 5678)

	server := dns.Server{
		Handler: dns.HandlerFunc(func(_ dns.ResponseWriter, req *dns.Request) {
			qs, err := req.AllQuestions()
			assert.NoError(t, err)

			fmt.Println(req.ID, req.OpCode, req.RCode, len(qs))
			assert.Equal(t, queries[0], req.ID)

			queries = queries[1:]
		}),
		ConnContext: func(context.Context, net.Conn) context.Context { return ctx },
	}

	server.Serve(&tester)
}

func TestStream(t *testing.T) {
	var tester StreamTester
	var queries []uint16

	tester.chunks = append(tester.chunks, []byte{0, 12}, GenerateQuery(42))
	queries = append(queries, 42)

	tester.chunks = append(tester.chunks, []byte{})

	buf := GenerateFrame(1234,
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeNS, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassCHAOS},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET},
		dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
	)

	queries = append(queries, 1234)

	// Break up a large-ish frame into multiple chunks
	tester.chunks = append(tester.chunks, buf[0:1], buf[1:14], buf[14:36], []byte{}, buf[36:64])

	buf2 := GenerateFrame(5678, dnsmessage.Question{Name: dnsmessage.MustNewName("foo.bar.baz."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	queries = append(queries, 5678)

	// Join two partial frames in one chunk
	tester.chunks = append(tester.chunks, append(buf[64:], buf2[:8]...), buf2[8:])

	server := dns.Server{
		Handler: dns.HandlerFunc(func(_ dns.ResponseWriter, req *dns.Request) {
			qs, err := req.AllQuestions()
			assert.NoError(t, err)

			fmt.Println(req.ID, req.OpCode, req.RCode, len(qs))
			assert.Equal(t, queries[0], req.ID)

			queries = queries[1:]
		}),
	}

	ctx := logging.New(
		server.Context(), // HACK: initialize the server's base contest before calling HandleStream
		zapcore.NewCore(
			zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
			zapcore.AddSync(os.Stderr), zapcore.DebugLevel,
		))

	server.HandleStream(ctx, &tester)
}
