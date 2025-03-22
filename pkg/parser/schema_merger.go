package parser

import (
	"reflect"
)

// SchemaMerger merges multiple schemas into a single comprehensive schema
type SchemaMerger struct {
	schemas map[string][]Schema // path+method -> schemas
}

// NewSchemaMerger creates a new schema merger
func NewSchemaMerger() *SchemaMerger {
	return &SchemaMerger{
		schemas: make(map[string][]Schema),
	}
}

// AddSchema adds a schema to the merger for the given path and method
func (m *SchemaMerger) AddSchema(path, method string, schema Schema) {
	key := path + ":" + method
	m.schemas[key] = append(m.schemas[key], schema)
}

// MergeSchemas merges all schemas for the given path and method
func (m *SchemaMerger) MergeSchemas(path, method string) Schema {
	key := path + ":" + method
	schemas, exists := m.schemas[key]
	if !exists || len(schemas) == 0 {
		return Schema{}
	}

	if len(schemas) == 1 {
		return schemas[0]
	}

	// Use the first schema as a base
	result := schemas[0]

	// Merge in the rest
	for _, schema := range schemas[1:] {
		result = mergeSchema(result, schema)
	}

	return result
}

// mergeSchema merges two schemas into one
func mergeSchema(a, b Schema) Schema {
	// Start with a copy of the first schema
	result := a

	// If types are different, use object as the common denominator
	if a.Type != b.Type && a.Type != "" && b.Type != "" {
		result.Type = "object"
	}

	// Handle different schema types
	switch a.Type {
	case "object":
		result.Properties = mergeObjectProperties(a.Properties, b.Properties)
		result.Required = mergeRequired(a.Required, b.Required)
	case "array":
		if b.Type == "array" {
			// Merge array item schemas
			if a.Items != nil && b.Items != nil {
				mergedItems := mergeSchema(*a.Items, *b.Items)
				result.Items = &mergedItems
			} else if a.Items == nil && b.Items != nil {
				result.Items = b.Items
			}
		}
	}

	// Merge examples if possible
	if a.Example == nil && b.Example != nil {
		result.Example = b.Example
	}

	// Merge enums
	result.Enum = mergeEnums(a.Enum, b.Enum)

	// Handle format - prefer more specific format
	if a.Format == "" && b.Format != "" {
		result.Format = b.Format
	}

	// Merge nullability - if either schema can be null, the merged one can be too
	result.Nullable = a.Nullable || b.Nullable

	return result
}

// mergeObjectProperties merges the properties of two object schemas
func mergeObjectProperties(aProps, bProps map[string]Schema) map[string]Schema {
	if aProps == nil && bProps == nil {
		return nil
	}

	result := make(map[string]Schema)

	// Copy properties from a
	if aProps != nil {
		for name, schema := range aProps {
			result[name] = schema
		}
	}

	// Merge in properties from b
	if bProps != nil {
		for name, schema := range bProps {
			if existingSchema, exists := result[name]; exists {
				// Property exists in both - merge the schemas
				result[name] = mergeSchema(existingSchema, schema)
			} else {
				// Property only in b - add it
				result[name] = schema
			}
		}
	}

	return result
}

// mergeRequired merges required properties lists
func mergeRequired(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	// Create a map for quick lookup
	required := make(map[string]bool)
	for _, name := range a {
		required[name] = true
	}

	// Add properties from b that aren't already in the result
	for _, name := range b {
		required[name] = true
	}

	// Convert back to slice
	var result []string
	for name := range required {
		result = append(result, name)
	}

	return result
}

// mergeEnums merges enum values
func mergeEnums(a, b []interface{}) []interface{} {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	// Use a map to deduplicate
	enumMap := make(map[interface{}]bool)
	for _, val := range a {
		enumMap[val] = true
	}
	for _, val := range b {
		enumMap[val] = true
	}

	// Convert back to slice
	var result []interface{}
	for val := range enumMap {
		result = append(result, val)
	}

	return result
}

// MergeRequestBody merges request bodies from multiple examples
func MergeRequestBody(bodies []*RequestBody) *RequestBody {
	if len(bodies) == 0 {
		return nil
	}

	if len(bodies) == 1 {
		return bodies[0]
	}

	// Start with a copy of the first body
	result := &RequestBody{
		Description: bodies[0].Description,
		Required:    bodies[0].Required || bodies[1].Required, // Required if any is required
		Content:     make(map[string]MediaType),
	}

	// Merge content types
	for _, body := range bodies {
		for contentType, mediaType := range body.Content {
			if existingMediaType, exists := result.Content[contentType]; exists {
				// Merge schemas for the same content type
				merged := mediaType
				merged.Schema = mergeSchema(existingMediaType.Schema, mediaType.Schema)
				result.Content[contentType] = merged
			} else {
				// Add new content type
				result.Content[contentType] = mediaType
			}
		}
	}

	return result
}

// MergeResponse merges responses from multiple examples
func MergeResponse(responses []Response) Response {
	if len(responses) == 0 {
		return Response{}
	}

	if len(responses) == 1 {
		return responses[0]
	}

	// Start with a copy of the first response
	result := Response{
		Description: responses[0].Description,
		Content:     make(map[string]MediaType),
		Headers:     make(map[string]Header),
	}

	// Merge content types
	for _, response := range responses {
		for contentType, mediaType := range response.Content {
			if existingMediaType, exists := result.Content[contentType]; exists {
				// Merge schemas for the same content type
				merged := mediaType
				merged.Schema = mergeSchema(existingMediaType.Schema, mediaType.Schema)
				result.Content[contentType] = merged
			} else {
				// Add new content type
				result.Content[contentType] = mediaType
			}
		}

		// Merge headers
		for name, header := range response.Headers {
			if existingHeader, exists := result.Headers[name]; exists {
				// Merge header schemas
				merged := header
				merged.Schema = mergeSchema(existingHeader.Schema, header.Schema)
				result.Headers[name] = merged
			} else {
				// Add new header
				result.Headers[name] = header
			}
		}
	}

	return result
}

// AreSchemasEqual checks if two schemas are semantically equivalent
func AreSchemasEqual(a, b Schema) bool {
	// Compare basic properties
	if a.Type != b.Type || a.Format != b.Format || a.Nullable != b.Nullable {
		return false
	}

	// Compare nested properties for objects
	if a.Type == "object" && b.Type == "object" {
		if len(a.Properties) != len(b.Properties) {
			return false
		}

		for name, propA := range a.Properties {
			propB, exists := b.Properties[name]
			if !exists || !AreSchemasEqual(propA, propB) {
				return false
			}
		}
	}

	// Compare array items
	if a.Type == "array" && b.Type == "array" {
		if (a.Items == nil && b.Items != nil) || (a.Items != nil && b.Items == nil) {
			return false
		}

		if a.Items != nil && b.Items != nil && !AreSchemasEqual(*a.Items, *b.Items) {
			return false
		}
	}

	// Compare enums
	if !reflect.DeepEqual(a.Enum, b.Enum) {
		return false
	}

	return true
}
