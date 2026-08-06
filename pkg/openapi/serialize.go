package openapi

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Format identifies a serialization format for generated specs.
type Format string

const (
	// FormatJSON serializes the spec as indented JSON. It is the default format.
	FormatJSON Format = "json"
	// FormatYAML serializes the spec as YAML.
	FormatYAML Format = "yaml"
)

// MarshalSpec serializes the generated OpenAPI spec in the requested format.
// The spec's JSON tags are the source of truth for both formats: YAML is
// produced by round-tripping the spec through JSON so the emitted document
// has exactly the shape of the JSON output.
func MarshalSpec(spec *OpenAPISpec, format Format) ([]byte, error) {
	switch format {
	case FormatJSON:
		return json.MarshalIndent(spec, "", "  ")
	case FormatYAML:
		return marshalYAML(spec)
	default:
		return nil, fmt.Errorf("unknown spec format %q", format)
	}
}

// marshalYAML converts the spec to YAML via a JSON round-trip. The spec
// struct (openapi3.T) carries only json tags, so marshaling it directly with
// yaml.v3 would emit Go field names; converting through JSON reuses the tag
// contract that defines the JSON output.
func marshalYAML(spec *OpenAPISpec) ([]byte, error) {
	asJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(asJSON, &doc); err != nil {
		return nil, err
	}
	return yaml.Marshal(doc)
}
