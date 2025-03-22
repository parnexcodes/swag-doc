package parser

import (
	"encoding/json"
	"regexp"
	"strconv"
	"time"
)

// TypeInferrer analyzes values to determine their types more intelligently
type TypeInferrer struct {
	typePatterns map[string]*regexp.Regexp
	samples      map[string][]interface{}
	maxSamples   int
}

// NewTypeInferrer creates a new type inference engine
func NewTypeInferrer(maxSamples int) *TypeInferrer {
	if maxSamples <= 0 {
		maxSamples = 10
	}

	t := &TypeInferrer{
		samples:    make(map[string][]interface{}),
		maxSamples: maxSamples,
	}

	// Initialize patterns for special types
	t.typePatterns = map[string]*regexp.Regexp{
		"uuid":     regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`),
		"email":    regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`),
		"uri":      regexp.MustCompile(`^(https?|ftp)://[^\s/$.?#].[^\s]*$`),
		"date":     regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`),
		"time":     regexp.MustCompile(`^\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`),
		"datetime": regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`),
		"ipv4":     regexp.MustCompile(`^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`),
		"ipv6":     regexp.MustCompile(`^(([0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$`),
	}

	return t
}

// AddSample adds a new sample value for analysis
func (t *TypeInferrer) AddSample(path string, value interface{}) {
	// Skip null values
	if value == nil {
		return
	}

	// Limit the number of samples we collect
	if samples, exists := t.samples[path]; exists && len(samples) >= t.maxSamples {
		return
	}

	t.samples[path] = append(t.samples[path], value)
}

// ResetSamples clears all collected samples
func (t *TypeInferrer) ResetSamples() {
	t.samples = make(map[string][]interface{})
}

// InferSchema generates a refined schema based on collected samples
func (t *TypeInferrer) InferSchema(path string) *Schema {
	samples, exists := t.samples[path]
	if !exists || len(samples) == 0 {
		return nil
	}

	// Start with a basic schema based on the first sample
	schema := t.inferType(samples[0])

	// Check if all samples are of the same type
	sameType := true
	for _, sample := range samples[1:] {
		sampleSchema := t.inferType(sample)
		if sampleSchema.Type != schema.Type {
			sameType = false
			break
		}
	}

	// If mixed types, try to find a common type or use oneOf
	if !sameType {
		schema = t.handleMixedTypes(path)
	} else {
		// For same types, enhance the schema with additional info
		schema = t.enhanceSchema(schema, samples)
	}

	return schema
}

// inferType determines the type of a single value
func (t *TypeInferrer) inferType(value interface{}) *Schema {
	schema := &Schema{}

	switch v := value.(type) {
	case map[string]interface{}:
		schema.Type = "object"
		schema.Properties = make(map[string]Schema)

		// Infer types for all properties
		for propName, propValue := range v {
			propSchema := t.inferType(propValue)
			if propSchema != nil {
				schema.Properties[propName] = *propSchema
			}
		}

	case []interface{}:
		schema.Type = "array"

		// Try to infer the type of array items
		if len(v) > 0 {
			itemSchema := t.inferType(v[0])
			schema.Items = itemSchema
		}

	case string:
		schema.Type = "string"

		// Try to infer more specific string formats
		format := t.inferStringFormat(v)
		if format != "" {
			schema.Format = format
		}

		// Check for enum potential if string is short
		if len(v) < 50 {
			schema.Example = v
		}

	case float64:
		// Check if it's actually an integer
		if v == float64(int64(v)) {
			schema.Type = "integer"
			schema.Format = "int64"
		} else {
			schema.Type = "number"
			schema.Format = "double"
		}
		schema.Example = v

	case bool:
		schema.Type = "boolean"
		schema.Example = v

	case nil:
		schema.Type = "null"

	default:
		// For any other types, convert to string and try to infer
		jsonBytes, err := json.Marshal(v)
		if err == nil {
			strValue := string(jsonBytes)
			schema.Type = "string"
			format := t.inferStringFormat(strValue)
			if format != "" {
				schema.Format = format
			}
		}
	}

	return schema
}

// inferStringFormat tries to determine if a string matches a known format
func (t *TypeInferrer) inferStringFormat(value string) string {
	// Try all patterns
	for format, pattern := range t.typePatterns {
		if pattern.MatchString(value) {
			return format
		}
	}

	// Additional checks for formats without simple regex

	// Check if it's a date-time by parsing
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return "date-time"
	}

	// Check for date
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return "date"
	}

	// Check if it's a number in string
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return "numeric"
	}

	// No specific format detected
	return ""
}

// handleMixedTypes deals with cases where samples have different types
func (t *TypeInferrer) handleMixedTypes(path string) *Schema {
	samples := t.samples[path]

	// Try to find a common type that works for all
	nullCount := 0
	stringCount := 0
	numberCount := 0
	intCount := 0
	boolCount := 0
	objectCount := 0
	arrayCount := 0

	for _, sample := range samples {
		switch v := sample.(type) {
		case nil:
			nullCount++
		case string:
			stringCount++
		case float64:
			numberCount++
			if v == float64(int64(v)) {
				intCount++
			}
		case bool:
			boolCount++
		case map[string]interface{}:
			objectCount++
		case []interface{}:
			arrayCount++
		}
	}

	// Determine if one type dominates
	total := len(samples)
	schema := &Schema{}

	// Check for nullable type
	if nullCount > 0 && nullCount < total {
		schema.Nullable = true
	}

	// Choose type based on majority
	maxCount := 0
	dominantType := ""

	if stringCount > maxCount {
		maxCount = stringCount
		dominantType = "string"
	}
	if numberCount > maxCount {
		maxCount = numberCount
		dominantType = "number"
		// Check if all numbers are integers
		if intCount == numberCount {
			dominantType = "integer"
		}
	}
	if boolCount > maxCount {
		maxCount = boolCount
		dominantType = "boolean"
	}
	if objectCount > maxCount {
		maxCount = objectCount
		dominantType = "object"
	}
	if arrayCount > maxCount {
		maxCount = arrayCount
		dominantType = "array"
	}

	// If one type is clearly dominant (>70%)
	if float64(maxCount)/float64(total) > 0.7 {
		schema.Type = dominantType

		// Handle properties for objects or items for arrays
		if dominantType == "object" {
			schema.Properties = t.inferObjectProperties(path)
		} else if dominantType == "array" {
			schema.Items = t.inferArrayItems(path)
		} else if dominantType == "string" {
			// Get a sample string to infer format
			for _, sample := range samples {
				if str, ok := sample.(string); ok {
					format := t.inferStringFormat(str)
					if format != "" {
						schema.Format = format
						break
					}
				}
			}
		}
	} else {
		// If no clear dominant type, use anyOf or fall back to string
		// For simplicity here, we'll just use string with format 'any'
		schema.Type = "string"
		schema.Format = "any"
	}

	return schema
}

// inferObjectProperties analyzes object samples and infers a common schema
func (t *TypeInferrer) inferObjectProperties(path string) map[string]Schema {
	properties := make(map[string]Schema)
	requiredProps := make(map[string]int)
	propCounts := make(map[string]int)

	// Find all possible properties across all object samples
	for _, sample := range t.samples[path] {
		if obj, ok := sample.(map[string]interface{}); ok {
			for propName, propValue := range obj {
				propCounts[propName]++

				// Add sample for this property
				propPath := path + "." + propName
				t.AddSample(propPath, propValue)

				// Track non-null properties for required determination
				if propValue != nil {
					requiredProps[propName]++
				}
			}
		}
	}

	// Count total object samples
	objectCount := 0
	for _, sample := range t.samples[path] {
		if _, ok := sample.(map[string]interface{}); ok {
			objectCount++
		}
	}

	// Generate schema for each property
	for propName := range propCounts {
		propPath := path + "." + propName
		propSchema := t.InferSchema(propPath)

		if propSchema != nil {
			// Make property nullable if it's sometimes absent
			if propCounts[propName] < objectCount {
				propSchema.Nullable = true
			}

			properties[propName] = *propSchema
		}
	}

	return properties
}

// inferArrayItems analyzes array samples and infers the item schema
func (t *TypeInferrer) inferArrayItems(path string) *Schema {
	// Collect all array items
	var allItems []interface{}

	for _, sample := range t.samples[path] {
		if arr, ok := sample.([]interface{}); ok {
			for _, item := range arr {
				// Add each item as a sample
				itemPath := path + "[]"
				t.AddSample(itemPath, item)
				allItems = append(allItems, item)
			}
		}
	}

	// No items found
	if len(allItems) == 0 {
		return nil
	}

	// Infer schema for array items
	itemPath := path + "[]"
	return t.InferSchema(itemPath)
}

// enhanceSchema adds additional information to a schema based on samples
func (t *TypeInferrer) enhanceSchema(schema *Schema, samples []interface{}) *Schema {
	switch schema.Type {
	case "string":
		return t.enhanceStringSchema(schema, samples)
	case "integer", "number":
		return t.enhanceNumberSchema(schema, samples)
	case "array":
		return t.enhanceArraySchema(schema, samples)
	}

	return schema
}

// enhanceStringSchema adds pattern, enum, etc. for string schemas
func (t *TypeInferrer) enhanceStringSchema(schema *Schema, samples []interface{}) *Schema {
	// Check for enum potential
	if len(samples) >= 2 && len(samples) <= 10 {
		// Convert samples to strings
		var strSamples []string
		for _, s := range samples {
			if str, ok := s.(string); ok {
				strSamples = append(strSamples, str)
			}
		}

		// If all strings are short, and there are few unique values, it might be an enum
		uniqueValues := make(map[string]bool)
		allShort := true

		for _, s := range strSamples {
			uniqueValues[s] = true
			if len(s) > 30 {
				allShort = false
			}
		}

		// If we have a small set of unique values and they're all short, suggest enum
		if len(uniqueValues) <= 5 && allShort && len(uniqueValues) < len(strSamples) {
			var enumValues []interface{}
			for val := range uniqueValues {
				enumValues = append(enumValues, val)
			}
			schema.Enum = enumValues
		}
	}

	// Check for common formats if not already set
	if schema.Format == "" && len(samples) > 0 {
		if str, ok := samples[0].(string); ok {
			schema.Format = t.inferStringFormat(str)
		}
	}

	return schema
}

// enhanceNumberSchema adds min, max, etc. for number schemas
func (t *TypeInferrer) enhanceNumberSchema(schema *Schema, samples []interface{}) *Schema {
	// If we have enough numeric samples, try to determine ranges
	if len(samples) >= 3 {
		var min, max float64
		var allIntegers = true

		// Initialize with first value
		if num, ok := samples[0].(float64); ok {
			min, max = num, num
			allIntegers = allIntegers && (num == float64(int64(num)))
		}

		// Find min/max and check if all values are integers
		for _, s := range samples[1:] {
			if num, ok := s.(float64); ok {
				if num < min {
					min = num
				}
				if num > max {
					max = num
				}
				allIntegers = allIntegers && (num == float64(int64(num)))
			}
		}

		// Set format based on range and pattern
		if allIntegers {
			schema.Type = "integer"
			if min >= 0 && max <= 255 {
				schema.Format = "int32"
			} else {
				schema.Format = "int64"
			}
		} else {
			schema.Type = "number"
			schema.Format = "double"
		}
	}

	return schema
}

// enhanceArraySchema improves array schema with item information
func (t *TypeInferrer) enhanceArraySchema(schema *Schema, samples []interface{}) *Schema {
	// Collect a sample of array items
	var allItems []interface{}

	for _, s := range samples {
		if arr, ok := s.([]interface{}); ok {
			for _, item := range arr {
				allItems = append(allItems, item)
				if len(allItems) >= t.maxSamples {
					break
				}
			}
		}
	}

	// If we have items, infer their schema
	if len(allItems) > 0 {
		itemSchema := t.inferType(allItems[0])

		// Check if all items have the same type
		sameType := true
		for _, item := range allItems[1:] {
			currSchema := t.inferType(item)
			if currSchema.Type != itemSchema.Type {
				sameType = false
				break
			}
		}

		if sameType {
			// Enhance the item schema
			itemSchema = t.enhanceSchema(itemSchema, allItems)
		} else {
			// Mixed types in array
			itemSchema.Type = "string" // Simplification - could use oneOf
			itemSchema.Format = "any"
		}

		schema.Items = itemSchema
	}

	return schema
}

// ApplyTypeInference enhances an existing schema with improved type detection
func ApplyTypeInference(schema Schema, samples []interface{}, maxSamples int) Schema {
	inferrer := NewTypeInferrer(maxSamples)

	// Add all samples
	for _, sample := range samples {
		inferrer.AddSample("root", sample)
	}

	// Get improved schema
	improved := inferrer.InferSchema("root")
	if improved == nil {
		return schema
	}

	// Update the original schema with improved information
	result := schema

	// If type was unknown, use the inferred type
	if result.Type == "" {
		result.Type = improved.Type
	}

	// Merge formats if appropriate
	if result.Format == "" && improved.Format != "" {
		result.Format = improved.Format
	}

	// Use inferred enum values if available
	if len(result.Enum) == 0 && len(improved.Enum) > 0 {
		result.Enum = improved.Enum
	}

	// For objects, merge properties
	if result.Type == "object" && improved.Type == "object" {
		if result.Properties == nil {
			result.Properties = make(map[string]Schema)
		}

		// Add any properties from improved schema
		for propName, propSchema := range improved.Properties {
			if _, exists := result.Properties[propName]; !exists {
				result.Properties[propName] = propSchema
			} else {
				// Merge with existing property
				existingProp := result.Properties[propName]
				mergedProp := mergeSchemaWithImprovement(existingProp, propSchema)
				result.Properties[propName] = mergedProp
			}
		}
	}

	// For arrays, enhance item schema
	if result.Type == "array" && improved.Type == "array" {
		if result.Items == nil && improved.Items != nil {
			result.Items = improved.Items
		} else if result.Items != nil && improved.Items != nil {
			mergedItems := mergeSchemaWithImprovement(*result.Items, *improved.Items)
			result.Items = &mergedItems
		}
	}

	return result
}

// mergeSchemaWithImprovement merges an original schema with improved information
func mergeSchemaWithImprovement(original, improved Schema) Schema {
	result := original

	// Use improved type if original is unknown
	if result.Type == "" {
		result.Type = improved.Type
	}

	// Use improved format if original is not specified
	if result.Format == "" && improved.Format != "" {
		result.Format = improved.Format
	}

	// Use improved enum if original has none
	if len(result.Enum) == 0 && len(improved.Enum) > 0 {
		result.Enum = improved.Enum
	}

	// Use improved example if original has none
	if result.Example == nil && improved.Example != nil {
		result.Example = improved.Example
	}

	// Merge object properties
	if result.Type == "object" && improved.Type == "object" {
		if result.Properties == nil {
			result.Properties = improved.Properties
		} else if improved.Properties != nil {
			// Add any missing properties from improved
			for propName, propSchema := range improved.Properties {
				if _, exists := result.Properties[propName]; !exists {
					result.Properties[propName] = propSchema
				}
			}
		}
	}

	// Merge array items
	if result.Type == "array" && improved.Type == "array" {
		if result.Items == nil && improved.Items != nil {
			result.Items = improved.Items
		}
	}

	return result
}
