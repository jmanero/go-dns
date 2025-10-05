package dns

import (
	"context"
	"net"

	"golang.org/x/net/dns/dnsmessage"
)

// Request stores a parsed dnsmessage.Header and a dnsmessage.Parser to read the rest of the request
type Request struct {
	dnsmessage.Header
	dnsmessage.Parser

	LocalAddr  net.Addr
	RemoteAddr net.Addr

	ctx context.Context
}

func (req *Request) String() string {
	return req.Header.GoString()
}

// Context returns the context for the request
func (req *Request) Context() context.Context {
	return req.ctx
}

// WithContext clones the REquest and sets its context value
func (req *Request) WithContext(ctx context.Context) *Request {
	clone := *req
	clone.ctx = ctx

	return &clone
}
