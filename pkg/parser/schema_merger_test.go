package parser

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchemaMerger(t *testing.T) {
	merger := NewSchemaMerger()
	assert.NotNil(t, merger)
	assert.NotNil(t, merger.schemas)
	assert.Empty(t, merger.schemas)
}

func TestAddSchema(t *testing.T) {
	merger := NewSchemaMerger()

	testPath := "/users"
	testMethod := "GET"
	testSchema := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"id": {
				Type:   "integer",
				Format: "int64",
			},
			"name": {
				Type: "string",
			},
		},
	}

	// Test adding a schema
	merger.AddSchema(testPath, testMethod, testSchema)

	// Check that the schema was added correctly
	key := testPath + ":" + testMethod
	schemas, exists := merger.schemas[key]
	assert.True(t, exists)
	assert.Len(t, schemas, 1)
	assert.Equal(t, testSchema, schemas[0])

	// Add another schema for the same path/method
	testSchema2 := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"id": {
				Type:   "integer",
				Format: "int64",
			},
			"name": {
				Type: "string",
			},
			"email": {
				Type: "string",
			},
		},
	}

	merger.AddSchema(testPath, testMethod, testSchema2)

	// Check that both schemas are stored
	schemas, exists = merger.schemas[key]
	assert.True(t, exists)
	assert.Len(t, schemas, 2)
	assert.Equal(t, testSchema, schemas[0])
	assert.Equal(t, testSchema2, schemas[1])
}

func TestMergeSchemas_NoSchemas(t *testing.T) {
	merger := NewSchemaMerger()

	// Test with non-existent path/method
	result := merger.MergeSchemas("/nonexistent", "GET")
	assert.Equal(t, Schema{}, result)

	// Test with empty schema list
	key := "/empty:GET"
	merger.schemas[key] = []Schema{}
	result = merger.MergeSchemas("/empty", "GET")
	assert.Equal(t, Schema{}, result)
}

func TestMergeSchemas_SingleSchema(t *testing.T) {
	merger := NewSchemaMerger()

	testPath := "/users"
	testMethod := "GET"
	testSchema := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"id": {
				Type:   "integer",
				Format: "int64",
			},
		},
	}

	merger.AddSchema(testPath, testMethod, testSchema)

	// When there's only one schema, it should be returned as-is
	result := merger.MergeSchemas(testPath, testMethod)
	assert.Equal(t, testSchema, result)
}

func TestMergeSchemas_MultipleSchemas(t *testing.T) {
	merger := NewSchemaMerger()

	testPath := "/users"
	testMethod := "GET"

	// Schema 1: User with id and name
	schema1 := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"id": {
				Type:   "integer",
				Format: "int64",
			},
			"name": {
				Type: "string",
			},
		},
		Required: []string{"id"},
	}

	// Schema 2: User with id, name, and email
	schema2 := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"id": {
				Type:   "integer",
				Format: "int64",
			},
			"name": {
				Type: "string",
			},
			"email": {
				Type:   "string",
				Format: "email",
			},
		},
		Required: []string{"id", "email"},
	}

	merger.AddSchema(testPath, testMethod, schema1)
	merger.AddSchema(testPath, testMethod, schema2)

	// Test merging of the schemas
	result := merger.MergeSchemas(testPath, testMethod)

	// The result should have all properties from both schemas
	assert.Equal(t, "object", result.Type)
	assert.Len(t, result.Properties, 3)

	// Check each property exists with correct type
	assert.Contains(t, result.Properties, "id")
	assert.Equal(t, "integer", result.Properties["id"].Type)
	assert.Equal(t, "int64", result.Properties["id"].Format)

	assert.Contains(t, result.Properties, "name")
	assert.Equal(t, "string", result.Properties["name"].Type)

	assert.Contains(t, result.Properties, "email")
	assert.Equal(t, "string", result.Properties["email"].Type)
	assert.Equal(t, "email", result.Properties["email"].Format)

	// Required fields should be merged
	assert.Len(t, result.Required, 2)
	assert.Contains(t, result.Required, "id")
	assert.Contains(t, result.Required, "email")
}

func TestMergeSchema_DifferentTypes(t *testing.T) {
	// Test merging schemas with different types
	schema1 := Schema{
		Type: "string",
	}

	schema2 := Schema{
		Type: "integer",
	}

	result := mergeSchema(schema1, schema2)

	// When merging different types, the result should be an object
	assert.Equal(t, "object", result.Type)
}

func TestMergeSchema_ObjectType(t *testing.T) {
	// Test merging two object schemas
	schema1 := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"name": {
				Type: "string",
			},
			"age": {
				Type:   "integer",
				Format: "int32",
			},
		},
		Required: []string{"name"},
	}

	schema2 := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"name": {
				Type: "string",
			},
			"email": {
				Type:   "string",
				Format: "email",
			},
		},
		Required: []string{"email"},
	}

	result := mergeSchema(schema1, schema2)

	// Check result type
	assert.Equal(t, "object", result.Type)

	// Check properties (should have all three)
	assert.Len(t, result.Properties, 3)
	assert.Contains(t, result.Properties, "name")
	assert.Contains(t, result.Properties, "age")
	assert.Contains(t, result.Properties, "email")

	// Check required fields (should have both)
	assert.Len(t, result.Required, 2)
	assert.Contains(t, result.Required, "name")
	assert.Contains(t, result.Required, "email")
}

func TestMergeSchema_ArrayType(t *testing.T) {
	// Test merging two array schemas
	schema1 := Schema{
		Type: "array",
		Items: &Schema{
			Type: "string",
		},
	}

	schema2 := Schema{
		Type: "array",
		Items: &Schema{
			Type: "string",
			Enum: []interface{}{"red", "green", "blue"},
		},
	}

	result := mergeSchema(schema1, schema2)

	// Check result type
	assert.Equal(t, "array", result.Type)

	// Check items (should have enum from schema2)
	require.NotNil(t, result.Items)
	assert.Equal(t, "string", result.Items.Type)
	assert.Len(t, result.Items.Enum, 3)
	assert.Contains(t, result.Items.Enum, "red")
	assert.Contains(t, result.Items.Enum, "green")
	assert.Contains(t, result.Items.Enum, "blue")

	// Test array with missing Items in one schema
	schema3 := Schema{
		Type: "array",
	}

	result = mergeSchema(schema3, schema2)

	// Check that items were added from schema2
	require.NotNil(t, result.Items)
	assert.Equal(t, "string", result.Items.Type)
	assert.Len(t, result.Items.Enum, 3)
}

func TestMergeSchema_Examples(t *testing.T) {
	// Test merging of example values
	schema1 := Schema{
		Type:    "string",
		Example: nil,
	}

	schema2 := Schema{
		Type:    "string",
		Example: "example value",
	}

	// When schema1 has nil example, use schema2's example
	result := mergeSchema(schema1, schema2)
	assert.Equal(t, "example value", result.Example)

	// When schema1 has example but schema2 doesn't, keep schema1's example
	result = mergeSchema(schema2, schema1)
	assert.Equal(t, "example value", result.Example)
}

func TestMergeSchema_Enums(t *testing.T) {
	// Test merging of enum values
	schema1 := Schema{
		Type: "string",
		Enum: []interface{}{"red", "green"},
	}

	schema2 := Schema{
		Type: "string",
		Enum: []interface{}{"green", "blue"},
	}

	result := mergeSchema(schema1, schema2)

	// Result should have all unique enum values
	assert.Len(t, result.Enum, 3)
	assert.Contains(t, result.Enum, "red")
	assert.Contains(t, result.Enum, "green")
	assert.Contains(t, result.Enum, "blue")
}

func TestMergeSchema_Format(t *testing.T) {
	// Test merging of format values
	schema1 := Schema{
		Type:   "string",
		Format: "",
	}

	schema2 := Schema{
		Type:   "string",
		Format: "email",
	}

	// When schema1 has empty format, use schema2's format
	result := mergeSchema(schema1, schema2)
	assert.Equal(t, "email", result.Format)

	// When both have format, keep the first one
	schema1.Format = "uuid"
	result = mergeSchema(schema1, schema2)
	assert.Equal(t, "uuid", result.Format)
}

func TestMergeSchema_Nullable(t *testing.T) {
	// Test merging of nullable property
	schema1 := Schema{
		Type:     "string",
		Nullable: false,
	}

	schema2 := Schema{
		Type:     "string",
		Nullable: true,
	}

	// If either schema is nullable, the result should be nullable
	result := mergeSchema(schema1, schema2)
	assert.True(t, result.Nullable)

	// Order shouldn't matter
	result = mergeSchema(schema2, schema1)
	assert.True(t, result.Nullable)

	// Both false should stay false
	schema2.Nullable = false
	result = mergeSchema(schema1, schema2)
	assert.False(t, result.Nullable)
}

func TestMergeObjectProperties_NilProps(t *testing.T) {
	// Test with nil properties
	result := mergeObjectProperties(nil, nil)
	assert.Nil(t, result)

	// Test with one nil properties
	props := map[string]Schema{
		"name": {Type: "string"},
	}

	result = mergeObjectProperties(props, nil)
	assert.Equal(t, props, result)

	result = mergeObjectProperties(nil, props)
	assert.Equal(t, props, result)
}

func TestMergeObjectProperties_MergeProps(t *testing.T) {
	// Test merging properties
	props1 := map[string]Schema{
		"name": {Type: "string"},
		"age":  {Type: "integer"},
	}

	props2 := map[string]Schema{
		"name":  {Type: "string", Format: "email"},
		"email": {Type: "string"},
	}

	result := mergeObjectProperties(props1, props2)

	// Should have all three properties
	assert.Len(t, result, 3)
	assert.Contains(t, result, "name")
	assert.Contains(t, result, "age")
	assert.Contains(t, result, "email")

	// Name property should be merged
	assert.Equal(t, "string", result["name"].Type)
	assert.Equal(t, "email", result["name"].Format)
}

func TestMergeRequired(t *testing.T) {
	// Test merging required properties
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected []string
	}{
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: []string{},
		},
		{
			name:     "a empty",
			a:        []string{},
			b:        []string{"name", "email"},
			expected: []string{"name", "email"},
		},
		{
			name:     "b empty",
			a:        []string{"id", "name"},
			b:        []string{},
			expected: []string{"id", "name"},
		},
		{
			name:     "overlapping values",
			a:        []string{"id", "name"},
			b:        []string{"name", "email"},
			expected: []string{"id", "name", "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeRequired(tt.a, tt.b)

			// Sort for reliable comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeEnums(t *testing.T) {
	// Test merging enum values
	tests := []struct {
		name     string
		a        []interface{}
		b        []interface{}
		expected int // Just check length as order can vary
	}{
		{
			name:     "both empty",
			a:        []interface{}{},
			b:        []interface{}{},
			expected: 0,
		},
		{
			name:     "a empty",
			a:        []interface{}{},
			b:        []interface{}{"red", "green"},
			expected: 2,
		},
		{
			name:     "b empty",
			a:        []interface{}{"red", "blue"},
			b:        []interface{}{},
			expected: 2,
		},
		{
			name:     "overlapping values",
			a:        []interface{}{"red", "green"},
			b:        []interface{}{"green", "blue"},
			expected: 3,
		},
		{
			name:     "duplicate values",
			a:        []interface{}{"red", "green", "red"},
			b:        []interface{}{"green", "blue", "green"},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeEnums(tt.a, tt.b)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestMergeRequestBody(t *testing.T) {
	// Test with nil bodies
	assert.Nil(t, MergeRequestBody(nil))
	assert.Nil(t, MergeRequestBody([]*RequestBody{}))

	// Test with single body
	body1 := &RequestBody{
		Description: "Test body",
		Required:    true,
		Content: map[string]MediaType{
			"application/json": {
				Schema: Schema{Type: "object"},
			},
		},
	}

	result := MergeRequestBody([]*RequestBody{body1})
	assert.Equal(t, body1, result)

	// Test merging two bodies
	body2 := &RequestBody{
		Description: "Another test body",
		Required:    false,
		Content: map[string]MediaType{
			"application/json": {
				Schema: Schema{
					Type: "object",
					Properties: map[string]Schema{
						"name": {Type: "string"},
					},
				},
			},
			"application/xml": {
				Schema: Schema{Type: "object"},
			},
		},
	}

	result = MergeRequestBody([]*RequestBody{body1, body2})

	// Check basic properties
	assert.Equal(t, body1.Description, result.Description)
	assert.True(t, result.Required) // true || false = true

	// Check content
	assert.Len(t, result.Content, 2)
	assert.Contains(t, result.Content, "application/json")
	assert.Contains(t, result.Content, "application/xml")

	// Check that schemas were merged for application/json
	jsonSchema := result.Content["application/json"].Schema
	assert.Equal(t, "object", jsonSchema.Type)
	assert.Contains(t, jsonSchema.Properties, "name")
}

func TestMergeResponse(t *testing.T) {
	// Test with empty responses
	assert.Equal(t, Response{}, MergeResponse(nil))
	assert.Equal(t, Response{}, MergeResponse([]Response{}))

	// Test with single response
	resp1 := Response{
		Description: "Test response",
		Content: map[string]MediaType{
			"application/json": {
				Schema: Schema{Type: "object"},
			},
		},
		Headers: map[string]Header{
			"X-Rate-Limit": {
				Schema: Schema{Type: "integer"},
			},
		},
	}

	result := MergeResponse([]Response{resp1})
	assert.Equal(t, resp1, result)

	// Test merging two responses
	resp2 := Response{
		Description: "Another test response",
		Content: map[string]MediaType{
			"application/json": {
				Schema: Schema{
					Type: "object",
					Properties: map[string]Schema{
						"name": {Type: "string"},
					},
				},
			},
			"application/xml": {
				Schema: Schema{Type: "object"},
			},
		},
		Headers: map[string]Header{
			"X-Rate-Limit": {
				Schema: Schema{Type: "integer", Format: "int64"},
			},
			"X-Request-ID": {
				Schema: Schema{Type: "string", Format: "uuid"},
			},
		},
	}

	result = MergeResponse([]Response{resp1, resp2})

	// Check basic properties
	assert.Equal(t, resp1.Description, result.Description)

	// Check content
	assert.Len(t, result.Content, 2)
	assert.Contains(t, result.Content, "application/json")
	assert.Contains(t, result.Content, "application/xml")

	// Check that schemas were merged for application/json
	jsonSchema := result.Content["application/json"].Schema
	assert.Equal(t, "object", jsonSchema.Type)
	assert.Contains(t, jsonSchema.Properties, "name")

	// Check headers
	assert.Len(t, result.Headers, 2)
	assert.Contains(t, result.Headers, "X-Rate-Limit")
	assert.Contains(t, result.Headers, "X-Request-ID")

	// Check that header schemas were merged
	rateLimitHeader := result.Headers["X-Rate-Limit"]
	assert.Equal(t, "integer", rateLimitHeader.Schema.Type)
	assert.Equal(t, "int64", rateLimitHeader.Schema.Format)
}

func TestAreSchemasEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        Schema
		b        Schema
		expected bool
	}{
		{
			name:     "identical empty schemas",
			a:        Schema{},
			b:        Schema{},
			expected: true,
		},
		{
			name:     "identical schemas with properties",
			a:        Schema{Type: "object", Format: "email", Nullable: true},
			b:        Schema{Type: "object", Format: "email", Nullable: true},
			expected: true,
		},
		{
			name:     "different types",
			a:        Schema{Type: "string"},
			b:        Schema{Type: "integer"},
			expected: false,
		},
		{
			name:     "different formats",
			a:        Schema{Type: "string", Format: "email"},
			b:        Schema{Type: "string", Format: "uri"},
			expected: false,
		},
		{
			name:     "different nullable",
			a:        Schema{Type: "string", Nullable: true},
			b:        Schema{Type: "string", Nullable: false},
			expected: false,
		},
		{
			name: "different object properties",
			a: Schema{
				Type: "object",
				Properties: map[string]Schema{
					"name": {Type: "string"},
					"age":  {Type: "integer"},
				},
			},
			b: Schema{
				Type: "object",
				Properties: map[string]Schema{
					"name":  {Type: "string"},
					"email": {Type: "string"},
				},
			},
			expected: false,
		},
		{
			name: "same object properties, different order",
			a: Schema{
				Type: "object",
				Properties: map[string]Schema{
					"name": {Type: "string"},
					"age":  {Type: "integer"},
				},
			},
			b: Schema{
				Type: "object",
				Properties: map[string]Schema{
					"age":  {Type: "integer"},
					"name": {Type: "string"},
				},
			},
			expected: true,
		},
		{
			name: "different array items",
			a: Schema{
				Type:  "array",
				Items: &Schema{Type: "string"},
			},
			b: Schema{
				Type:  "array",
				Items: &Schema{Type: "integer"},
			},
			expected: false,
		},
		{
			name: "one has array items, one doesn't",
			a: Schema{
				Type:  "array",
				Items: &Schema{Type: "string"},
			},
			b: Schema{
				Type: "array",
			},
			expected: false,
		},
		{
			name: "different enums",
			a: Schema{
				Type: "string",
				Enum: []interface{}{"red", "green"},
			},
			b: Schema{
				Type: "string",
				Enum: []interface{}{"red", "blue"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AreSchemasEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
