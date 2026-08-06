package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/parnexcodes/swag-doc/pkg/logger"
)

// RequestData stores information about an HTTP request
type RequestData struct {
	Method      string
	Path        string
	QueryParams url.Values
	Headers     http.Header
	Body        []byte
	Timestamp   time.Time
}

// ResponseData stores information about an HTTP response
type ResponseData struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Timestamp  time.Time
}

// APITransaction represents a complete API transaction (request + response)
type APITransaction struct {
	Request  RequestData
	Response ResponseData
	// ResponseTruncated is true when the captured response body was cut at
	// the proxy's capture limit; the full response was still forwarded.
	ResponseTruncated bool `json:"response_truncated,omitempty"`
}

// APIInterceptor is a function that processes API transactions
type APIInterceptor func(APITransaction)

// DefaultMaxBodyBytes is the default capture limit for response bodies.
const DefaultMaxBodyBytes int64 = 10 << 20 // 10 MiB

// ProxyServer is an HTTP proxy server that captures API traffic
type ProxyServer struct {
	port        int
	targetURL   *url.URL
	proxy       *httputil.ReverseProxy
	interceptor APIInterceptor
	// MaxBodyBytes bounds how much of a response body is captured. The full
	// response is still forwarded to the client. <= 0 means unlimited.
	MaxBodyBytes int64
	// WSInterceptor receives captured WebSocket sessions when connections
	// close. Nil means WebSocket sessions are not stored.
	WSInterceptor WebSocketInterceptor
	// MaxWSMessageBytes bounds how much of a WebSocket message payload is
	// captured per message. The full payload is still forwarded. <= 0 means
	// unlimited.
	MaxWSMessageBytes int64
	// MaxWSMessages bounds how many WebSocket messages are captured per
	// connection; forwarding continues unchanged after the limit. <= 0 means
	// unlimited.
	MaxWSMessages int

	// wsTransport forwards upgrade requests to the target and returns the
	// raw connection for the 101 response.
	wsTransport *http.Transport
	// wsMu guards wsConns; wsWG tracks in-flight WebSocket sessions so
	// graceful shutdown can close them and wait for their session records.
	wsMu    sync.Mutex
	wsConns map[*wsConnection]struct{}
	wsWG    sync.WaitGroup
}

// NewProxyServer creates a new proxy server
func NewProxyServer(port int, target string, interceptor APIInterceptor) (*ProxyServer, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	return &ProxyServer{
		port:         port,
		targetURL:    targetURL,
		proxy:        proxy,
		interceptor:  interceptor,
		MaxBodyBytes: DefaultMaxBodyBytes,
		// Defaults mirror the storage layer's capture limits.
		MaxWSMessageBytes: DefaultMaxWSMessageBytes,
		MaxWSMessages:     DefaultMaxWSMessages,
		wsTransport:       http.DefaultTransport.(*http.Transport).Clone(),
		wsConns:           make(map[*wsConnection]struct{}),
	}, nil
}

// Start starts the proxy server with graceful shutdown support
func (p *ProxyServer) Start() error {
	addr := fmt.Sprintf(":%d", p.port)
	srv := &http.Server{
		Addr:    addr,
		Handler: p.handler(),
	}

	// Channel to receive errors from the server
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting proxy server on %s", addr)
		errChan <- srv.ListenAndServe()
	}()

	// Listen for interrupt signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Shutdown does not track hijacked connections; close them so their
		// sessions are stored before the storage final flush.
		p.closeAllWSConnections()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		// Wait for in-flight WebSocket sessions to be recorded.
		p.wsWG.Wait()
	}

	select {
	case err := <-errChan:
		shutdown()
		return err
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down gracefully...", sig)
		shutdown()
		return nil
	}
}

// handler returns the HTTP handler that captures API transactions and
// WebSocket sessions. WebSocket upgrades are routed to the dedicated capture
// path: the ReverseProxy would hijack the connection without recording it.
func (p *ProxyServer) handler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			p.handleWebSocket(w, r)
			return
		}

		// Capture the request
		reqData, err := captureRequest(r)
		if err != nil {
			log.Printf("Error capturing request: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Create a custom response writer to capture the response
		rw := newResponseWriter(w, p.MaxBodyBytes)

		// Forward the request to the target server
		p.proxy.ServeHTTP(rw, r)

		// Capture the response
		respData := captureResponse(rw)

		// Create a complete transaction and pass it to the interceptor
		transaction := APITransaction{
			Request:           reqData,
			Response:          respData,
			ResponseTruncated: rw.Truncated(),
		}

		// Log the captured request to the console
		logTransaction(transaction)

		// Pass the transaction to the interceptor
		if p.interceptor != nil {
			p.interceptor(transaction)
		}
	})
}

// closeAllWSConnections closes every active WebSocket connection so the
// pumps observe EOF and store their sessions.
func (p *ProxyServer) closeAllWSConnections() {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	for conn := range p.wsConns {
		_ = conn.client.Close()
		_ = conn.target.Close()
	}
}

func (p *ProxyServer) trackWS(conn *wsConnection) {
	p.wsMu.Lock()
	p.wsConns[conn] = struct{}{}
	p.wsMu.Unlock()
	p.wsWG.Add(1)
}

func (p *ProxyServer) untrackWS(conn *wsConnection) {
	p.wsMu.Lock()
	delete(p.wsConns, conn)
	p.wsMu.Unlock()
	p.wsWG.Done()
}

// handleWebSocket captures a WebSocket upgrade: it forwards the handshake to
// the target, relays the 101 response to the client, hijacks the connection,
// and pumps frames in both directions while recording them.
func (p *ProxyServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	reqData, err := captureRequest(r)
	if err != nil {
		log.Printf("Error capturing WebSocket request: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	outReq := r.Clone(r.Context())
	outReq.URL.Scheme = p.targetURL.Scheme
	outReq.URL.Host = p.targetURL.Host
	outReq.Host = p.targetURL.Host
	outReq.RequestURI = ""
	// Drop hop-by-hop headers except Connection/Upgrade, which the transport
	// needs to perform the upgrade.
	outReq.Header.Del("Proxy-Connection")
	outReq.Header.Del("Keep-Alive")
	outReq.Header.Del("TE")
	outReq.Header.Del("Trailer")
	outReq.Header.Del("Transfer-Encoding")

	resp, err := p.wsTransport.RoundTrip(outReq)
	if err != nil {
		log.Printf("WebSocket upgrade to %s failed: %v", p.targetURL, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		// The upgrade did not complete: record the exchange as an ordinary
		// HTTP transaction, exactly as before WebSocket support.
		rw := newResponseWriter(w, p.MaxBodyBytes)
		copyHeader(rw.Header(), resp.Header)
		rw.WriteHeader(resp.StatusCode)
		if resp.Body != nil {
			_, _ = io.Copy(rw, resp.Body)
			_ = resp.Body.Close()
		}
		transaction := APITransaction{
			Request:           reqData,
			Response:          captureResponse(rw),
			ResponseTruncated: rw.Truncated(),
		}
		logTransaction(transaction)
		if p.interceptor != nil {
			p.interceptor(transaction)
		}
		return
	}

	// Hijack the client connection, then relay the 101 response over it —
	// the same ordering httputil.ReverseProxy uses for upgrades.
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = resp.Body.Close()
		log.Printf("ResponseWriter does not support hijacking")
		return
	}
	clientConn, brw, err := hj.Hijack()
	if err != nil {
		_ = resp.Body.Close()
		log.Printf("Error hijacking WebSocket connection: %v", err)
		return
	}

	targetConn, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		_ = resp.Body.Close()
		_ = clientConn.Close()
		log.Printf("Upgrade response body is not a raw connection")
		return
	}

	if err := writeSwitchProtocolsResponse(brw, resp); err != nil {
		log.Printf("Error writing WebSocket handshake response: %v", err)
		_ = resp.Body.Close()
		_ = clientConn.Close()
		return
	}

	session := &WebSocketSession{
		HandshakeRequest: reqData,
		ResponseStatus:   resp.StatusCode,
		ResponseHeaders:  sanitizeHeaders(resp.Header),
		Subprotocol:      resp.Header.Get("Sec-WebSocket-Protocol"),
	}

	conn := &wsConnection{
		client:      clientConn,
		clientR:     brw.Reader,
		target:      targetConn,
		targetR:     bufio.NewReader(targetConn),
		session:     session,
		maxMsgBytes: p.MaxWSMessageBytes,
		maxMsgs:     p.MaxWSMessages,
	}
	p.trackWS(conn)
	conn.run(p.WSInterceptor)
	p.untrackWS(conn)
}

// writeSwitchProtocolsResponse writes a 101 response over the hijacked
// connection. resp.Write cannot be used: for an upgrade response the body is
// the raw target connection, which must not be copied.
func writeSwitchProtocolsResponse(brw *bufio.ReadWriter, resp *http.Response) error {
	if _, err := fmt.Fprintf(brw, "HTTP/1.1 %s\r\n", resp.Status); err != nil {
		return err
	}
	if err := resp.Header.Write(brw); err != nil {
		return err
	}
	if _, err := brw.WriteString("\r\n"); err != nil {
		return err
	}
	return brw.Flush()
}

// copyHeader copies all header values from src to dst.
func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// captureRequest captures data from an HTTP request
func captureRequest(r *http.Request) (RequestData, error) {
	// Read the request body
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return RequestData{}, fmt.Errorf("reading request body: %w", err)
		}
		// Restore the body for further processing
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// Store raw body — sanitization happens during generation
	reqData := RequestData{
		Method:      r.Method,
		Path:        r.URL.Path,
		QueryParams: sanitizeQueryParams(r.URL.Query()),
		Headers:     sanitizeHeaders(r.Header),
		Body:        bodyBytes,
		Timestamp:   time.Now(),
	}

	// Restore the body without copying the bytes (io.ReadAll already owns them)
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return reqData, nil
}

// responseWriter is a custom ResponseWriter that captures the response
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
	maxBody    int64
	captured   int64
	truncated  bool
}

// newResponseWriter creates a new responseWriter with the default capture limit
func newResponseWriter(w http.ResponseWriter, maxBody int64) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default status code
		body:           &bytes.Buffer{},
		maxBody:        maxBody,
	}
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the response body, forwarding the full body to the client but
// retaining at most maxBody bytes for capture.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.maxBody > 0 && rw.captured >= rw.maxBody {
		rw.truncated = true
	} else {
		n := len(b)
		if rw.maxBody > 0 {
			if remaining := rw.maxBody - rw.captured; int64(n) > remaining {
				n = int(remaining)
				rw.truncated = true
			}
		}
		rw.body.Write(b[:n])
		rw.captured += int64(n)
	}
	// Write the full body to the original response writer
	return rw.ResponseWriter.Write(b)
}

// Truncated reports whether the captured body was cut at the capture limit.
func (rw *responseWriter) Truncated() bool {
	return rw.truncated
}

// Flush implements http.Flusher for SSE/streaming support
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijack")
}

// captureResponse captures data from the response
func captureResponse(rw *responseWriter) ResponseData {
	// Store raw body — sanitization happens during generation
	body := rw.body.Bytes()

	return ResponseData{
		StatusCode: rw.statusCode,
		Headers:    sanitizeHeaders(rw.Header()),
		Body:       body,
		Timestamp:  time.Now(),
	}
}

// sanitizeJSON preserves the structure of JSON data but replaces values with type placeholders
func sanitizeJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}

	sanitized := sanitizeValue(obj)
	return json.Marshal(sanitized)
}

// sanitizeValue replaces actual values with type placeholders
func sanitizeValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = sanitizeValue(val)
		}
		return result
	case []interface{}:
		if len(v) == 0 {
			return []interface{}{}
		}
		// Keep up to 5 elements for better schema inference
		maxElements := 5
		if len(v) < maxElements {
			maxElements = len(v)
		}
		result := make([]interface{}, maxElements)
		for i := 0; i < maxElements; i++ {
			result[i] = sanitizeValue(v[i])
		}
		return result
	case string:
		return "__string__"
	case float64:
		if v == float64(int(v)) {
			return "__integer__"
		}
		return "__number__"
	case bool:
		return "__boolean__"
	default:
		return "__unknown__"
	}
}

// sanitizeHeaders removes sensitive values from headers
func sanitizeHeaders(headers http.Header) http.Header {
	sanitized := make(http.Header)
	sensitiveHeaders := map[string]bool{
		"Authorization":       true,
		"Cookie":              true,
		"Set-Cookie":          true,
		"X-Api-Key":           true,
		"X-Auth-Token":        true,
		"Api-Key":             true,
		"X-Auth":              true,
		"Token":               true,
		"Password":            true,
		"Secret":              true,
		"Credentials":         true,
		"Private-Key":         true,
		"Session":             true,
		"Access-Token":        true,
		"Refresh-Token":       true,
		"Authentication":      true,
		"Authentication-Info": true,
	}

	for key, values := range headers {
		if sensitiveHeaders[key] {
			sanitized[key] = []string{"__redacted__"}
		} else {
			sanitized[key] = values
		}
	}
	return sanitized
}

// sanitizeQueryParams removes sensitive values from query parameters
func sanitizeQueryParams(params url.Values) url.Values {
	sanitized := make(url.Values)
	sensitiveParams := map[string]bool{
		"token":         true,
		"key":           true,
		"api_key":       true,
		"apikey":        true,
		"password":      true,
		"secret":        true,
		"access_token":  true,
		"refresh_token": true,
		"auth":          true,
		"auth_token":    true,
		"session":       true,
		"credential":    true,
		"credentials":   true,
	}

	for key, values := range params {
		if sensitiveParams[key] {
			sanitized[key] = []string{"__redacted__"}
		} else {
			newValues := make([]string, len(values))
			for i, v := range values {
				newValues[i] = sanitizeQueryParamValue(v)
			}
			sanitized[key] = newValues
		}
	}
	return sanitized
}

// sanitizeQueryParamValue infers the type of a query parameter value and returns a placeholder
func sanitizeQueryParamValue(value string) string {
	// Check for boolean
	if value == "true" || value == "false" {
		return "__boolean__"
	}
	// Check for integer
	if _, err := strconv.Atoi(value); err == nil {
		return "__integer__"
	}
	// Check for float
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return "__number__"
	}
	return "__string__"
}

// logTransaction logs the captured transaction details to the console
func logTransaction(tx APITransaction) {
	// Get content type
	contentType := "unknown"
	if ct, ok := tx.Response.Headers["Content-Type"]; ok && len(ct) > 0 {
		contentType = ct[0]
	}

	// Use our new logger to print the request log
	logger.PrintRequestLog(tx.Request.Method, tx.Request.Path, tx.Response.StatusCode, contentType)
}
