package parser

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAPIDefinition(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		version     string
		basePath    string
	}{
		{
			name:        "empty values",
			title:       "",
			description: "",
			version:     "",
			basePath:    "",
		},
		{
			name:        "with values",
			title:       "Test API",
			description: "Test Description",
			version:     "1.0.0",
			basePath:    "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := NewAPIDefinition(tt.title, tt.description, tt.version, tt.basePath)
			assert.NotNil(t, def)
			assert.Equal(t, tt.title, def.Title)
			assert.Equal(t, tt.description, def.Description)
			assert.Equal(t, tt.version, def.Version)
			assert.Equal(t, tt.basePath, def.BasePath)
			assert.NotNil(t, def.Paths)
			assert.NotNil(t, def.Components.Schemas)
		})
	}
}

func TestGenerateOperationID(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			method:   "get",
			path:     "/users",
			expected: "getusers",
		},
		{
			name:     "path with parameters",
			method:   "get",
			path:     "/users/{id}",
			expected: "getusersId",
		},
		{
			name:     "nested path",
			method:   "post",
			path:     "/api/v1/users/create",
			expected: "postapiV1UsersCreate",
		},
		{
			name:     "path with special characters",
			method:   "put",
			path:     "/users/!@#$%^&*()",
			expected: "putusers",
		},
		{
			name:     "empty path",
			method:   "delete",
			path:     "/",
			expected: "delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateOperationID(tt.method, tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateTags(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "simple path",
			path:     "/users",
			expected: []string{"users"},
		},
		{
			name:     "nested path",
			path:     "/api/v1/users",
			expected: []string{"api"},
		},
		{
			name:     "root path",
			path:     "/",
			expected: []string{"default"},
		},
		{
			name:     "empty path",
			path:     "",
			expected: []string{"default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateTags(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseParameters(t *testing.T) {
	tests := []struct {
		name           string
		request        proxy.RequestData
		expectedCount  int
		validateParams func(t *testing.T, params []Parameter)
	}{
		{
			name: "path parameters",
			request: proxy.RequestData{
				Path: "/users/{id}/posts/{postId}",
			},
			expectedCount: 2,
			validateParams: func(t *testing.T, params []Parameter) {
				assert.Equal(t, "id", params[0].Name)
				assert.Equal(t, "path", params[0].In)
				assert.True(t, params[0].Required)
				assert.Equal(t, "postId", params[1].Name)
				assert.Equal(t, "path", params[1].In)
				assert.True(t, params[1].Required)
			},
		},
		{
			name: "query parameters",
			request: proxy.RequestData{
				QueryParams: map[string][]string{
					"page":  {"1"},
					"limit": {"10"},
				},
			},
			expectedCount: 2,
			validateParams: func(t *testing.T, params []Parameter) {
				for _, p := range params {
					assert.Equal(t, "query", p.In)
					assert.False(t, p.Required)
					assert.Contains(t, []string{"page", "limit"}, p.Name)
				}
			},
		},
		{
			name: "mixed parameters",
			request: proxy.RequestData{
				Path: "/users/{id}",
				QueryParams: map[string][]string{
					"fields": {"name,email"},
				},
			},
			expectedCount: 2,
			validateParams: func(t *testing.T, params []Parameter) {
				var pathParam, queryParam *Parameter
				for _, p := range params {
					if p.In == "path" {
						pathParam = &p
					} else if p.In == "query" {
						queryParam = &p
					}
				}
				assert.NotNil(t, pathParam)
				assert.NotNil(t, queryParam)
				assert.Equal(t, "id", pathParam.Name)
				assert.Equal(t, "fields", queryParam.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := parseParameters(tt.request)
			assert.Len(t, params, tt.expectedCount)
			if tt.validateParams != nil {
				tt.validateParams(t, params)
			}
		})
	}
}

func TestInferSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected Schema
	}{
		{
			name:  "null value",
			input: nil,
			expected: Schema{
				Type:     "null",
				Nullable: true,
			},
		},
		{
			name:  "boolean",
			input: true,
			expected: Schema{
				Type:    "boolean",
				Example: true,
			},
		},
		{
			name:  "integer",
			input: float64(42),
			expected: Schema{
				Type:    "integer",
				Format:  "int64",
				Example: 42,
			},
		},
		{
			name:  "float",
			input: 42.5,
			expected: Schema{
				Type:    "number",
				Format:  "double",
				Example: 42.5,
			},
		},
		{
			name:  "string",
			input: "test",
			expected: Schema{
				Type:    "string",
				Example: "test",
			},
		},
		{
			name:  "date-time string",
			input: "2023-01-01T12:00:00Z",
			expected: Schema{
				Type:    "string",
				Format:  "date-time",
				Example: "2023-01-01T12:00:00Z",
			},
		},
		{
			name:  "array",
			input: []interface{}{"one", "two"},
			expected: Schema{
				Type: "array",
				Items: &Schema{
					Type:    "string",
					Example: "one",
				},
			},
		},
		{
			name:  "empty array",
			input: []interface{}{},
			expected: Schema{
				Type: "array",
				Items: &Schema{
					Type: "string",
				},
			},
		},
		{
			name: "object",
			input: map[string]interface{}{
				"name": "John",
				"age":  float64(30),
			},
			expected: Schema{
				Type: "object",
				Properties: map[string]Schema{
					"name": {
						Type:    "string",
						Example: "John",
					},
					"age": {
						Type:    "integer",
						Format:  "int64",
						Example: 30,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferSchema(tt.input)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.Format, result.Format)
			assert.Equal(t, tt.expected.Example, result.Example)
			assert.Equal(t, tt.expected.Nullable, result.Nullable)

			if tt.expected.Items != nil {
				assert.NotNil(t, result.Items)
				assert.Equal(t, tt.expected.Items.Type, result.Items.Type)
			}

			if tt.expected.Properties != nil {
				assert.Equal(t, tt.expected.Properties, result.Properties)
			}
		})
	}
}

func TestGenerateOpenAPISpec(t *testing.T) {
	tests := []struct {
		name         string
		transactions []proxy.APITransaction
		validate     func(*testing.T, []byte)
	}{
		{
			name:         "empty transactions",
			transactions: []proxy.APITransaction{},
			validate: func(t *testing.T, data []byte) {
				var spec map[string]interface{}
				err := json.Unmarshal(data, &spec)
				require.NoError(t, err)
				assert.Equal(t, OpenAPIVersion, spec["openapi"])
				assert.NotNil(t, spec["paths"])
				assert.NotNil(t, spec["components"])
			},
		},
		{
			name: "basic CRUD endpoints",
			transactions: []proxy.APITransaction{
				{
					Request: proxy.RequestData{
						Method: "GET",
						Path:   "/users",
					},
					Response: proxy.ResponseData{
						StatusCode: 200,
						Headers: http.Header{
							"Content-Type": []string{"application/json"},
						},
						Body: []byte(`[{"id":1,"name":"John"}]`),
					},
				},
				{
					Request: proxy.RequestData{
						Method: "POST",
						Path:   "/users",
						Headers: http.Header{
							"Content-Type": []string{"application/json"},
						},
						Body: []byte(`{"name":"Jane"}`),
					},
					Response: proxy.ResponseData{
						StatusCode: 201,
						Headers: http.Header{
							"Content-Type": []string{"application/json"},
						},
						Body: []byte(`{"id":2,"name":"Jane"}`),
					},
				},
			},
			validate: func(t *testing.T, data []byte) {
				var spec map[string]interface{}
				err := json.Unmarshal(data, &spec)
				require.NoError(t, err)

				paths, ok := spec["paths"].(map[string]interface{})
				require.True(t, ok)

				userPath, ok := paths["/users"].(map[string]interface{})
				require.True(t, ok)

				assert.NotNil(t, userPath["get"])
				assert.NotNil(t, userPath["post"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := GenerateOpenAPISpec(
				tt.transactions,
				"Test API",
				"Test Description",
				"1.0.0",
				"http://localhost:8080",
			)
			require.NoError(t, err)
			tt.validate(t, spec)
		})
	}
}

func TestIsDateTime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "RFC3339",
			input:    "2023-01-01T12:00:00Z",
			expected: true,
		},
		{
			name:     "ISO8601",
			input:    "2023-01-01T12:00:00",
			expected: true,
		},
		{
			name:     "date only",
			input:    "2023-01-01",
			expected: true,
		},
		{
			name:     "invalid date",
			input:    "not a date",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDateTime(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPathParameters(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "no parameters",
			path:     "/users",
			expected: nil,
		},
		{
			name:     "single parameter",
			path:     "/users/{id}",
			expected: []string{"id"},
		},
		{
			name:     "multiple parameters",
			path:     "/users/{userId}/posts/{postId}",
			expected: []string{"userId", "postId"},
		},
		{
			name:     "nested parameters",
			path:     "/api/{version}/users/{id}/posts/{postId}",
			expected: []string{"version", "id", "postId"},
		},
		{
			name:     "empty path",
			path:     "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPathParameters(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
