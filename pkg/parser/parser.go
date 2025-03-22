package parser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"swag-doc/pkg/proxy"
)

// OpenAPIVersion is the OpenAPI version used in generated specs
const OpenAPIVersion = "3.0.3"

// APIDefinition stores the collected API information
type APIDefinition struct {
	Paths       map[string]map[string]Operation // path -> method -> operation
	Components  Components
	BasePath    string
	Title       string
	Description string
	Version     string
}

// Components holds reusable OpenAPI components
type Components struct {
	Schemas map[string]Schema
}

// Schema represents an OpenAPI schema
type Schema struct {
	Type         string            `json:"type,omitempty"`
	Format       string            `json:"format,omitempty"`
	Properties   map[string]Schema `json:"properties,omitempty"`
	Items        *Schema           `json:"items,omitempty"`
	Required     []string          `json:"required,omitempty"`
	Enum         []interface{}     `json:"enum,omitempty"`
	Example      interface{}       `json:"example,omitempty"`
	AllOf        []Schema          `json:"allOf,omitempty"`
	OneOf        []Schema          `json:"oneOf,omitempty"`
	AnyOf        []Schema          `json:"anyOf,omitempty"`
	Not          *Schema           `json:"not,omitempty"`
	Default      interface{}       `json:"default,omitempty"`
	Nullable     bool              `json:"nullable,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
	WriteOnly    bool              `json:"writeOnly,omitempty"`
	XML          map[string]string `json:"xml,omitempty"`
	ExternalDocs map[string]string `json:"externalDocs,omitempty"`
	Deprecated   bool              `json:"deprecated,omitempty"`
}

// Operation represents an OpenAPI operation
type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Deprecated  bool                `json:"deprecated,omitempty"`
}

// Parameter represents an OpenAPI parameter
type Parameter struct {
	Name        string      `json:"name"`
	In          string      `json:"in"` // query, path, header, cookie
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Schema      Schema      `json:"schema"`
	Example     interface{} `json:"example,omitempty"`
}

// RequestBody represents an OpenAPI request body
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Content     map[string]MediaType `json:"content"`
	Required    bool                 `json:"required,omitempty"`
}

// Response represents an OpenAPI response
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
	Headers     map[string]Header    `json:"headers,omitempty"`
}

// MediaType represents an OpenAPI media type
type MediaType struct {
	Schema   Schema              `json:"schema"`
	Example  interface{}         `json:"example,omitempty"`
	Examples map[string]Example  `json:"examples,omitempty"`
	Encoding map[string]Encoding `json:"encoding,omitempty"`
}

// Example represents an OpenAPI example
type Example struct {
	Summary       string      `json:"summary,omitempty"`
	Description   string      `json:"description,omitempty"`
	Value         interface{} `json:"value,omitempty"`
	ExternalValue string      `json:"externalValue,omitempty"`
}

// Header represents an OpenAPI header
type Header struct {
	Description string      `json:"description,omitempty"`
	Schema      Schema      `json:"schema"`
	Example     interface{} `json:"example,omitempty"`
}

// Encoding represents an OpenAPI encoding
type Encoding struct {
	ContentType   string            `json:"contentType,omitempty"`
	Headers       map[string]Header `json:"headers,omitempty"`
	Style         string            `json:"style,omitempty"`
	Explode       bool              `json:"explode,omitempty"`
	AllowReserved bool              `json:"allowReserved,omitempty"`
}

// NewAPIDefinition creates a new APIDefinition with default values
func NewAPIDefinition(title, description, version, basePath string) *APIDefinition {
	return &APIDefinition{
		Paths: make(map[string]map[string]Operation),
		Components: Components{
			Schemas: make(map[string]Schema),
		},
		BasePath:    basePath,
		Title:       title,
		Description: description,
		Version:     version,
	}
}

// AddTransaction adds an API transaction to the definition
func (d *APIDefinition) AddTransaction(transaction proxy.APITransaction) {
	// Extract path and method
	path := transaction.Request.Path
	method := strings.ToLower(transaction.Request.Method)

	// Create path if it doesn't exist
	if _, ok := d.Paths[path]; !ok {
		d.Paths[path] = make(map[string]Operation)
	}

	// Create or update operation
	operation := d.Paths[path][method]
	if operation.OperationID == "" {
		operation.OperationID = generateOperationID(method, path)
	}

	// Set tags based on path segments
	operation.Tags = generateTags(path)

	// Parse request parameters
	operation.Parameters = parseParameters(transaction.Request)

	// Parse request body
	if hasRequestBody(method) && len(transaction.Request.Body) > 0 {
		contentType := getContentType(transaction.Request.Headers)
		if contentType != "" {
			requestSchema := parseJSONSchema(transaction.Request.Body)
			operation.RequestBody = &RequestBody{
				Content: map[string]MediaType{
					contentType: {
						Schema: requestSchema,
					},
				},
				Required: true,
			}
		}
	}

	// Parse response
	statusCode := fmt.Sprintf("%d", transaction.Response.StatusCode)
	contentType := getContentType(transaction.Response.Headers)

	var description string
	switch {
	case transaction.Response.StatusCode >= 200 && transaction.Response.StatusCode < 300:
		description = "Successful operation"
	case transaction.Response.StatusCode >= 400 && transaction.Response.StatusCode < 500:
		description = "Client error"
	case transaction.Response.StatusCode >= 500:
		description = "Server error"
	default:
		description = "Unknown"
	}

	if operation.Responses == nil {
		operation.Responses = make(map[string]Response)
	}

	response := Response{
		Description: description,
	}

	if contentType != "" && len(transaction.Response.Body) > 0 {
		responseSchema := parseJSONSchema(transaction.Response.Body)

		if response.Content == nil {
			response.Content = make(map[string]MediaType)
		}

		response.Content[contentType] = MediaType{
			Schema: responseSchema,
		}
	}

	operation.Responses[statusCode] = response

	// Update the operation
	d.Paths[path][method] = operation
}

// ToOpenAPI converts the API definition to an OpenAPI 3.0 specification
func (d *APIDefinition) ToOpenAPI() map[string]interface{} {
	// Sort paths for consistent output
	paths := make([]string, 0, len(d.Paths))
	for path := range d.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	pathsMap := make(map[string]interface{})
	for _, path := range paths {
		pathObj := make(map[string]interface{})
		for method, op := range d.Paths[path] {
			pathObj[method] = op
		}
		pathsMap[path] = pathObj
	}

	return map[string]interface{}{
		"openapi": OpenAPIVersion,
		"info": map[string]interface{}{
			"title":       d.Title,
			"description": d.Description,
			"version":     d.Version,
		},
		"servers": []map[string]interface{}{
			{
				"url": d.BasePath,
			},
		},
		"paths":      pathsMap,
		"components": d.Components,
	}
}

// Generate a valid operation ID from method and path
func generateOperationID(method, path string) string {
	// Remove leading and trailing slashes
	path = strings.Trim(path, "/")

	// Replace slashes with underscores
	path = strings.ReplaceAll(path, "/", "_")

	// Replace non-alphanumeric characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	path = re.ReplaceAllString(path, "_")

	// Convert to camelCase
	segments := strings.Split(path, "_")
	for i := 1; i < len(segments); i++ {
		if len(segments[i]) > 0 {
			segments[i] = strings.ToUpper(segments[i][:1]) + segments[i][1:]
		}
	}

	return method + strings.Join(segments, "")
}

// Generate tags based on the first path segment
func generateTags(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return []string{"default"}
	}
	return []string{parts[0]}
}

// Parse query and path parameters from a request
func parseParameters(req proxy.RequestData) []Parameter {
	var parameters []Parameter

	// Parse path parameters
	pathParams := extractPathParameters(req.Path)
	for _, name := range pathParams {
		parameters = append(parameters, Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema: Schema{
				Type: "string",
			},
		})
	}

	// Parse query parameters
	for name, values := range req.QueryParams {
		var example interface{}
		if len(values) > 0 {
			example = values[0]
		}

		parameters = append(parameters, Parameter{
			Name:     name,
			In:       "query",
			Required: false,
			Schema: Schema{
				Type:    "string",
				Example: example,
			},
		})
	}

	return parameters
}

// Extract path parameters using a simple regex pattern
func extractPathParameters(path string) []string {
	var params []string
	re := regexp.MustCompile(`{([^}]+)}`)
	matches := re.FindAllStringSubmatch(path, -1)

	for _, match := range matches {
		if len(match) > 1 {
			params = append(params, match[1])
		}
	}

	return params
}

// Check if a method can have a request body
func hasRequestBody(method string) bool {
	method = strings.ToUpper(method)
	return method == "POST" || method == "PUT" || method == "PATCH"
}

// Get content type from headers
func getContentType(headers http.Header) string {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		return ""
	}

	// Extract the main content type without parameters
	return strings.Split(contentType, ";")[0]
}

// Parse JSON to generate a schema
func parseJSONSchema(data []byte) Schema {
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		// If it's not valid JSON, return a string schema
		return Schema{
			Type: "string",
		}
	}

	return inferSchema(jsonData)
}

// Infer schema from a value
func inferSchema(value interface{}) Schema {
	if value == nil {
		return Schema{
			Type:     "null",
			Nullable: true,
		}
	}

	switch v := value.(type) {
	case bool:
		return Schema{
			Type:    "boolean",
			Example: v,
		}
	case float64:
		// Check if it's an integer
		if v == float64(int(v)) {
			return Schema{
				Type:    "integer",
				Format:  "int64",
				Example: int(v),
			}
		}
		return Schema{
			Type:    "number",
			Format:  "double",
			Example: v,
		}
	case string:
		// Try to determine if it's a date-time
		if isDateTime(v) {
			return Schema{
				Type:    "string",
				Format:  "date-time",
				Example: v,
			}
		}
		return Schema{
			Type:    "string",
			Example: v,
		}
	case []interface{}:
		if len(v) == 0 {
			return Schema{
				Type:  "array",
				Items: &Schema{Type: "string"}, // Default to string if empty
			}
		}

		// Infer schema from the first item
		itemSchema := inferSchema(v[0])
		return Schema{
			Type:  "array",
			Items: &itemSchema,
		}
	case map[string]interface{}:
		schema := Schema{
			Type:       "object",
			Properties: make(map[string]Schema),
		}

		for k, v := range v {
			schema.Properties[k] = inferSchema(v)
		}

		return schema
	default:
		// For unknown types, use string as fallback
		return Schema{
			Type:    "string",
			Example: fmt.Sprintf("%v", value),
		}
	}
}

// Check if a string is likely a date-time format
func isDateTime(s string) bool {
	// Try common date-time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006/01/02",
	}

	for _, format := range formats {
		if _, err := time.Parse(format, s); err == nil {
			return true
		}
	}

	return false
}

// Generate OpenAPI spec from API transactions
func GenerateOpenAPISpec(transactions []proxy.APITransaction, title, description, version, basePath string) ([]byte, error) {
	apiDef := NewAPIDefinition(title, description, version, basePath)

	for _, transaction := range transactions {
		apiDef.AddTransaction(transaction)
	}

	spec := apiDef.ToOpenAPI()
	return json.MarshalIndent(spec, "", "  ")
}
