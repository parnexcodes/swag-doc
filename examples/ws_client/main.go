// Command ws_client exercises a WebSocket endpoint through the SwagDoc
// proxy: it performs a handshake, sends a few messages, and prints the
// echoed replies. Used by `make demo` to capture WebSocket traffic.
//
// Usage: go run ./examples/ws_client/main.go [host:port]
package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

func main() {
	addr := "localhost:8888"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	path := "/ws"
	if idx := strings.Index(addr, "/"); idx >= 0 {
		path = addr[idx:]
		addr = addr[:idx]
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	defer func() { _ = conn.Close() }()

	// Handshake (RFC 6455 section 4.2.1).
	key := base64.StdEncoding.EncodeToString([]byte("demo-client-key"))
	_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, addr, key)
	if err != nil {
		log.Fatalf("handshake write: %v", err)
	}

	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		log.Fatalf("handshake read: %v", err)
	}
	if !strings.Contains(line, "101") {
		log.Fatalf("handshake failed: %s", strings.TrimSpace(line))
	}
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			log.Fatalf("handshake read: %v", err)
		}
		if h == "\r\n" || h == "\n" {
			break
		}
	}
	log.Printf("connected to %s%s", addr, path)

	messages := []string{
		"hello from demo client",
		`{"type":"greeting","user":"demo"}`,
		`{"type":"ping","n":1}`,
	}
	for _, msg := range messages {
		if err := proxy.WriteWSFrame(conn, true, proxy.WSOpcodeText, []byte(msg), true); err != nil {
			log.Fatalf("send: %v", err)
		}
		hdr, err := proxy.ReadWSFrameHeader(br)
		if err != nil {
			log.Fatalf("receive: %v", err)
		}
		payload := make([]byte, hdr.PayloadLen)
		if _, err := io.ReadFull(br, payload); err != nil {
			log.Fatalf("receive: %v", err)
		}
		if hdr.Masked {
			proxy.UnmaskBytes(payload, payload, hdr.MaskKey, 0)
		}
		log.Printf("echo: %s", payload)
	}

	// Close politely with a close frame (status 1000).
	if err := proxy.WriteWSFrame(conn, true, proxy.WSOpcodeClose, []byte{0x03, 0xe8}, true); err != nil {
		log.Fatalf("close frame: %v", err)
	}
	log.Println("done")
}
