package openapi

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/parnexcodes/swag-doc/internal/testcorpus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// buildTestSpec generates a spec from a deterministic corpus so serialization
// tests exercise a realistic document (info, paths, schemas, examples).
func buildTestSpec(t *testing.T) *OpenAPISpec {
	t.Helper()
	corpus := testcorpus.Build(testcorpus.Options{N: 30, Paths: 5, DistinctBodies: 6, RepeatedBodies: true})
	generator := NewOpenAPIGenerator(benchConfig())
	for _, tx := range corpus {
		generator.AddTransaction(tx)
	}
	spec, err := generator.GenerateSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)
	return spec
}

func TestMarshalSpecYAMLProducesOpenAPIDocument(t *testing.T) {
	data, err := MarshalSpec(buildTestSpec(t), FormatYAML)
	require.NoError(t, err)

	// The output must be exactly one YAML document.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "output must parse as YAML")
	var extra any
	assert.ErrorIs(t, dec.Decode(&extra), io.EOF, "output must be a single YAML document")

	openapiVer, ok := doc["openapi"].(string)
	require.True(t, ok, "openapi field must be a string, got %T", doc["openapi"])
	assert.True(t, strings.HasPrefix(openapiVer, "3.0"), "openapi version %q must be 3.0.x", openapiVer)
	assert.NotNil(t, doc["paths"], "paths field must be present")
}

func TestMarshalSpecYAMLEquivalentToJSON(t *testing.T) {
	spec := buildTestSpec(t)
	jsonData, err := MarshalSpec(spec, FormatJSON)
	require.NoError(t, err)
	yamlData, err := MarshalSpec(spec, FormatYAML)
	require.NoError(t, err)

	// Compare canonical forms: parse each format into generic values, then
	// re-marshal to JSON. This absorbs numeric type drift between parsers
	// (JSON float64 vs YAML int) and key ordering.
	canonical := func(t *testing.T, data []byte, format Format) string {
		t.Helper()
		var doc any
		switch format {
		case FormatJSON:
			require.NoError(t, json.Unmarshal(data, &doc))
		case FormatYAML:
			require.NoError(t, yaml.Unmarshal(data, &doc))
		}
		canon, err := json.Marshal(doc)
		require.NoError(t, err)
		return string(canon)
	}

	assert.Equal(t, canonical(t, jsonData, FormatJSON), canonical(t, yamlData, FormatYAML))
}

func TestMarshalSpecRejectsUnknownFormat(t *testing.T) {
	_, err := MarshalSpec(buildTestSpec(t), Format("xml"))
	assert.Error(t, err)
}

func TestMarshalSpecSerializationErrorPropagates(t *testing.T) {
	// A channel cannot be serialized; the spec's extensions flow into the
	// marshaled document, so this poisons serialization deterministically.
	spec := buildTestSpec(t)
	spec.Extensions = map[string]any{"unserializable": make(chan int)}

	for _, format := range []Format{FormatJSON, FormatYAML} {
		data, err := MarshalSpec(spec, format)
		assert.Error(t, err, "format %s must report the serialization failure", format)
		assert.Nil(t, data, "format %s must not return partial bytes", format)
	}
}
