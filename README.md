go-dns
======

A very simple DNS server that attempts to look like `net/http.Server`. Based upon [codeberg.org/miekg/dns](https://pkg.go.dev/codeberg.org/miekg/dns) and [Miek's blog post](https://miek.nl/2022/july/15/a-miekg/dns-v2-package/) pointing out the existence of the [golang.org/x/net/dns/dnsmessage](https://pkg.go.dev/golang.org/x/net/dns/dnsmessage) package.

The `dns.Handler` interface provides implementations with a `dns.Request` struct and a `dns.ResponseWriter` implementation for the respective stream/TCP or datagram/UDP connection that the message was received upon.

- `dns.Request` contains a `dnsmessage.Header` and `dnsmessage.Parser` for an incoming DNS message. Handlers can use the `dnsmessage.Parser` to read resource records from the message.
- `dns.ResponseWriter` provides helper methods to create a `dnsmessage.Builder`, and to send the resulting response message to the client.
- Handlers may call the `ResponseWriter.Send()` and `ResponseWriter.SendBuilder()` multiple times to write multiple DNS messages to the underlying connection
- The `ResponseWriter.Send()` method writes a message directly to the underlying connection. NOTE that for stream/TCP connections, the `Send()` method _does not_ prepend a length header to the message.
- For stream/TCP connections, the `ResponseWriter.Builder()` method creates `dnsmessage.Builder` instances with two byte prefixes for length headers, which will be returned by the `Builder.Finish()` method. The respective `ResponseWriter.SendBuilder()` call for stream/TCP connections _will_ automatically encode a big-endian length header into these prefix bytes before writing the message to the connection.

## Example

The [`example`](./example/main.go) package contains a minimal Hello World server. Run it:

```
$ go run ./example
```

And query it:

```
$ dig @127.0.0.1 -p 7653 TXT example.com

; <<>> DiG 9.10.6 <<>> @127.0.0.1 -p 7653 TXT example.com
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 29056
;; flags: qr aa; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0

;; QUESTION SECTION:
;example.com.			IN	TXT

;; ANSWER SECTION:
example.com.		42	IN	TXT	"Hello World!"

;; Query time: 0 msec
;; SERVER: 127.0.0.1#7653(127.0.0.1)
;; WHEN: Sat Oct 04 22:32:55 PDT 2025
;; MSG SIZE  rcvd: 65
```
