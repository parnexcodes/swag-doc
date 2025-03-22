package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
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
}

// APIInterceptor is a function that processes API transactions
type APIInterceptor func(APITransaction)

// ProxyServer is an HTTP proxy server that captures API traffic
type ProxyServer struct {
	port        int
	targetURL   *url.URL
	proxy       *httputil.ReverseProxy
	interceptor APIInterceptor
}

// NewProxyServer creates a new proxy server
func NewProxyServer(port int, target string, interceptor APIInterceptor) (*ProxyServer, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	return &ProxyServer{
		port:        port,
		targetURL:   targetURL,
		proxy:       proxy,
		interceptor: interceptor,
	}, nil
}

// Start starts the proxy server
func (p *ProxyServer) Start() error {
	// Create a custom handler that wraps the proxy
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request
		reqData, err := captureRequest(r)
		if err != nil {
			log.Printf("Error capturing request: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Create a custom response writer to capture the response
		rw := newResponseWriter(w)

		// Forward the request to the target server
		p.proxy.ServeHTTP(rw, r)

		// Capture the response
		respData := captureResponse(rw)

		// Create a complete transaction and pass it to the interceptor
		transaction := APITransaction{
			Request:  reqData,
			Response: respData,
		}

		// Pass the transaction to the interceptor
		if p.interceptor != nil {
			p.interceptor(transaction)
		}
	})

	// Start the server
	addr := fmt.Sprintf(":%d", p.port)
	log.Printf("Starting proxy server on %s", addr)
	return http.ListenAndServe(addr, handler)
}

// captureRequest captures data from an HTTP request
func captureRequest(r *http.Request) (RequestData, error) {
	// Read the request body
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, _ = io.ReadAll(r.Body)
		// Restore the body for further processing
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// Sanitize the body to remove actual data values
	sanitizedBody, err := sanitizeJSON(bodyBytes)
	if err != nil {
		// If there's an error sanitizing, still capture the request but with empty body
		sanitizedBody = []byte{}
	}

	// Create a RequestData object with sanitized body
	reqData := RequestData{
		Method:      r.Method,
		Path:        r.URL.Path,
		QueryParams: sanitizeQueryParams(r.URL.Query()),
		Headers:     sanitizeHeaders(r.Header),
		Body:        sanitizedBody,
		Timestamp:   time.Now(),
	}

	return reqData, nil
}

// responseWriter is a custom ResponseWriter that captures the response
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

// newResponseWriter creates a new responseWriter
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default status code
		body:           &bytes.Buffer{},
	}
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the response body
func (rw *responseWriter) Write(b []byte) (int, error) {
	// Write to our buffer
	rw.body.Write(b)
	// Write to the original response writer
	return rw.ResponseWriter.Write(b)
}

// captureResponse captures data from the response
func captureResponse(rw *responseWriter) ResponseData {
	// Sanitize the body to remove actual data values
	sanitizedBody, err := sanitizeJSON(rw.body.Bytes())
	if err != nil {
		// If there's an error sanitizing, still capture the response but with empty body
		sanitizedBody = []byte{}
	}

	return ResponseData{
		StatusCode: rw.statusCode,
		Headers:    sanitizeHeaders(rw.ResponseWriter.Header()),
		Body:       sanitizedBody,
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
		// For arrays, we only need one sample to infer schema
		return []interface{}{sanitizeValue(v[0])}
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
			for i := range values {
				newValues[i] = "__string__"
			}
			sanitized[key] = newValues
		}
	}
	return sanitized
}
