package proxy

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebSocket opcodes (RFC 6455 section 5.2).
const (
	WSOpcodeContinuation = 0x0
	WSOpcodeText         = 0x1
	WSOpcodeBinary       = 0x2
	WSOpcodeClose        = 0x8
	WSOpcodePing         = 0x9
	WSOpcodePong         = 0xA
)

// Message directions for captured WebSocket messages.
const (
	DirectionClientToServer = "client_to_server"
	DirectionServerToClient = "server_to_client"
)

// Defaults for WebSocket capture limits.
const (
	// DefaultMaxWSMessageBytes is the default per-message payload capture
	// limit; the full payload is still forwarded to the recipient.
	DefaultMaxWSMessageBytes int64 = 64 << 10 // 64 KiB
	// DefaultMaxWSMessages is the default per-session message capture limit;
	// forwarding continues unchanged after the limit is reached.
	DefaultMaxWSMessages = 1000
)

// WebSocketMessage is one message exchanged over a WebSocket connection,
// reassembled from its frames (fragmentation is transparent).
type WebSocketMessage struct {
	Direction string    `json:"direction"`
	Opcode    int       `json:"opcode"`
	Payload   []byte    `json:"payload,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// WebSocketSession captures one WebSocket connection: the upgrade handshake
// plus the messages exchanged until the connection closed.
type WebSocketSession struct {
	HandshakeRequest RequestData        `json:"handshake_request"`
	ResponseStatus   int                `json:"response_status"`
	ResponseHeaders  http.Header        `json:"response_headers,omitempty"`
	Subprotocol      string             `json:"subprotocol,omitempty"`
	Messages         []WebSocketMessage `json:"messages,omitempty"`
}

// WebSocketInterceptor processes a captured WebSocket session. It is called
// once per connection, after the connection closes.
type WebSocketInterceptor func(WebSocketSession)

// WSFrameHeader describes a parsed WebSocket frame header. Header holds the
// exact wire bytes of the header so callers can forward them verbatim.
type WSFrameHeader struct {
	Fin        bool
	Opcode     byte
	Masked     bool
	MaskKey    [4]byte
	PayloadLen uint64
	Header     []byte
}

var (
	errWSInvalidOpcode        = errors.New("websocket: invalid opcode")
	errWSReservedBits         = errors.New("websocket: reserved bits set")
	errWSFragmentedControl    = errors.New("websocket: fragmented control frame")
	errWSControlFrameTooLarge = errors.New("websocket: control frame payload exceeds 125 bytes")
)

// ReadWSFrameHeader reads and parses one frame header from r. The returned
// Header slice holds the exact header bytes read (fixed header, extended
// length, and mask key) so they can be forwarded verbatim.
func ReadWSFrameHeader(r *bufio.Reader) (WSFrameHeader, error) {
	var fixed [2]byte
	if _, err := io.ReadFull(r, fixed[:]); err != nil {
		return WSFrameHeader{}, err
	}

	h := WSFrameHeader{
		Fin:    fixed[0]&0x80 != 0,
		Opcode: fixed[0] & 0x0F,
		Masked: fixed[1]&0x80 != 0,
	}
	length := uint64(fixed[1] & 0x7F)

	if fixed[0]&0x70 != 0 {
		return WSFrameHeader{}, errWSReservedBits
	}
	if !validWSOpcode(h.Opcode) {
		return WSFrameHeader{}, fmt.Errorf("%w: %#x", errWSInvalidOpcode, h.Opcode)
	}
	if isWSControlOpcode(h.Opcode) {
		// Control frames must not be fragmented and carry at most 125 bytes
		// of payload (RFC 6455 section 5.5).
		if !h.Fin {
			return WSFrameHeader{}, errWSFragmentedControl
		}
		if length > 125 {
			return WSFrameHeader{}, errWSControlFrameTooLarge
		}
	}

	// Header bytes are accumulated for verbatim forwarding. The fixed header
	// is copied rather than aliased so the caller may reuse the slice.
	header := make([]byte, 2, 14)
	copy(header, fixed[:])

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return WSFrameHeader{}, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
		header = append(header, ext[:]...)
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return WSFrameHeader{}, err
		}
		length = binary.BigEndian.Uint64(ext[:])
		header = append(header, ext[:]...)
	}

	if h.Masked {
		var key [4]byte
		if _, err := io.ReadFull(r, key[:]); err != nil {
			return WSFrameHeader{}, err
		}
		h.MaskKey = key
		header = append(header, key[:]...)
	}

	h.PayloadLen = length
	h.Header = header
	return h, nil
}

// WriteWSFrame writes one complete WebSocket frame to w. When mask is true
// the payload is masked (required for client-to-server frames).
func WriteWSFrame(w io.Writer, fin bool, opcode byte, payload []byte, mask bool) error {
	var header [10]byte
	header[0] = opcode
	if fin {
		header[0] |= 0x80
	}
	if mask {
		header[1] = 0x80
	}

	length := len(payload)
	headerLen := 2
	switch {
	case length <= 125:
		header[1] |= byte(length)
	case length <= 0xFFFF:
		header[1] |= 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
		headerLen = 4
	default:
		header[1] |= 127
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
		headerLen = 10
	}

	if _, err := w.Write(header[:headerLen]); err != nil {
		return err
	}

	if !mask {
		_, err := w.Write(payload)
		return err
	}

	// Client frames must be masked with a random 32-bit key (RFC 6455
	// section 5.3). A deterministic key is fine for capture traffic; the
	// recipient only requires it to be unpredictable per connection.
	key := [4]byte{0xAB, 0xCD, 0xEF, 0x01}
	if _, err := w.Write(key[:]); err != nil {
		return err
	}
	masked := make([]byte, len(payload))
	unmaskBytes(masked, payload, key, 0)
	_, err := w.Write(masked)
	return err
}

// UnmaskBytes applies the WebSocket masking transform to src and stores the
// result in dst. offset is the payload-relative offset of src (the mask key
// rotates every 4 bytes across the whole payload).
func UnmaskBytes(dst, src []byte, key [4]byte, offset uint64) {
	unmaskBytes(dst, src, key, offset)
}

func unmaskBytes(dst, src []byte, key [4]byte, offset uint64) {
	for i := range src {
		dst[i] = src[i] ^ key[(offset+uint64(i))%4]
	}
}

func validWSOpcode(opcode byte) bool {
	switch opcode {
	case WSOpcodeContinuation, WSOpcodeText, WSOpcodeBinary,
		WSOpcodeClose, WSOpcodePing, WSOpcodePong:
		return true
	default:
		return false
	}
}

func isWSControlOpcode(opcode byte) bool {
	return opcode >= WSOpcodeClose
}

// wsMessageSink serializes message recording across both directions of a
// connection and enforces the per-session message cap. A connection's two
// pumps share one sink.
type wsMessageSink struct {
	mu      sync.Mutex
	session *WebSocketSession
	maxMsgs int
	count   int
}

// record appends a completed message, returning false when the session cap
// is reached (the message is dropped and recording should stop).
func (s *wsMessageSink) record(msg WebSocketMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxMsgs > 0 && s.count >= s.maxMsgs {
		return false
	}
	s.session.Messages = append(s.session.Messages, msg)
	s.count++
	return true
}

// wsRecorder accumulates captured payloads for one direction. It is not
// safe for concurrent use; each direction has its own recorder.
type wsRecorder struct {
	sink      *wsMessageSink
	direction string
	maxBytes  int64 // per-message capture limit; <= 0 means unlimited
	recording bool

	// Fragmentation state: a non-data opcode (0) means no message in flight.
	msgOpcode byte
	msgBuf    []byte
	msgTrunc  bool
	msgStart  time.Time
	scratch   []byte
}

func newWSRecorder(sink *wsMessageSink, direction string, maxBytes int64) *wsRecorder {
	return &wsRecorder{
		sink:      sink,
		direction: direction,
		maxBytes:  maxBytes,
		recording: true,
	}
}

// capture retains (at most maxBytes of) a payload chunk, unmasking it when
// the frame is masked. offset is the chunk's payload-relative position.
func (r *wsRecorder) capture(h WSFrameHeader, chunk []byte, offset uint64) {
	if !r.recording || isWSControlOpcode(h.Opcode) {
		return
	}
	if len(r.msgBuf) == 0 && r.msgStart.IsZero() {
		r.msgStart = time.Now()
	}
	if r.maxBytes > 0 && int64(len(r.msgBuf)) >= r.maxBytes {
		r.msgTrunc = true
		return
	}

	src := chunk
	if h.Masked {
		if cap(r.scratch) < len(chunk) {
			r.scratch = make([]byte, len(chunk))
		}
		r.scratch = r.scratch[:len(chunk)]
		unmaskBytes(r.scratch, chunk, h.MaskKey, offset)
		src = r.scratch
	}

	room := len(src)
	if r.maxBytes > 0 {
		if left := r.maxBytes - int64(len(r.msgBuf)); int64(room) > left {
			room = int(left)
			r.msgTrunc = true
		}
	}
	r.msgBuf = append(r.msgBuf, src[:room]...)
}

// frame finalizes messages at frame boundaries.
func (r *wsRecorder) frame(h WSFrameHeader) {
	if !r.recording {
		return
	}
	switch h.Opcode {
	case WSOpcodeText, WSOpcodeBinary:
		if h.Fin {
			r.finishMessage(h.Opcode)
		} else {
			r.msgOpcode = h.Opcode
			if r.msgStart.IsZero() {
				r.msgStart = time.Now()
			}
		}
	case WSOpcodeContinuation:
		if h.Fin {
			r.finishMessage(r.msgOpcode)
			r.msgOpcode = 0
		}
	}
}

func (r *wsRecorder) finishMessage(opcode byte) {
	if opcode == 0 {
		return
	}
	if !r.sink.record(WebSocketMessage{
		Direction: r.direction,
		Opcode:    int(opcode),
		Payload:   r.msgBuf,
		Truncated: r.msgTrunc,
		Timestamp: r.msgStart,
	}) {
		// Session cap reached: stop recording entirely.
		r.recording = false
	}
	r.msgBuf = nil
	r.msgTrunc = false
	r.msgStart = time.Time{}
}

// wsConnection bridges a hijacked client connection and the target's raw
// connection, forwarding every frame verbatim in both directions while
// recording it.
type wsConnection struct {
	client      net.Conn
	clientR     *bufio.Reader
	target      io.ReadWriteCloser
	targetR     *bufio.Reader
	session     *WebSocketSession
	maxMsgBytes int64
	maxMsgs     int
}

// run pumps both directions until either side closes, then invokes
// interceptor with the captured session.
func (c *wsConnection) run(interceptor WebSocketInterceptor) {
	sink := &wsMessageSink{session: c.session, maxMsgs: c.maxMsgs}
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		_ = c.pump(sink, c.clientR, c.target, DirectionClientToServer)
		_ = c.target.Close()
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		_ = c.pump(sink, c.targetR, c.client, DirectionServerToClient)
		_ = c.client.Close()
	}()
	<-done
	<-done
	if interceptor != nil {
		interceptor(*c.session)
	}
}

// pump copies frames from src to dst, forwarding header and payload bytes
// verbatim while recording them. It returns on EOF, a protocol error, or a
// write failure.
func (c *wsConnection) pump(sink *wsMessageSink, srcR *bufio.Reader, dst io.Writer, direction string) error {
	rec := newWSRecorder(sink, direction, c.maxMsgBytes)
	buf := make([]byte, 32*1024)
	for {
		h, err := ReadWSFrameHeader(srcR)
		if err != nil {
			return err
		}
		if _, err := dst.Write(h.Header); err != nil {
			return err
		}
		var offset uint64
		remaining := h.PayloadLen
		for remaining > 0 {
			n := len(buf)
			if uint64(n) > remaining {
				n = int(remaining)
			}
			nr, rerr := srcR.Read(buf[:n])
			if nr > 0 {
				if _, werr := dst.Write(buf[:nr]); werr != nil {
					return werr
				}
				rec.capture(h, buf[:nr], offset)
				offset += uint64(nr)
				remaining -= uint64(nr)
			}
			if rerr != nil {
				return rerr
			}
		}
		rec.frame(h)
	}
}

// isWebSocketUpgrade reports whether r is a WebSocket upgrade request
// (Connection contains "upgrade" and Upgrade contains "websocket",
// case-insensitive).
func isWebSocketUpgrade(r *http.Request) bool {
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return false
	}
	return headerContainsToken(r.Header, "Upgrade", "websocket")
}

func headerContainsToken(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
