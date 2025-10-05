package dns

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"sync"

	"github.com/jmanero/go-logging"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// Handler receives DNS messages and builds responses
type Handler interface {
	ServeDNS(ResponseWriter, *Request)
}

// HandlerFunc wraps a function with a Handler interface
type HandlerFunc func(ResponseWriter, *Request)

// ServeDNS calls the wrapped function
func (fn HandlerFunc) ServeDNS(wr ResponseWriter, req *Request) {
	fn(wr, req)
}

type closers struct {
	entries []io.Closer
	sync.Mutex
}

func (cls *closers) AddCloser(closer io.Closer) {
	cls.Lock()
	defer cls.Unlock()
	cls.entries = append(cls.entries, closer)
}

func (cls *closers) CloseAll() (err error) {
	cls.Lock()
	defer cls.Unlock()

	for _, closer := range cls.entries {
		err = multierr.Append(err, closer.Close())
	}

	return
}

type canceler struct {
	once   sync.Once
	base   context.Context
	cancel context.CancelFunc
}

// Context initializes a context.Context and context.CancelFunc on first call. Subsequent calls return the same base Context value
func (ca *canceler) Context() context.Context {
	ca.once.Do(func() { ca.base, ca.cancel = context.WithCancel(context.Background()) })
	return ca.base
}

// Server receives DNS messages from clients and calls a Handler to generate responses
type Server struct {
	Handler

	// BaseContext is called when new Serve/ServeStream routines are created
	BaseContext func(context.Context, net.Addr) context.Context
	// ConnContext is called when a new connection is accepted from a Listener
	ConnContext func(context.Context, net.Conn) context.Context

	sync.WaitGroup
	closers
	canceler
}

// Handle is called when a message is received from a connection. It parses the message's header, then calls the Server's Handler
func (server *Server) Handle(ctx context.Context, buf []byte, wr ResponseWriter, req *Request) {
	defer func() {
		if value := recover(); value != nil {
			logging.Error(ctx, "handler.panic", zap.Any("panic", value), zap.String("stack", string(debug.Stack())))
		}
	}()

	var err error

	// Parse the message's header
	req.Header, err = req.Start(buf)
	if err != nil {
		logging.Error(ctx, "handler.parse", zap.Error(err))
		return
	}

	server.ServeDNS(wr, req)
}

// Serve handles DNS messages from a PacketConn
func (server *Server) Serve(conn net.PacketConn) error {
	server.Add(1)
	server.AddCloser(conn)

	defer server.Done()
	defer conn.Close()

	ctx := server.Context()
	if server.BaseContext != nil {
		ctx = server.BaseContext(ctx, conn.LocalAddr())
	}

	for {
		// Get a 4k buffer to read the next datagram
		buf := GetBuffer(4096, 4096)

		size, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}

		server.Go(func() {
			defer FreeBuffer(buf)
			server.Handle(ctx, buf[:size],
				&PacketWriter{PacketConn: conn, Addr: from},
				&Request{ctx: ctx, LocalAddr: conn.LocalAddr(), RemoteAddr: from})
		})
	}
}

// ServeStream handles DNS messages from a Listener
func (server *Server) ServeStream(listener net.Listener) error {
	server.Add(1)
	server.AddCloser(listener)

	defer server.Done()
	defer listener.Close()

	ctx := server.Context()
	if server.BaseContext != nil {
		ctx = server.BaseContext(ctx, listener.Addr())
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		server.Go(func() { server.HandleStream(ctx, conn) })
	}
}

// HandleStream reconstructs frames from a net.Conn stream and passes them to the
// message handler. Packets are processed serially
func (server *Server) HandleStream(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	if server.ConnContext != nil {
		ctx = server.ConnContext(ctx, conn)
	}

	logger := logging.FromContext(ctx)

	// Get a 4k buffer for reassembling frames
	buf := GetBuffer(4096, 4096)
	defer FreeBuffer(buf)

	// Write position, read position in buffer
	var wpos, rpos int

	for {
		nread, err := conn.Read(buf[wpos:])
		wpos += nread

		// Read frames out of the buffer while there's at least one frame header (2 bytes)
		// and DNS message header (12 bytes)
		for wpos-rpos >= 14 {
			// Read the frame header
			size := int(DecodeLength(buf[rpos:]))

			// Make sure that the buffer has enough capacity to hold the whole frame
			buf = GrowBuffer(buf, size, size)

			// Check if the whole frame has been read into the buffer
			if size > wpos-(rpos+2) {
				// Wait for stream.Read() to append more of the frame
				break
			}

			// Step past the frame header
			rpos += 2

			// Send the message to the handler
			server.Handle(ctx, buf[rpos:rpos+size],
				&StreamWriter{Conn: conn},
				&Request{ctx: ctx, LocalAddr: conn.RemoteAddr(), RemoteAddr: conn.RemoteAddr()})

			// Step passed the processed message and check for another frame
			rpos += size
		}

		if errors.Is(err, io.EOF) {
			// Connection is closed
			return
		}

		if err != nil {
			logger.Warn("connection", zap.Error(err))
			return
		}

		// Shift a trailing frame fragment to the front of the buffer and continue
		// reading. In the wild, this happens very rarely as most TCP DNS clients
		// only use a connection for a single request/response transaction
		wpos = copy(buf, buf[rpos:wpos])
		rpos = 0

		buf = buf[:cap(buf)]
	}
}

// Shutdown gracefully stops accepting requests and attempts to
func (server *Server) Shutdown(ctx context.Context) (err error) {
	// Close connections to stop accepting new requests and join Serve/ServeStream routines
	if cerr := server.CloseAll(); cerr != nil {
		logging.Error(ctx, "shutdown.close", zap.Error(cerr))
	}

	// Wait for in-flight handlers to complete, or context to be canceled
	wait := make(chan struct{})
	go func() { server.Wait(); close(wait) }()

	select {
	case <-ctx.Done():
	case <-wait:
	}

	if server.cancel != nil {
		// Cancel handler contexts
		server.cancel()
	}

	return
}
