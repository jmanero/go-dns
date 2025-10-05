package dns

import (
	"context"
	"net"
	"time"

	"github.com/jmanero/go-listen"
	"github.com/jmanero/go-logging"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Options configure a DNS server
type Options struct {
	Streams   []ListenOptions `json:"streams,omitempty"`
	Datagrams []ListenOptions `json:"datagrams,omitempty"`
	Shutdown  time.Duration   `json:"shutdown_timeout"`
}

// ListenOptions configures a listener
type ListenOptions struct {
	Network string         `json:"network"`
	Listen  string         `json:"listen"`
	Socket  listen.Options `json:"options"`
}

// ListenAndServeStream opens net.Listeners and starts accepting connections from them
func ListenAndServeStream(ctx context.Context, opts ListenOptions, group *errgroup.Group, server *Server) (err error) {
	_, logger := logging.With(ctx, zap.String("bind", opts.Listen))

	listeners, err := listen.Listen(ctx, opts.Network, opts.Listen, opts.Socket)
	if err != nil && len(listeners) == 0 {
		logger.Error("listen.error", zap.Error(err))
		return
	}

	for _, listener := range listeners {
		logger.Info("listening", zap.Stringer("addr", listener.Addr()))
		group.Go(func() error { return server.ServeStream(listener) })
	}

	return
}

// ListenAndServeDatagram opens and binds net.PacketConns and starts reading message datagrams from them
func ListenAndServeDatagram(ctx context.Context, opts ListenOptions, group *errgroup.Group, server *Server) (err error) {
	_, logger := logging.With(ctx, zap.String("bind", opts.Listen))

	conns, err := listen.Packet(ctx, opts.Network, opts.Listen, opts.Socket)
	if err != nil && len(conns) == 0 {
		// Fail if no connections could be opened for the given address and options
		return
	}

	for _, conn := range conns {
		logger.Info("listening", zap.Stringer("addr", conn.LocalAddr()))
		group.Go(func() error { return server.Serve(conn) })
	}

	return
}

// Serve creates a Server, starts configured listeners, and creates a shutdown monitor
func Serve(ctx context.Context, opts Options, group *errgroup.Group, handler Handler) (err error) {
	ctx, logger := logging.Named(ctx, "dns")

	service := Server{
		Handler: handler,
		BaseContext: func(ctx context.Context, addr net.Addr) context.Context {
			return logging.WithLogger(ctx, logger.With(zap.String("proto", addr.Network()), zap.Stringer("listener", addr)))
		},
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			ctx, _ = logging.With(ctx, zap.Stringer("conn", conn.RemoteAddr()))
			return ctx
		},
	}

	// Start the shutdown monitor before trying to create listeners. This will clean up any running
	// server routines and their listeners if subsequent ListenAndServeXXX calls fail
	group.Go(func() error { return Shutdown(ctx, opts.Shutdown, service.Shutdown) })

	for _, opts := range opts.Streams {
		err = ListenAndServeStream(ctx, opts, group, &service)
		if err != nil {
			return
		}
	}

	for _, opts := range opts.Datagrams {
		err = ListenAndServeDatagram(ctx, opts, group, &service)
		if err != nil {
			return
		}
	}

	return
}

// Shutdown gracefully stops a server instance
func Shutdown(ctx context.Context, timeout time.Duration, shutdown func(context.Context) error) error {
	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logging.Info(ctx, "stopping")
	defer logging.Info(ctx, "stopped")
	return shutdown(ctx)
}
