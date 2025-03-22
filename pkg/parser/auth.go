package parser

import (
	"fmt"
	"net/http"
	"strings"
)

// AuthScheme represents an authentication scheme
type AuthScheme struct {
	Type        string // http, apiKey, oauth2, openIdConnect
	Scheme      string // basic, bearer, digest (for http type)
	Description string
	In          string // header, query, cookie (for apiKey type)
	Name        string // header name or query parameter name
	Format      string // JWT, etc. (for bearer token)
	Example     string
}

// AuthDetector analyzes API traffic to detect authentication schemes
type AuthDetector struct {
	detectedSchemes map[string]AuthScheme
}

// NewAuthDetector creates a new auth detector
func NewAuthDetector() *AuthDetector {
	return &AuthDetector{
		detectedSchemes: make(map[string]AuthScheme),
	}
}

// AnalyzeTransaction analyzes an API transaction for authentication information
func (d *AuthDetector) AnalyzeTransaction(req *http.Request, resp *http.Response) {
	d.analyzeHeaders(req.Header)
	d.analyzeQueryParams(req.URL.Query())
	if resp != nil {
		d.analyzeResponseHeaders(resp.Header)
	}
}

// analyzeHeaders analyzes request headers for authentication information
func (d *AuthDetector) analyzeHeaders(headers http.Header) {
	// Check Authorization header
	authHeader := headers.Get("Authorization")
	if authHeader != "" {
		d.analyzeAuthorizationHeader(authHeader)
	}

	// Check for API key in headers (common patterns)
	apiKeyHeaders := []string{
		"X-API-Key", "API-Key", "api-key", "apikey",
		"X-Auth-Token", "x-auth-token",
		"App-Key", "app-key", "AppKey",
	}

	for _, header := range apiKeyHeaders {
		if value := headers.Get(header); value != "" {
			d.analyzeApiKeyHeader(header, value)
		}
	}

	// Check for custom auth headers (format X-*)
	for header := range headers {
		if strings.HasPrefix(strings.ToLower(header), "x-") && !contains(apiKeyHeaders, header) {
			// Potential custom auth header
			if value := headers.Get(header); value != "" {
				d.analyzeCustomHeader(header, value)
			}
		}
	}
}

// analyzeQueryParams analyzes query parameters for authentication information
func (d *AuthDetector) analyzeQueryParams(params map[string][]string) {
	// Common API key parameter names
	apiKeyParams := []string{
		"api_key", "apikey", "api-key", "key",
		"token", "access_token", "auth_token",
	}

	for _, param := range apiKeyParams {
		if values, exists := params[param]; exists && len(values) > 0 {
			d.analyzeApiKeyParam(param, values[0])
		}
	}
}

// analyzeResponseHeaders analyzes response headers for authentication information
func (d *AuthDetector) analyzeResponseHeaders(headers http.Header) {
	// Check WWW-Authenticate header
	if wwwAuth := headers.Get("WWW-Authenticate"); wwwAuth != "" {
		d.parseWWWAuthenticateHeader(wwwAuth)
	}
}

// parseWWWAuthenticateHeader parses the WWW-Authenticate header
func (d *AuthDetector) parseWWWAuthenticateHeader(value string) {
	// Basic structure: scheme [realm="value"]
	parts := strings.SplitN(value, " ", 2)
	if len(parts) < 1 {
		return
	}

	scheme := strings.ToLower(parts[0])

	switch scheme {
	case "basic":
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "basic",
			Description: "Basic authentication",
			In:          "header",
			Name:        "Authorization",
		})
	case "bearer":
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "bearer",
			Description: "Bearer token authentication",
			In:          "header",
			Name:        "Authorization",
		})
	case "digest":
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "digest",
			Description: "Digest authentication",
			In:          "header",
			Name:        "Authorization",
		})
	}
}

// analyzeAuthorizationHeader analyzes the Authorization header
func (d *AuthDetector) analyzeAuthorizationHeader(header string) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) < 2 {
		return
	}

	scheme := strings.ToLower(parts[0])
	value := parts[1]

	switch scheme {
	case "basic":
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "basic",
			Description: "Basic authentication using base64 encoded credentials",
			In:          "header",
			Name:        "Authorization",
		})
	case "bearer":
		description := "Bearer token authentication"
		if isJWTToken(value) {
			description = "Bearer token authentication using JWT"
		}

		format := ""
		if isJWTToken(value) {
			format = "JWT"
		}

		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "bearer",
			Description: description,
			Format:      format,
			In:          "header",
			Name:        "Authorization",
		})
	case "digest":
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      "digest",
			Description: "Digest access authentication",
			In:          "header",
			Name:        "Authorization",
		})
	default:
		// Handle custom authorization schemes
		d.addAuthScheme(AuthScheme{
			Type:        "http",
			Scheme:      strings.ToLower(parts[0]),
			Description: fmt.Sprintf("Custom authentication scheme: %s", parts[0]),
			In:          "header",
			Name:        "Authorization",
		})
	}
}

// analyzeApiKeyHeader analyzes potential API key headers
func (d *AuthDetector) analyzeApiKeyHeader(header, value string) {
	d.addAuthScheme(AuthScheme{
		Type:        "apiKey",
		Description: fmt.Sprintf("API key authentication via %s header", header),
		In:          "header",
		Name:        header,
		Example:     sanitizeExample(value),
	})
}

// analyzeCustomHeader analyzes custom authentication headers
func (d *AuthDetector) analyzeCustomHeader(header, value string) {
	// Check if it looks like an auth header
	if len(value) > 10 && !strings.Contains(strings.ToLower(header), "version") {
		d.addAuthScheme(AuthScheme{
			Type:        "apiKey",
			Description: fmt.Sprintf("Custom authentication via %s header", header),
			In:          "header",
			Name:        header,
			Example:     sanitizeExample(value),
		})
	}
}

// analyzeApiKeyParam analyzes API key query parameters
func (d *AuthDetector) analyzeApiKeyParam(param, value string) {
	d.addAuthScheme(AuthScheme{
		Type:        "apiKey",
		Description: fmt.Sprintf("API key authentication via %s query parameter", param),
		In:          "query",
		Name:        param,
		Example:     sanitizeExample(value),
	})
}

// isJWTToken checks if a token looks like a JWT
func isJWTToken(token string) bool {
	// Simple check: JWTs have three segments separated by dots
	parts := strings.Split(token, ".")
	return len(parts) == 3
}

// sanitizeExample shortens examples to avoid leaking sensitive data
func sanitizeExample(value string) string {
	if len(value) > 10 {
		return value[:5] + "..."
	}
	return "..."
}

// addAuthScheme adds an authentication scheme to the detector
func (d *AuthDetector) addAuthScheme(scheme AuthScheme) {
	key := scheme.Type + "-" + scheme.Scheme
	if scheme.Type == "apiKey" {
		key = scheme.Type + "-" + scheme.In + "-" + scheme.Name
	}

	d.detectedSchemes[key] = scheme
}

// GetAuthSchemes returns all detected authentication schemes
func (d *AuthDetector) GetAuthSchemes() []AuthScheme {
	var schemes []AuthScheme
	for _, scheme := range d.detectedSchemes {
		schemes = append(schemes, scheme)
	}
	return schemes
}

// contains checks if a string is in a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
