// cmd/delayproxy is a TCP proxy that holds every chunk it forwards for half the given delay in
// each direction, so a request through it pays about that much per round trip. The dev stack's
// object store answers in about a millisecond, which hides exactly the cost production pays on
// every R2 call; pointing a dev worker at the store through this proxy brings it back
// (docs/PERFORMANCE.md "Local measurements"). From services/server:
//
//	go run ./cmd/delayproxy 127.0.0.1:9555 127.0.0.1:9000 80ms
//
// then run the worker with S3_ENDPOINT=http://127.0.0.1:9555. Raw TCP rather than an HTTP
// proxy, so the Host header S3 request signatures cover goes through untouched.
package main

import (
	"log"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: delayproxy LISTEN_ADDR UPSTREAM_ADDR DELAY")
	}
	delay, err := time.ParseDuration(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			u, err := net.Dial("tcp", os.Args[2])
			if err != nil {
				return
			}
			defer u.Close()
			go pipe(u, c, delay/2)
			pipe(c, u, delay/2)
		}(c)
	}
}

// pipe copies src to dst, holding each chunk for d first, and half-closes dst at the end.
func pipe(dst, src net.Conn, d time.Duration) {
	buf := make([]byte, 64<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			time.Sleep(d)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if tc, ok := dst.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
			return
		}
	}
}
