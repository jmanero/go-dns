package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"

	"github.com/jmanero/go-dns"
	"golang.org/x/net/dns/dnsmessage"
)

// ServeTXT implements a dns.HandlerFunc that responds to TXT queries with a Hello World message
func ServeTXT(wr dns.ResponseWriter, req *dns.Request) {
	queries, err := req.AllQuestions()
	if err != nil {
		panic(err)
	}

	if len(queries) != 1 {
		panic(fmt.Errorf("UNexpected number of queries: %d != 1", len(queries)))
	}

	query := queries[0]
	if query.Type != dnsmessage.TypeTXT {
		res := wr.Builder(dnsmessage.Header{
			ID:            req.ID,
			Response:      true,
			OpCode:        req.OpCode,
			Authoritative: true,
			RCode:         dnsmessage.RCodeRefused,
		})

		wr.SendBuilder(&res)
		return
	}

	res := wr.Builder(dnsmessage.Header{
		ID:            req.ID,
		Response:      true,
		OpCode:        req.OpCode,
		Authoritative: true,
	})

	err = res.StartQuestions()
	if err != nil {
		panic(err)
	}
	err = res.Question(query)
	if err != nil {
		panic(err)
	}

	err = res.StartAnswers()
	if err != nil {
		panic(err)
	}
	err = res.TXTResource(
		dnsmessage.ResourceHeader{Name: query.Name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET, TTL: 42},
		dnsmessage.TXTResource{TXT: []string{"Hello World!"}},
	)
	if err != nil {
		panic(err)
	}

	wr.SendBuilder(&res)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	conn, err := net.ListenPacket("udp", "127.0.0.1:7653")
	if err != nil {
		panic(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:7653")
	if err != nil {
		panic(err)
	}

	server := dns.Server{
		Handler: dns.HandlerFunc(ServeTXT),
	}

	go server.Serve(conn)
	go server.ServeStream(listener)

	<-ctx.Done()
	server.Shutdown(context.Background())
}
