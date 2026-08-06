package proxy

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildWSFrame builds a frame in wire format for test expectations.
func buildWSFrame(t *testing.T, fin bool, opcode byte, payload []byte, mask bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, WriteWSFrame(&buf, fin, opcode, payload, mask))
	return buf.Bytes()
}

func readFrameFrom(t *testing.T, r *bufio.Reader) (WSFrameHeader, []byte) {
	t.Helper()
	hdr, err := ReadWSFrameHeader(r)
	require.NoError(t, err)
	payload := make([]byte, hdr.PayloadLen)
	_, err = r.Read(payload)
	require.NoError(t, err)
	return hdr, payload
}

func TestReadWSFrameHeaderLengths(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"7-bit length", []byte("hello")},
		{"16-bit length", bytes.Repeat([]byte("a"), 300)},
		{"64-bit length", bytes.Repeat([]byte("b"), 70000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mask := range []bool{false, true} {
				wire := buildWSFrame(t, true, WSOpcodeText, tc.payload, mask)
				r := bufio.NewReader(bytes.NewReader(wire))
				hdr, err := ReadWSFrameHeader(r)
				require.NoError(t, err)
				assert.Equal(t, uint64(len(tc.payload)), hdr.PayloadLen)
				assert.Equal(t, mask, hdr.Masked)
				assert.Equal(t, byte(WSOpcodeText), hdr.Opcode)
				assert.True(t, hdr.Fin)
				// The header bytes must be exact for verbatim forwarding.
				assert.Equal(t, wire[:len(wire)-len(tc.payload)], hdr.Header)

				payload := make([]byte, hdr.PayloadLen)
				_, err = io.ReadFull(r, payload)
				require.NoError(t, err)
				if mask {
					UnmaskBytes(payload, payload, hdr.MaskKey, 0)
				}
				assert.Equal(t, tc.payload, payload)
			}
		})
	}
}

func TestReadWSFrameHeaderOpcodes(t *testing.T) {
	for _, opcode := range []byte{WSOpcodeText, WSOpcodeBinary} {
		wire := buildWSFrame(t, true, opcode, []byte("x"), false)
		hdr, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		require.NoError(t, err)
		assert.Equal(t, opcode, hdr.Opcode)
	}

	// Fragmented (FIN=0) text frame followed by a FIN continuation frame.
	wire := buildWSFrame(t, false, WSOpcodeText, []byte("hel"), false)
	wire = append(wire, buildWSFrame(t, true, WSOpcodeContinuation, []byte("lo"), false)...)
	r := bufio.NewReader(bytes.NewReader(wire))
	hdr, err := ReadWSFrameHeader(r)
	require.NoError(t, err)
	assert.False(t, hdr.Fin)
	assert.Equal(t, byte(WSOpcodeText), hdr.Opcode)
	_, err = r.Discard(int(hdr.PayloadLen))
	require.NoError(t, err)
	hdr, err = ReadWSFrameHeader(r)
	require.NoError(t, err)
	assert.True(t, hdr.Fin)
	assert.Equal(t, byte(WSOpcodeContinuation), hdr.Opcode)
}

func TestReadWSFrameHeaderControlFrames(t *testing.T) {
	// Interleaved ping/pong/close between data frames.
	var wire []byte
	wire = append(wire, buildWSFrame(t, true, WSOpcodeText, []byte("data"), true)...)
	wire = append(wire, buildWSFrame(t, true, WSOpcodePing, []byte("p"), true)...)
	wire = append(wire, buildWSFrame(t, true, WSOpcodePong, []byte("p"), true)...)
	wire = append(wire, buildWSFrame(t, true, WSOpcodeClose, []byte{0x03, 0xe8}, true)...)

	r := bufio.NewReader(bytes.NewReader(wire))
	var opcodes []byte
	for i := 0; i < 4; i++ {
		hdr, err := ReadWSFrameHeader(r)
		require.NoError(t, err)
		opcodes = append(opcodes, hdr.Opcode)
		_, _ = r.Discard(int(hdr.PayloadLen))
	}
	assert.Equal(t, []byte{WSOpcodeText, WSOpcodePing, WSOpcodePong, WSOpcodeClose}, opcodes)
}

func TestReadWSFrameHeaderErrors(t *testing.T) {
	t.Run("invalid opcode", func(t *testing.T) {
		wire := []byte{0x83, 0x01, 'x'} // opcode 0x3 is reserved
		_, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid opcode")
	})

	t.Run("reserved bits set", func(t *testing.T) {
		wire := []byte{0x80 | 0x40 | WSOpcodeText, 0x01, 'x'} // RSV1 set
		_, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved bits")
	})

	t.Run("fragmented control frame", func(t *testing.T) {
		wire := []byte{0x00 | WSOpcodePing, 0x00} // ping with FIN=0
		_, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fragmented control")
	})

	t.Run("control frame too large", func(t *testing.T) {
		wire := []byte{0x80 | WSOpcodePing, 0x7E, 0x00, 0x7E} // 126-byte ping
		_, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds 125")
	})

	t.Run("truncated header", func(t *testing.T) {
		_, err := ReadWSFrameHeader(bufio.NewReader(bytes.NewReader([]byte{0x81})))
		require.Error(t, err)
	})
}

func TestWriteWSFrameRoundTrip(t *testing.T) {
	for _, mask := range []bool{false, true} {
		wire := buildWSFrame(t, true, WSOpcodeText, []byte("round trip"), mask)
		hdr, payload := readFrameFrom(t, bufio.NewReader(bytes.NewReader(wire)))
		assert.Equal(t, byte(WSOpcodeText), hdr.Opcode)
		assert.Equal(t, mask, hdr.Masked)
		if mask {
			UnmaskBytes(payload, payload, hdr.MaskKey, 0)
		}
		assert.Equal(t, "round trip", string(payload))
	}
}

func TestWSRecorderReassemblesFragmentedMessages(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 0}, DirectionClientToServer, 0)

	// Fragmented text: "hel" (FIN=0) + "lo" (FIN=1 continuation).
	// The pump captures payload chunks first, then signals the frame end.
	rec.capture(WSFrameHeader{Opcode: WSOpcodeText}, []byte("hel"), 0)
	rec.frame(WSFrameHeader{Fin: false, Opcode: WSOpcodeText, PayloadLen: 3})
	rec.capture(WSFrameHeader{Opcode: WSOpcodeContinuation}, []byte("lo"), 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeContinuation, PayloadLen: 2})

	// Direct binary message.
	rec.capture(WSFrameHeader{Opcode: WSOpcodeBinary}, []byte{0x00, 0x01}, 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeBinary, PayloadLen: 2})

	require.Len(t, session.Messages, 2)
	assert.Equal(t, "hello", string(session.Messages[0].Payload))
	assert.Equal(t, WSOpcodeText, session.Messages[0].Opcode)
	assert.Equal(t, DirectionClientToServer, session.Messages[0].Direction)
	assert.Equal(t, []byte{0x00, 0x01}, session.Messages[1].Payload)
	assert.Equal(t, WSOpcodeBinary, session.Messages[1].Opcode)
	assert.False(t, session.Messages[0].Truncated)
}

func TestWSRecorderUnmasksClientFrames(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 0}, DirectionClientToServer, 0)

	key := [4]byte{0x11, 0x22, 0x33, 0x44}
	// masked[i] = plain[i] ^ key[i%4]
	plain := []byte("hello")
	masked := make([]byte, len(plain))
	for i := range plain {
		masked[i] = plain[i] ^ key[i%4]
	}
	rec.capture(WSFrameHeader{Opcode: WSOpcodeText, Masked: true, MaskKey: key}, masked, 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeText, Masked: true, MaskKey: key, PayloadLen: 5})

	require.Len(t, session.Messages, 1)
	assert.Equal(t, "hello", string(session.Messages[0].Payload))
}

func TestWSRecorderUnmaskAcrossChunks(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 0}, DirectionClientToServer, 0)

	key := [4]byte{0x10, 0x20, 0x30, 0x40}
	plain := []byte("0123456789abcdef")
	masked := make([]byte, len(plain))
	for i := range plain {
		masked[i] = plain[i] ^ key[i%4]
	}
	// Feed the payload in two chunks with correct offsets; the mask key must
	// continue rotating across chunk boundaries.
	rec.capture(WSFrameHeader{Opcode: WSOpcodeText, Masked: true, MaskKey: key}, masked[:7], 0)
	rec.capture(WSFrameHeader{Opcode: WSOpcodeText, Masked: true, MaskKey: key}, masked[7:], 7)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeText, Masked: true, MaskKey: key, PayloadLen: 16})

	require.Len(t, session.Messages, 1)
	assert.Equal(t, string(plain), string(session.Messages[0].Payload))
}

func TestWSRecorderSkipsControlFrames(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 0}, DirectionServerToClient, 0)

	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodePing, PayloadLen: 1})
	rec.capture(WSFrameHeader{Opcode: WSOpcodePing}, []byte("p"), 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodePong, PayloadLen: 1})
	rec.capture(WSFrameHeader{Opcode: WSOpcodePong}, []byte("p"), 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeClose, PayloadLen: 2})
	rec.capture(WSFrameHeader{Opcode: WSOpcodeClose}, []byte{0x03, 0xe8}, 0)

	assert.Empty(t, session.Messages)
}

func TestWSRecorderTruncatesOversizedMessages(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 0}, DirectionClientToServer, 5)

	payload := []byte("0123456789")
	rec.capture(WSFrameHeader{Opcode: WSOpcodeText}, payload, 0)
	rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeText, PayloadLen: 10})

	require.Len(t, session.Messages, 1)
	assert.Equal(t, "01234", string(session.Messages[0].Payload))
	assert.True(t, session.Messages[0].Truncated)
}

func TestWSRecorderStopsAtMessageCap(t *testing.T) {
	session := &WebSocketSession{}
	rec := newWSRecorder(&wsMessageSink{session: session, maxMsgs: 2}, DirectionServerToClient, 0)

	for _, msg := range []string{"one", "two", "three"} {
		rec.capture(WSFrameHeader{Opcode: WSOpcodeText}, []byte(msg), 0)
		rec.frame(WSFrameHeader{Fin: true, Opcode: WSOpcodeText, PayloadLen: uint64(len(msg))})
	}

	require.Len(t, session.Messages, 2)
	assert.Equal(t, "one", string(session.Messages[0].Payload))
	assert.Equal(t, "two", string(session.Messages[1].Payload))
}

// ---- End-to-end capture through the proxy ----

// wsAcceptKey computes the Sec-WebSocket-Accept response value.
func wsAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// startWSEchoServer starts an API server that upgrades to WebSocket and
// echoes every frame verbatim.
func startWSEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWebSocketUpgrade(r) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", wsAcceptKey(r.Header.Get("Sec-WebSocket-Key")))
		w.WriteHeader(http.StatusSwitchingProtocols)

		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		buf := make([]byte, 4096)
		for {
			hdr, err := ReadWSFrameHeader(brw.Reader)
			if err != nil {
				return
			}
			if _, err := conn.Write(hdr.Header); err != nil {
				return
			}
			remaining := hdr.PayloadLen
			for remaining > 0 {
				n := len(buf)
				if uint64(n) > remaining {
					n = int(remaining)
				}
				nr, rerr := brw.Read(buf[:n])
				if nr > 0 {
					if _, werr := conn.Write(buf[:nr]); werr != nil {
						return
					}
					remaining -= uint64(nr)
				}
				if rerr != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsDial performs a WebSocket handshake over raw TCP.
func wsDial(t *testing.T, addr, path string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	key := base64.StdEncoding.EncodeToString([]byte("test-client-key-1234"))
	_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, addr, key)
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "101", "handshake must succeed")
	for {
		h, err := br.ReadString('\n')
		require.NoError(t, err)
		if h == "\r\n" || h == "\n" {
			break
		}
	}
	return conn, br
}

// startTestProxy starts a ProxyServer on a random port and returns its
// address. Cleanup closes WebSocket connections and waits for sessions.
func startTestProxy(t *testing.T, target string, interceptor APIInterceptor, wsInterceptor WebSocketInterceptor, maxMsgBytes int64, maxMsgs int) (string, *ProxyServer) {
	t.Helper()
	p, err := NewProxyServer(0, target, interceptor)
	require.NoError(t, err)
	p.MaxWSMessageBytes = maxMsgBytes
	p.MaxWSMessages = maxMsgs
	p.WSInterceptor = wsInterceptor

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: p.handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		p.closeAllWSConnections()
		_ = srv.Close()
		p.wsWG.Wait()
	})
	return ln.Addr().String(), p
}

// waitForSessions waits until n WebSocket sessions have been intercepted.
func waitForSessions(t *testing.T, ch chan WebSocketSession, n int) []WebSocketSession {
	t.Helper()
	var sessions []WebSocketSession
	timeout := time.After(10 * time.Second)
	for len(sessions) < n {
		select {
		case s := <-ch:
			sessions = append(sessions, s)
		case <-timeout:
			t.Fatalf("timed out waiting for %d WebSocket sessions, got %d", n, len(sessions))
		}
	}
	return sessions
}

func TestWebSocketCaptureEndToEnd(t *testing.T) {
	echo := startWSEchoServer(t)

	var (
		mu       sync.Mutex
		sessions []WebSocketSession
	)
	sessionCh := make(chan WebSocketSession, 8)
	wsInterceptor := func(s WebSocketSession) {
		mu.Lock()
		sessions = append(sessions, s)
		mu.Unlock()
		sessionCh <- s
	}

	proxyAddr, _ := startTestProxy(t, echo.URL, nil, wsInterceptor, 0, 0)
	conn, br := wsDial(t, proxyAddr, "/ws")

	// Send a masked text frame; the echo must be byte-identical to what the
	// client sent on the wire (masked payload included).
	sent := buildWSFrame(t, true, WSOpcodeText, []byte("hello"), true)
	_, err := conn.Write(sent)
	require.NoError(t, err)

	echoedHeader := make([]byte, len(sent))
	_, err = readFullFrom(br, echoedHeader) // cannot use readFrameFrom: frame is masked
	require.NoError(t, err)
	assert.Equal(t, sent, echoedHeader, "echoed frame must be byte-identical")

	// Send a fragmented message: "hel" + "lo".
	part1 := buildWSFrame(t, false, WSOpcodeText, []byte("hel"), true)
	part2 := buildWSFrame(t, true, WSOpcodeContinuation, []byte("lo"), true)
	_, err = conn.Write(part1)
	require.NoError(t, err)
	_, err = conn.Write(part2)
	require.NoError(t, err)
	hdr, err := ReadWSFrameHeader(br)
	require.NoError(t, err)
	assert.Equal(t, byte(WSOpcodeText), hdr.Opcode)
	assert.False(t, hdr.Fin)
	_, _ = br.Discard(int(hdr.PayloadLen))
	hdr, err = ReadWSFrameHeader(br)
	require.NoError(t, err)
	assert.Equal(t, byte(WSOpcodeContinuation), hdr.Opcode)
	_, _ = br.Discard(int(hdr.PayloadLen))

	// Close the connection and wait for the captured session.
	_ = conn.Close()
	got := waitForSessions(t, sessionCh, 1)
	require.Len(t, got, 1)

	session := got[0]
	assert.Equal(t, "/ws", session.HandshakeRequest.Path)
	assert.Equal(t, http.StatusSwitchingProtocols, session.ResponseStatus)

	// Fragmented client message is recorded as one text message; the echo
	// server echoes frames individually, so the echoed fragments are also
	// reassembled into one server-to-client message.
	var clientMsgs, serverMsgs []WebSocketMessage
	for _, m := range session.Messages {
		switch m.Direction {
		case DirectionClientToServer:
			clientMsgs = append(clientMsgs, m)
		case DirectionServerToClient:
			serverMsgs = append(serverMsgs, m)
		}
	}
	require.Len(t, clientMsgs, 2, "two client messages: hello + fragmented hello")
	assert.Equal(t, "hello", string(clientMsgs[0].Payload))
	assert.Equal(t, "hello", string(clientMsgs[1].Payload), "fragmented message reassembled")
	require.Len(t, serverMsgs, 2, "echo of hello + reassembled echo of fragments")
	assert.Equal(t, "hello", string(serverMsgs[0].Payload))
	assert.Equal(t, "hello", string(serverMsgs[1].Payload))
}

func TestWebSocketCaptureTruncationAndCap(t *testing.T) {
	echo := startWSEchoServer(t)

	sessionCh := make(chan WebSocketSession, 8)
	wsInterceptor := func(s WebSocketSession) { sessionCh <- s }

	// 8-byte message limit, 1-message session cap.
	proxyAddr, _ := startTestProxy(t, echo.URL, nil, wsInterceptor, 8, 1)
	conn, br := wsDial(t, proxyAddr, "/ws")

	// Oversized message: 20 bytes sent, truncated to 8 on record, forwarded
	// in full (echo must be byte-identical, masked payload included).
	big := bytes.Repeat([]byte("x"), 20)
	sent := buildWSFrame(t, true, WSOpcodeBinary, big, true)
	_, err := conn.Write(sent)
	require.NoError(t, err)
	echoed := make([]byte, len(sent))
	_, err = readFullFrom(br, echoed)
	require.NoError(t, err)
	assert.Equal(t, sent, echoed, "full payload must be forwarded")

	// Second message: forwarded but not recorded (cap reached).
	_, err = conn.Write(buildWSFrame(t, true, WSOpcodeText, []byte("two"), true))
	require.NoError(t, err)
	hdr, err := ReadWSFrameHeader(br)
	require.NoError(t, err)
	assert.Equal(t, byte(WSOpcodeText), hdr.Opcode)
	_, _ = br.Discard(int(hdr.PayloadLen))

	_ = conn.Close()
	got := waitForSessions(t, sessionCh, 1)
	require.Len(t, got, 1)

	require.Len(t, got[0].Messages, 1, "only one message recorded (cap)")
	msg := got[0].Messages[0]
	assert.True(t, msg.Truncated, "oversized message marked truncated")
	assert.Equal(t, 8, len(msg.Payload))
	assert.Equal(t, WSOpcodeBinary, msg.Opcode)
}

func TestWebSocketUpgradeFailureCapturedAsTransaction(t *testing.T) {
	// A target that rejects upgrades: the exchange must be captured as a
	// normal HTTP transaction.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"no upgrade"}`))
	}))
	t.Cleanup(api.Close)

	txCh := make(chan APITransaction, 4)
	sessionCh := make(chan WebSocketSession, 4)
	proxyAddr, _ := startTestProxy(t, api.URL,
		func(tx APITransaction) { txCh <- tx },
		func(s WebSocketSession) { sessionCh <- s },
		0, 0)

	conn, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", proxyAddr)
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	select {
	case tx := <-txCh:
		assert.Equal(t, "/ws", tx.Request.Path)
		assert.Equal(t, http.StatusBadRequest, tx.Response.StatusCode)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP transaction capture")
	}
	select {
	case s := <-sessionCh:
		t.Fatalf("no WebSocket session expected, got %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// readFullFrom reads len(dst) bytes from r.
func readFullFrom(r *bufio.Reader, dst []byte) (int, error) {
	total := 0
	for total < len(dst) {
		n, err := r.Read(dst[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
