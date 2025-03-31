package openapi

import (
	"net/http"
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/parser"
	"github.com/parnexcodes/swag-doc/pkg/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAPIGenerator(t *testing.T) {
	tests := []struct {
		name   string
		config OpenAPIConfig
	}{
		{
			name: "empty config",
			config: OpenAPIConfig{
				TagMappings:     make(map[string]string),
				VersionPrefixes: make(map[string]bool),
			},
		},
		{
			name: "full config",
			config: OpenAPIConfig{
				Title:       "Test API",
				Description: "Test Description",
				Version:     "1.0.0",
				Servers: []OpenAPIServer{
					{URL: "http://localhost:8080", Description: "Local Server"},
				},
				TagMappings: map[string]string{
					"users": "User Management",
				},
				UsePathGroups: true,
				VersionPrefixes: map[string]bool{
					"v1": true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewOpenAPIGenerator(tt.config)
			assert.NotNil(t, generator)
			assert.Equal(t, tt.config, generator.config)
			assert.NotNil(t, generator.transactions)
			assert.NotNil(t, generator.schemas)
		})
	}
}

func TestAddTransaction(t *testing.T) {
	generator := NewOpenAPIGenerator(OpenAPIConfig{})
	tx := proxy.APITransaction{
		Request: proxy.RequestData{
			Method: "GET",
			Path:   "/users",
		},
		Response: proxy.ResponseData{
			StatusCode: 200,
		},
	}

	generator.AddTransaction(tx)
	assert.Len(t, generator.transactions, 1)
	assert.Equal(t, tx, generator.transactions[0])
}

func TestGenerateSpec(t *testing.T) {
	tests := []struct {
		name         string
		config       OpenAPIConfig
		transactions []proxy.APITransaction
		validate     func(*testing.T, *OpenAPISpec)
	}{
		{
			name: "empty spec",
			config: OpenAPIConfig{
				Title:           "Test API",
				Description:     "Test Description",
				Version:         "1.0.0",
				TagMappings:     make(map[string]string),
				VersionPrefixes: make(map[string]bool),
			},
			transactions: []proxy.APITransaction{},
			validate: func(t *testing.T, spec *OpenAPISpec) {
				assert.Equal(t, "3.0.3", spec.OpenAPI)
				assert.Equal(t, "Test API", spec.Info.Title)
				assert.Equal(t, "Test Description", spec.Info.Description)
				assert.Equal(t, "1.0.0", spec.Info.Version)
				assert.Equal(t, 0, len(spec.Paths.Map()))
			},
		},
		{
			name: "basic CRUD endpoints",
			config: OpenAPIConfig{
				Title:           "Test API",
				Description:     "Test Description",
				Version:         "1.0.0",
				TagMappings:     make(map[string]string),
				VersionPrefixes: make(map[string]bool),
			},
			transactions: []proxy.APITransaction{
				{
					Request: proxy.RequestData{
						Method: "GET",
						Path:   "/users",
						Headers: http.Header{
							"Accept": []string{"application/json"},
						},
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
			validate: func(t *testing.T, spec *OpenAPISpec) {
				assert.NotNil(t, spec.Paths)
				path := spec.Paths.Find("/users")
				require.NotNil(t, path)

				// Validate GET endpoint
				assert.NotNil(t, path.Get)
				assert.Contains(t, path.Get.Responses.Map(), "200")

				// Validate POST endpoint
				assert.NotNil(t, path.Post)
				assert.Contains(t, path.Post.Responses.Map(), "201")
				assert.NotNil(t, path.Post.RequestBody)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewOpenAPIGenerator(tt.config)
			for _, tx := range tt.transactions {
				generator.AddTransaction(tx)
			}

			spec, err := generator.GenerateSpec()
			require.NoError(t, err)
			require.NotNil(t, spec)
			tt.validate(t, spec)
		})
	}
}

func TestParseJSONBody(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected *parser.Schema
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:  "simple string",
			input: "test",
			expected: &parser.Schema{
				Type:    "string",
				Example: "test",
			},
		},
		{
			name:  "integer",
			input: float64(42),
			expected: &parser.Schema{
				Type:    "integer",
				Format:  "int64",
				Example: int64(42),
			},
		},
		{
			name:  "boolean",
			input: true,
			expected: &parser.Schema{
				Type:    "boolean",
				Example: true,
			},
		},
		{
			name: "object",
			input: map[string]interface{}{
				"name": "John",
				"age":  float64(30),
			},
			expected: &parser.Schema{
				Type: "object",
				Properties: map[string]parser.Schema{
					"name": {
						Type:    "string",
						Example: "John",
					},
					"age": {
						Type:    "integer",
						Format:  "int64",
						Example: int64(30),
					},
				},
				Required: []string{"name", "age"},
				Example: map[string]interface{}{
					"name": "John",
					"age":  float64(30),
				},
			},
		},
		{
			name:  "array",
			input: []interface{}{"one", "two"},
			expected: &parser.Schema{
				Type: "array",
				Items: &parser.Schema{
					Type:    "string",
					Example: "one",
				},
				Example: []interface{}{"one", "two"},
			},
		},
	}

	generator := NewOpenAPIGenerator(OpenAPIConfig{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := generator.parseJSONBody(tt.input)
			if tt.expected == nil {
				assert.Nil(t, schema)
				assert.NoError(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, schema.Type)
			assert.Equal(t, tt.expected.Format, schema.Format)
			assert.Equal(t, tt.expected.Example, schema.Example)

			if tt.expected.Items != nil {
				assert.NotNil(t, schema.Items)
				assert.Equal(t, tt.expected.Items.Type, schema.Items.Type)
			}

			if tt.expected.Properties != nil {
				assert.Equal(t, tt.expected.Properties, schema.Properties)
				assert.ElementsMatch(t, tt.expected.Required, schema.Required)
			}
		})
	}
}

func TestExtractTagFromPath(t *testing.T) {
	tests := []struct {
		name     string
		config   OpenAPIConfig
		path     string
		expected string
	}{
		{
			name:     "simple path",
			config:   OpenAPIConfig{},
			path:     "/users",
			expected: "Users",
		},
		{
			name: "versioned path",
			config: OpenAPIConfig{
				VersionPrefixes: map[string]bool{"v1": true},
			},
			path:     "/v1/users",
			expected: "Users",
		},
		{
			name: "custom tag mapping",
			config: OpenAPIConfig{
				TagMappings: map[string]string{
					"users": "User Management",
				},
			},
			path:     "/users",
			expected: "User Management",
		},
		{
			name:     "empty path",
			config:   OpenAPIConfig{},
			path:     "/",
			expected: "default",
		},
		{
			name:     "nested path",
			config:   OpenAPIConfig{},
			path:     "/users/posts/comments",
			expected: "Users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewOpenAPIGenerator(tt.config)
			tag := generator.extractTagFromPath(tt.path)
			assert.Equal(t, tt.expected, tag)
		})
	}
}

func TestIsVersionPrefix(t *testing.T) {
	tests := []struct {
		name     string
		config   OpenAPIConfig
		segment  string
		expected bool
	}{
		{
			name:     "default version prefix",
			config:   OpenAPIConfig{},
			segment:  "v1",
			expected: true,
		},
		{
			name: "custom version prefix",
			config: OpenAPIConfig{
				VersionPrefixes: map[string]bool{
					"api": true,
				},
			},
			segment:  "api",
			expected: true,
		},
		{
			name:     "non-version prefix",
			config:   OpenAPIConfig{},
			segment:  "users",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewOpenAPIGenerator(tt.config)
			result := generator.isVersionPrefix(tt.segment)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetContentType(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected string
	}{
		{
			name: "simple content type",
			headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
			expected: "application/json",
		},
		{
			name: "content type with charset",
			headers: http.Header{
				"Content-Type": []string{"application/json; charset=utf-8"},
			},
			expected: "application/json",
		},
		{
			name:     "no content type",
			headers:  http.Header{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getContentType(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function to create a test transaction
func createTestTransaction(method, path string, reqBody, respBody []byte, statusCode int) proxy.APITransaction {
	return proxy.APITransaction{
		Request: proxy.RequestData{
			Method: method,
			Path:   path,
			Headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: reqBody,
		},
		Response: proxy.ResponseData{
			StatusCode: statusCode,
			Headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: respBody,
		},
	}
}
