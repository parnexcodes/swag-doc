package asyncapi

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Format identifies a serialization format for generated documents.
type Format string

const (
	// FormatJSON serializes the document as indented JSON. It is the default.
	FormatJSON Format = "json"
	// FormatYAML serializes the document as YAML.
	FormatYAML Format = "yaml"
)

// Marshal serializes the document in the requested format. YAML is produced
// by round-tripping the document through JSON, matching the OpenAPI
// generator's serialization contract.
func Marshal(doc *Document, format Format) ([]byte, error) {
	switch format {
	case FormatJSON:
		return json.MarshalIndent(doc, "", "  ")
	case FormatYAML:
		asJSON, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(asJSON, &m); err != nil {
			return nil, err
		}
		return yaml.Marshal(m)
	default:
		return nil, fmt.Errorf("unknown asyncapi format %q", format)
	}
}
