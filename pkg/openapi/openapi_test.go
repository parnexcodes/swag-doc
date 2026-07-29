package openapi

import (
	"encoding/json"
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

func TestGenerateSpecEndToEnd(t *testing.T) {
	config := OpenAPIConfig{
		Title:           "E2E Test API",
		Description:     "End-to-end test",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}

	transactions := []proxy.APITransaction{
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
				Body: []byte(`[{"id":1,"name":"Alice","email":"alice@example.com"},{"id":2,"name":"Bob","email":"bob@example.com"}]`),
			},
		},
		{
			Request: proxy.RequestData{
				Method: "GET",
				Path:   "/users/1",
				Headers: http.Header{
					"Accept": []string{"application/json"},
				},
			},
			Response: proxy.ResponseData{
				StatusCode: 200,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"id":1,"name":"Alice","email":"alice@example.com"}`),
			},
		},
		{
			Request: proxy.RequestData{
				Method: "POST",
				Path:   "/users",
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"name":"Charlie","email":"charlie@example.com"}`),
			},
			Response: proxy.ResponseData{
				StatusCode: 201,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"id":3,"name":"Charlie","email":"charlie@example.com"}`),
			},
		},
	}

	generator := NewOpenAPIGenerator(config)
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Validate basic structure
	assert.Equal(t, "3.0.3", spec.OpenAPI)
	assert.Equal(t, "E2E Test API", spec.Info.Title)

	// Validate paths exist
	paths := spec.Paths.Map()
	assert.Contains(t, paths, "/users")
	assert.Contains(t, paths, "/users/{id}")

	// Validate methods
	usersPath := paths["/users"]
	assert.NotNil(t, usersPath.Get)
	assert.NotNil(t, usersPath.Post)

	userByIDPath := paths["/users/{id}"]
	assert.NotNil(t, userByIDPath.Get)

	// Validate path parameters
	assert.Len(t, userByIDPath.Get.Parameters, 1)
	assert.Equal(t, "id", userByIDPath.Get.Parameters[0].Value.Name)
	assert.Equal(t, "path", userByIDPath.Get.Parameters[0].Value.In)
	assert.True(t, userByIDPath.Get.Parameters[0].Value.Required)

	// Validate request body exists for POST
	assert.NotNil(t, usersPath.Post.RequestBody)

	// Validate response schemas exist
	getUsersResp := usersPath.Get.Responses.Map()["200"]
	require.NotNil(t, getUsersResp)
	require.NotNil(t, getUsersResp.Value)
	require.NotNil(t, getUsersResp.Value.Content)

	jsonContent := getUsersResp.Value.Content.Get("application/json")
	require.NotNil(t, jsonContent)
	require.NotNil(t, jsonContent.Schema)
	require.NotNil(t, jsonContent.Schema.Value)

	// Response should be an array
	assert.Contains(t, jsonContent.Schema.Value.Type.Slice(), "array")
	assert.NotNil(t, jsonContent.Schema.Value.Items)
}

func TestGenerateSpecErrorResponses(t *testing.T) {
	config := OpenAPIConfig{
		Title:           "Error Test API",
		Description:     "Test error responses",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}

	transactions := []proxy.APITransaction{
		{
			Request: proxy.RequestData{
				Method: "GET",
				Path:   "/users/999",
			},
			Response: proxy.ResponseData{
				StatusCode: 404,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"error":"User not found","code":404}`),
			},
		},
		{
			Request: proxy.RequestData{
				Method: "GET",
				Path:   "/users/1",
			},
			Response: proxy.ResponseData{
				StatusCode: 200,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"id":1,"name":"Alice"}`),
			},
		},
		{
			Request: proxy.RequestData{
				Method: "POST",
				Path:   "/users",
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{}`),
			},
			Response: proxy.ResponseData{
				StatusCode: 500,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{"error":"Internal server error"}`),
			},
		},
	}

	generator := NewOpenAPIGenerator(config)
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Validate that multiple status codes are captured
	paths := spec.Paths.Map()

	// /users/{id} should have both 200 and 404
	usersByIDPath := paths["/users/{id}"]
	require.NotNil(t, usersByIDPath)
	require.NotNil(t, usersByIDPath.Get)
	assert.Contains(t, usersByIDPath.Get.Responses.Map(), "200")
	assert.Contains(t, usersByIDPath.Get.Responses.Map(), "404")

	// /users POST should have 500
	usersPath := paths["/users"]
	require.NotNil(t, usersPath)
	require.NotNil(t, usersPath.Post)
	assert.Contains(t, usersPath.Post.Responses.Map(), "500")
}

func TestGenerateSpecDeeplyNestedJSON(t *testing.T) {
	config := OpenAPIConfig{
		Title:           "Nested Test API",
		Description:     "Test deeply nested JSON",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}

	transactions := []proxy.APITransaction{
		{
			Request: proxy.RequestData{
				Method: "GET",
				Path:   "/organizations/1/departments/2/teams/3/members",
			},
			Response: proxy.ResponseData{
				StatusCode: 200,
				Headers: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: []byte(`{
					"organization": {
						"id": 1,
						"name": "Acme Corp",
						"departments": [
							{
								"id": 2,
								"name": "Engineering",
								"teams": [
									{
										"id": 3,
										"name": "Backend",
										"members": [
											{"id": 10, "name": "Alice", "role": "lead"},
											{"id": 11, "name": "Bob", "role": "developer"}
										]
									}
								]
							}
						]
					}
				}`),
			},
		},
	}

	generator := NewOpenAPIGenerator(config)
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Verify the spec was generated with nested structure
	paths := spec.Paths.Map()
	assert.Len(t, paths, 1)

	// Verify path was templatized
	_, hasTemplatized := paths["/organizations/{id}/departments/{id}/teams/{id}/members"]
	_, hasOriginal := paths["/organizations/1/departments/2/teams/3/members"]
	assert.True(t, hasTemplatized || hasOriginal, "Path should exist in some form")

	// Verify response schema exists and has nested properties
	for _, pathItem := range paths {
		if pathItem.Get != nil {
			resp := pathItem.Get.Responses.Map()["200"]
			require.NotNil(t, resp)
			require.NotNil(t, resp.Value)
			jsonContent := resp.Value.Content.Get("application/json")
			require.NotNil(t, jsonContent)
			require.NotNil(t, jsonContent.Schema)
			require.NotNil(t, jsonContent.Schema.Value)
			assert.Contains(t, jsonContent.Schema.Value.Type.Slice(), "object")
			assert.NotEmpty(t, jsonContent.Schema.Value.Properties)
		}
	}
}

func TestGenerateSpecPathTemplatization(t *testing.T) {
	config := OpenAPIConfig{
		Title:           "Templatize Test",
		Description:     "Test path templatization",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}

	transactions := []proxy.APITransaction{
		{
			Request:  proxy.RequestData{Method: "GET", Path: "/users/123"},
			Response: proxy.ResponseData{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"id":123}`)},
		},
		{
			Request:  proxy.RequestData{Method: "GET", Path: "/users/456"},
			Response: proxy.ResponseData{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"id":456}`)},
		},
		{
			Request:  proxy.RequestData{Method: "GET", Path: "/products/550e8400-e29b-41d4-a716-446655440000"},
			Response: proxy.ResponseData{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"uuid":"550e8400-e29b-41d4-a716-446655440000"}`)},
		},
		{
			Request:  proxy.RequestData{Method: "GET", Path: "/products/660e8400-e29b-41d4-a716-446655440001"},
			Response: proxy.ResponseData{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"uuid":"660e8400-e29b-41d4-a716-446655440001"}`)},
		},
	}

	generator := NewOpenAPIGenerator(config)
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	paths := spec.Paths.Map()

	// Numeric IDs should be templatized
	assert.Contains(t, paths, "/users/{id}")
	assert.NotContains(t, paths, "/users/123")
	assert.NotContains(t, paths, "/users/456")

	// UUIDs should be templatized
	assert.Contains(t, paths, "/products/{uuid}")
	assert.NotContains(t, paths, "/products/550e8400-e29b-41d4-a716-446655440000")
}

func TestGenerateSpecQueryParams(t *testing.T) {
	config := OpenAPIConfig{
		Title:           "Query Test",
		Description:     "Test query parameters",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}

	transactions := []proxy.APITransaction{
		{
			Request: proxy.RequestData{
				Method: "GET",
				Path:   "/search",
				QueryParams: map[string][]string{
					"q":     {"test"},
					"page":  {"1"},
					"limit": {"10"},
				},
			},
			Response: proxy.ResponseData{
				StatusCode: 200,
				Headers:    http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"results":[],"total":0}`),
			},
		},
	}

	generator := NewOpenAPIGenerator(config)
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	paths := spec.Paths.Map()
	searchPath := paths["/search"]
	require.NotNil(t, searchPath)
	require.NotNil(t, searchPath.Get)

	// Should have query parameters
	assert.Len(t, searchPath.Get.Parameters, 3)

	paramNames := make(map[string]bool)
	for _, p := range searchPath.Get.Parameters {
		paramNames[p.Value.Name] = true
		assert.Equal(t, "query", p.Value.In)
		assert.False(t, p.Value.Required)
	}
	assert.True(t, paramNames["q"])
	assert.True(t, paramNames["page"])
	assert.True(t, paramNames["limit"])
}

func TestSanitizeBodyForSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty data",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "invalid JSON",
			input:    []byte("not json"),
			expected: "",
		},
		{
			name:     "simple object",
			input:    []byte(`{"name":"Alice","age":30,"active":true}`),
			expected: `{"__boolean__":"__boolean__","__integer__":"__integer__","__string__":"__string__"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeBodyForSchema(tt.input)
			if tt.expected == "" {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				// Verify it's valid JSON
				var obj map[string]interface{}
				assert.NoError(t, json.Unmarshal(result, &obj))
			}
		})
	}
}
