package openapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"swag-doc/pkg/parser"
	"swag-doc/pkg/proxy"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPISpec is an alias for openapi3.T to make it clearer
type OpenAPISpec = openapi3.T

// OpenAPIGenerator generates OpenAPI specs from API transactions
type OpenAPIGenerator struct {
	config       OpenAPIConfig
	transactions []proxy.APITransaction
	schemas      map[string]*openapi3.Schema
}

// OpenAPIConfig holds configuration for the generator
type OpenAPIConfig struct {
	Title       string
	Description string
	Version     string
	Servers     []OpenAPIServer
}

// OpenAPIServer represents an API server in the OpenAPI spec
type OpenAPIServer struct {
	URL         string
	Description string
}

// NewOpenAPIGenerator creates a new OpenAPI generator
func NewOpenAPIGenerator(config OpenAPIConfig) *OpenAPIGenerator {
	return &OpenAPIGenerator{
		config:       config,
		transactions: []proxy.APITransaction{},
		schemas:      make(map[string]*openapi3.Schema),
	}
}

// AddTransaction adds an API transaction to be analyzed
func (g *OpenAPIGenerator) AddTransaction(tx proxy.APITransaction) {
	g.transactions = append(g.transactions, tx)
}

// LoadTransactionsFromFile loads API transactions from a file
func (g *OpenAPIGenerator) LoadTransactionsFromFile(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	var transactions []proxy.APITransaction
	if err := json.Unmarshal(data, &transactions); err != nil {
		return err
	}

	g.transactions = append(g.transactions, transactions...)
	return nil
}

// LoadTransactionsFromDirectory loads API transactions from all JSON files in a directory
func (g *OpenAPIGenerator) LoadTransactionsFromDirectory(dirPath string) error {
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		err := g.LoadTransactionsFromFile(filepath.Join(dirPath, file.Name()))
		if err != nil {
			return err
		}
	}

	return nil
}

// GenerateSpec generates an OpenAPI spec from the collected transactions
func (g *OpenAPIGenerator) GenerateSpec() (*OpenAPISpec, error) {
	return g.generateAPI()
}

// Helper function to decode Base64 response body if needed
func maybeDecodeBase64(data []byte) ([]byte, error) {
	// Check if data is base64 encoded
	if len(data) > 0 && data[0] == 'W' {
		// Try to decode base64
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err == nil {
			// If decoding worked and result looks like JSON, return it
			if len(decoded) > 0 && (decoded[0] == '{' || decoded[0] == '[') {
				return decoded, nil
			}
		}
	}
	// Return original data if not base64 or decode failed
	return data, nil
}

// generateAPI generates an OpenAPI document from the transactions
func (g *OpenAPIGenerator) generateAPI() (*OpenAPISpec, error) {
	// Create a new OpenAPI document
	doc := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       g.config.Title,
			Description: g.config.Description,
			Version:     g.config.Version,
		},
		Paths: openapi3.NewPaths(),
	}

	// Add servers if configured
	for _, server := range g.config.Servers {
		doc.Servers = append(doc.Servers, &openapi3.Server{
			URL:         server.URL,
			Description: server.Description,
		})
	}

	// Create parser components
	pathDetector := parser.NewPathPatternDetector()
	authDetector := parser.NewAuthDetector()
	schemaMerger := parser.NewSchemaMerger()
	typeInferrer := parser.NewTypeInferrer(10) // Collect up to 10 samples per field

	// Status code descriptions for responses
	statusCodeDescriptions := map[string]string{
		"200": "OK",
		"201": "Created",
		"202": "Accepted",
		"204": "No Content",
		"400": "Bad Request",
		"401": "Unauthorized",
		"403": "Forbidden",
		"404": "Not Found",
		"500": "Internal Server Error",
	}

	// First pass: analyze paths and auth
	for _, tx := range g.transactions {
		// Add path for pattern detection
		pathDetector.AddPath(tx.Request.Path)

		// Create http.Request with headers for auth detection
		reqHeader := make(http.Header)
		for k, v := range tx.Request.Headers {
			if len(v) > 0 {
				reqHeader.Set(k, v[0])
			}
		}

		// Create URL for request
		reqURL, _ := url.Parse(tx.Request.Path)
		if tx.Request.QueryParams != nil {
			reqURL.RawQuery = tx.Request.QueryParams.Encode()
		}

		authReq := &http.Request{
			Header: reqHeader,
			URL:    reqURL,
		}

		// Create http.Response with headers for auth detection
		respHeader := make(http.Header)
		for k, v := range tx.Response.Headers {
			if len(v) > 0 {
				respHeader.Set(k, v[0])
			}
		}

		authResp := &http.Response{
			Header: respHeader,
		}

		authDetector.AnalyzeTransaction(authReq, authResp)

		// Collect samples for type inference
		if tx.Request.Body != nil {
			bodyObj := make(map[string]interface{})
			if err := json.Unmarshal(tx.Request.Body, &bodyObj); err == nil {
				typeInferrer.AddSample("request:"+tx.Request.Method+":"+tx.Request.Path, bodyObj)
			}
		}

		if tx.Response.Body != nil {
			// Decode the response body from base64 if needed
			decodedBody, err := maybeDecodeBase64(tx.Response.Body)
			if err == nil {
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(decodedBody, &bodyObj); err == nil {
					typeInferrer.AddSample("response:"+tx.Request.Method+":"+tx.Request.Path, bodyObj)
				} else {
					// Try as array
					var bodyArr []interface{}
					if err := json.Unmarshal(decodedBody, &bodyArr); err == nil {
						if len(bodyArr) > 0 {
							// Use the first item as a sample if it's an array
							typeInferrer.AddSample("response:"+tx.Request.Method+":"+tx.Request.Path, bodyArr[0])
						}
					}
				}
			}
		}
	}

	// Analyze path patterns
	pathDetector.AnalyzePatterns()

	// Second pass: generate paths and schemas
	endpoints := make(map[string]map[string]bool) // path -> method -> bool

	for _, tx := range g.transactions {
		// Get the templated path
		templatedPath := pathDetector.TemplatizePath(tx.Request.Path)
		if templatedPath == "" {
			templatedPath = tx.Request.Path
		}

		// Check if we've already processed this endpoint
		if _, exists := endpoints[templatedPath]; !exists {
			endpoints[templatedPath] = make(map[string]bool)
		}

		// Skip if we've already processed this method
		if endpoints[templatedPath][tx.Request.Method] {
			// Add to schema merger for later merging
			if tx.Request.Body != nil {
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(tx.Request.Body, &bodyObj); err == nil {
					if requestSchema, err := g.parseJSONBody(bodyObj); err == nil && requestSchema != nil {
						schemaMerger.AddSchema(templatedPath, tx.Request.Method, *requestSchema)
					}
				}
			}

			if tx.Response.Body != nil {
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(tx.Response.Body, &bodyObj); err == nil {
					if responseSchema, err := g.parseJSONBody(bodyObj); err == nil && responseSchema != nil {
						schemaMerger.AddSchema(templatedPath+":response", tx.Request.Method, *responseSchema)
					}
				}
			}

			continue
		}

		// Mark as processed
		endpoints[templatedPath][tx.Request.Method] = true

		// Get or create the path item
		pathItem := doc.Paths.Find(templatedPath)
		if pathItem == nil {
			pathItem = &openapi3.PathItem{}
			doc.Paths.Set(templatedPath, pathItem)
		}

		// Create the operation
		op := &openapi3.Operation{
			Responses: openapi3.NewResponses(),
			Tags:      []string{},
		}

		// Extract path parameters
		pathParams := parser.GetPathParameters(tx.Request.Path, templatedPath)
		for name, value := range pathParams {
			var schema *openapi3.Schema

			// Detect parameter type
			if _, err := strconv.Atoi(value); err == nil {
				schema = openapi3.NewInt64Schema()
			} else {
				// Check if it's a UUID-like string - simplified check
				if len(value) == 36 && strings.Count(value, "-") == 4 {
					schema = openapi3.NewUUIDSchema()
				} else {
					schema = openapi3.NewStringSchema()
				}
			}

			op.Parameters = append(op.Parameters, &openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					Name:        name,
					In:          "path",
					Required:    true,
					Description: fmt.Sprintf("Path parameter: %s", name),
					Schema: &openapi3.SchemaRef{
						Value: schema,
					},
				},
			})
		}

		// Add query parameters if available
		if tx.Request.QueryParams != nil {
			for name, values := range tx.Request.QueryParams {
				var example interface{}
				if len(values) > 0 {
					example = values[0]
				}

				op.Parameters = append(op.Parameters, &openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        name,
						In:          "query",
						Required:    false,
						Description: "",
						Schema: &openapi3.SchemaRef{
							Value: openapi3.NewStringSchema(),
						},
						Example: example,
					},
				})
			}
		}

		// Add headers (excluding common headers)
		for name, values := range tx.Request.Headers {
			// Skip common headers and auth headers (handled separately)
			if isCommonHeader(name) || isAuthHeader(name) {
				continue
			}

			var example interface{}
			if len(values) > 0 {
				example = values[0]
			}

			op.Parameters = append(op.Parameters, &openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					Name:        name,
					In:          "header",
					Required:    false,
					Description: "",
					Schema: &openapi3.SchemaRef{
						Value: openapi3.NewStringSchema(),
					},
					Example: example,
				},
			})
		}

		// Parse request body
		var requestSchema *parser.Schema
		if tx.Request.Body != nil {
			bodyObj := make(map[string]interface{})
			if err := json.Unmarshal(tx.Request.Body, &bodyObj); err == nil {
				requestSchema, _ = g.parseJSONBody(bodyObj)
			}
		}

		if requestSchema != nil {
			contentType := getContentType(tx.Request.Headers)
			if contentType == "" {
				contentType = "application/json"
			}

			// Apply type inference to improve schema quality
			samples := []interface{}{}
			if tx.Request.Body != nil {
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(tx.Request.Body, &bodyObj); err == nil {
					samples = append(samples, bodyObj)
				}
			}

			*requestSchema = parser.ApplyTypeInference(*requestSchema, samples, 10)

			// Add to schema merger for future refinement
			schemaMerger.AddSchema(templatedPath, tx.Request.Method, *requestSchema)

			op.RequestBody = &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						contentType: &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: toOpenAPISchema(*requestSchema),
							},
						},
					},
				},
			}
		}

		// Parse response
		var responseSchema *parser.Schema
		if tx.Response.Body != nil {
			// Decode the response body from base64 if needed
			decodedBody, err := maybeDecodeBase64(tx.Response.Body)
			if err == nil {
				// Try as object first
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(decodedBody, &bodyObj); err == nil {
					responseSchema, _ = g.parseJSONBody(bodyObj)
				} else {
					// Try as array
					var bodyArr []interface{}
					if err := json.Unmarshal(decodedBody, &bodyArr); err == nil {
						if len(bodyArr) > 0 {
							// Create array schema with the first item as a sample
							itemSchema, _ := g.parseJSONBody(bodyArr[0])
							if itemSchema != nil {
								responseSchema = &parser.Schema{
									Type:  "array",
									Items: itemSchema,
								}
							}
						}
					}
				}
			}
		}

		if responseSchema != nil {
			statusCode := fmt.Sprintf("%d", tx.Response.StatusCode)
			contentType := getContentType(tx.Response.Headers)
			if contentType == "" {
				contentType = "application/json"
			}

			// Apply type inference to improve schema quality
			samples := []interface{}{}
			if tx.Response.Body != nil {
				bodyObj := make(map[string]interface{})
				if err := json.Unmarshal(tx.Response.Body, &bodyObj); err == nil {
					samples = append(samples, bodyObj)
				}
			}

			*responseSchema = parser.ApplyTypeInference(*responseSchema, samples, 10)

			// Add to schema merger for future refinement
			schemaMerger.AddSchema(templatedPath+":response", tx.Request.Method, *responseSchema)

			description := "Response"
			if desc, ok := statusCodeDescriptions[statusCode]; ok {
				description = desc
			}

			op.Responses.Set(statusCode, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &description,
					Content: openapi3.Content{
						contentType: &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: toOpenAPISchema(*responseSchema),
							},
						},
					},
				},
			})
		} else {
			// Add a default response
			statusCode := fmt.Sprintf("%d", tx.Response.StatusCode)
			description := "Response"
			if desc, ok := statusCodeDescriptions[statusCode]; ok {
				description = desc
			}

			op.Responses.Set(statusCode, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &description,
				},
			})
		}

		// Assign the operation to the path item based on the method
		switch tx.Request.Method {
		case "GET":
			pathItem.Get = op
		case "POST":
			pathItem.Post = op
		case "PUT":
			pathItem.Put = op
		case "DELETE":
			pathItem.Delete = op
		case "PATCH":
			pathItem.Patch = op
		case "HEAD":
			pathItem.Head = op
		case "OPTIONS":
			pathItem.Options = op
		}
	}

	// Apply schema merging to improve schema quality
	for path, methods := range endpoints {
		for method := range methods {
			// Merge request schemas
			mergedSchema := schemaMerger.MergeSchemas(path, method)

			// Update request body schema if it exists
			pathItem := doc.Paths.Find(path)
			if pathItem != nil {
				var op *openapi3.Operation
				switch method {
				case "GET":
					op = pathItem.Get
				case "POST":
					op = pathItem.Post
				case "PUT":
					op = pathItem.Put
				case "DELETE":
					op = pathItem.Delete
				case "PATCH":
					op = pathItem.Patch
				case "HEAD":
					op = pathItem.Head
				case "OPTIONS":
					op = pathItem.Options
				}

				if op != nil && op.RequestBody != nil && op.RequestBody.Value != nil {
					for mediaType, content := range op.RequestBody.Value.Content {
						if content.Schema != nil && content.Schema.Value != nil {
							// Update with merged schema
							content.Schema.Value = toOpenAPISchema(mergedSchema)
							op.RequestBody.Value.Content[mediaType] = content
						}
					}
				}

				// Merge response schemas
				responseSchema := schemaMerger.MergeSchemas(path+":response", method)

				if op != nil && op.Responses != nil {
					responseMap := op.Responses.Map()
					for _, response := range responseMap {
						if response != nil && response.Value != nil {
							for mediaType, content := range response.Value.Content {
								if content.Schema != nil && content.Schema.Value != nil {
									// Update with merged schema
									content.Schema.Value = toOpenAPISchema(responseSchema)
									response.Value.Content[mediaType] = content
								}
							}
						}
					}
				}
			}
		}
	}

	// Add security schemes
	doc.Components = &openapi3.Components{
		SecuritySchemes: openapi3.SecuritySchemes{},
	}

	// Apply auth schemes
	authSchemes := authDetector.GetAuthSchemes()
	for _, scheme := range authSchemes {
		securityScheme := &openapi3.SecurityScheme{
			Description: scheme.Description,
		}

		switch scheme.Type {
		case "http":
			securityScheme.Type = "http"
			securityScheme.Scheme = scheme.Scheme
			if scheme.Format != "" {
				securityScheme.BearerFormat = scheme.Format
			}
		case "apiKey":
			securityScheme.Type = "apiKey"
			securityScheme.In = scheme.In
			securityScheme.Name = scheme.Name
		}

		// Add to document
		key := scheme.Type
		if scheme.Type == "http" {
			key = scheme.Scheme
		} else if scheme.Type == "apiKey" {
			key = "apiKey_" + scheme.Name
		}

		doc.Components.SecuritySchemes[key] = &openapi3.SecuritySchemeRef{
			Value: securityScheme,
		}
	}

	return doc, nil
}

// parseJSONBody parses a JSON object to extract schema information
func (g *OpenAPIGenerator) parseJSONBody(body interface{}) (*parser.Schema, error) {
	if body == nil {
		return nil, nil
	}

	// Convert sanitized placeholders back to proper types when generating schemas
	processedBody := convertPlaceholders(body)

	// Parse schema
	var schema parser.Schema
	switch v := processedBody.(type) {
	case map[string]interface{}:
		schema = parser.Schema{
			Type:       "object",
			Properties: make(map[string]parser.Schema),
		}

		for key, val := range v {
			if propSchema, err := g.parseJSONBody(val); err == nil && propSchema != nil {
				schema.Properties[key] = *propSchema
				schema.Required = append(schema.Required, key)
			}
		}
	case []interface{}:
		schema = parser.Schema{
			Type: "array",
		}

		if len(v) > 0 {
			if itemSchema, err := g.parseJSONBody(v[0]); err == nil && itemSchema != nil {
				schema.Items = itemSchema
			}
		}
	case string:
		schema = parser.Schema{
			Type: "string",
		}
	case float64:
		if v == float64(int(v)) {
			schema = parser.Schema{
				Type:   "integer",
				Format: "int64",
			}
		} else {
			schema = parser.Schema{
				Type:   "number",
				Format: "double",
			}
		}
	case bool:
		schema = parser.Schema{
			Type: "boolean",
		}
	default:
		return nil, fmt.Errorf("unsupported type: %T", processedBody)
	}

	return &schema, nil
}

// convertPlaceholders converts placeholder values like "__string__" back to appropriate sample values
func convertPlaceholders(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = convertPlaceholders(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = convertPlaceholders(val)
		}
		return result
	case string:
		switch v {
		case "__string__":
			return "string"
		case "__integer__":
			return int64(0)
		case "__number__":
			return 0.0
		case "__boolean__":
			return false
		case "__unknown__":
			return "unknown"
		case "__redacted__":
			return "redacted"
		default:
			return v
		}
	default:
		return v
	}
}

// Helper function to check if a header is a common HTTP header
func isCommonHeader(name string) bool {
	commonHeaders := map[string]bool{
		"Accept":            true,
		"Accept-Charset":    true,
		"Accept-Encoding":   true,
		"Accept-Language":   true,
		"Cache-Control":     true,
		"Connection":        true,
		"Content-Length":    true,
		"Content-Type":      true,
		"Cookie":            true,
		"Date":              true,
		"Host":              true,
		"Origin":            true,
		"Referer":           true,
		"User-Agent":        true,
		"X-Forwarded-For":   true,
		"X-Forwarded-Proto": true,
	}

	return commonHeaders[name]
}

// Helper function to check if a header is an auth header
func isAuthHeader(name string) bool {
	authHeaders := map[string]bool{
		"Authorization": true,
		"X-API-Key":     true,
		"X-Auth-Token":  true,
		"X-Auth":        true,
		"Api-Key":       true,
		"Token":         true,
		"Bearer":        true,
		"JWT":           true,
	}

	return authHeaders[name]
}

// Helper function to convert a parser.Schema to an openapi3.Schema
func toOpenAPISchema(schema parser.Schema) *openapi3.Schema {
	var result *openapi3.Schema

	switch schema.Type {
	case "string":
		result = openapi3.NewStringSchema()
		result.Format = schema.Format
	case "integer":
		if schema.Format == "int64" {
			result = openapi3.NewInt64Schema()
		} else {
			result = openapi3.NewInt32Schema()
		}
	case "number":
		result = openapi3.NewFloat64Schema()
	case "boolean":
		result = openapi3.NewBoolSchema()
	case "array":
		result = openapi3.NewArraySchema()
	case "object":
		result = openapi3.NewObjectSchema()
	default:
		// Default to generic schema
		result = openapi3.NewSchema()
	}

	result.Example = schema.Example
	result.Nullable = schema.Nullable

	// Convert enum values
	if len(schema.Enum) > 0 {
		result.Enum = schema.Enum
	}

	// Convert properties for objects
	if schema.Type == "object" && schema.Properties != nil {
		for name, propSchema := range schema.Properties {
			result.Properties[name] = &openapi3.SchemaRef{
				Value: toOpenAPISchema(propSchema),
			}
		}

		// Add required properties
		if len(schema.Required) > 0 {
			result.Required = schema.Required
		}
	}

	// Convert items for arrays
	if schema.Type == "array" && schema.Items != nil {
		result.Items = &openapi3.SchemaRef{
			Value: toOpenAPISchema(*schema.Items),
		}
	}

	return result
}

// Helper function to get content type from headers
func getContentType(headers http.Header) string {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		return ""
	}

	// Remove charset and other parameters
	if semicolon := strings.Index(contentType, ";"); semicolon != -1 {
		contentType = contentType[:semicolon]
	}

	return strings.TrimSpace(contentType)
}
