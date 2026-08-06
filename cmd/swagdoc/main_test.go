package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parnexcodes/swag-doc/internal/testcorpus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// dirSnapshot captures the names and contents of every file in a directory.
func dirSnapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		require.False(t, entry.IsDir(), "unexpected subdirectory %s", entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		snapshot[entry.Name()] = data
	}
	return snapshot
}

func TestGenerateDocsLeavesDataDirUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	outDir := t.TempDir()

	corpus := testcorpus.Build(testcorpus.Options{N: 50, Paths: 8, DistinctBodies: 10, RepeatedBodies: true})
	require.NoError(t, testcorpus.WriteSessionFile(dataDir, "session-test.json", corpus))

	before := dirSnapshot(t, dataDir)

	out := filepath.Join(outDir, "swagger.json")
	require.NoError(t, generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil))

	// The data directory must be byte-for-byte unchanged: no session file
	// created, nothing modified.
	after := dirSnapshot(t, dataDir)
	assert.Equal(t, before, after)

	// The output was still produced
	_, err := os.Stat(out)
	require.NoError(t, err)
}

func TestGenerateDocsErrorsOnMissingDataDir(t *testing.T) {
	outDir := t.TempDir()
	err := generateDocs(filepath.Join(outDir, "swagger.json"), filepath.Join(outDir, "missing"),
		"Test API", "Test Description", "1.0.0", "http://localhost:8080", false, true, nil, nil)
	assert.Error(t, err)
}

// generateDocsTo runs generateDocs over a fresh corpus written into a temp
// data dir and returns the produced output file contents.
func generateDocsTo(t *testing.T, output string) []byte {
	t.Helper()
	dataDir := t.TempDir()
	corpus := testcorpus.Build(testcorpus.Options{N: 50, Paths: 8, DistinctBodies: 10, RepeatedBodies: true})
	require.NoError(t, testcorpus.WriteSessionFile(dataDir, "session-test.json", corpus))

	require.NoError(t, generateDocs(output, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil))

	data, err := os.ReadFile(output)
	require.NoError(t, err)
	return data
}

func TestGenerateDocsYAMLOutput(t *testing.T) {
	outDir := t.TempDir()

	for _, name := range []string{"swagger.yaml", "docs.yml", "SWAGGER.YAML"} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(outDir, name)
			data := generateDocsTo(t, out)

			var doc map[string]any
			require.NoError(t, yaml.Unmarshal(data, &doc), "output %s must parse as YAML", name)
			openapiVer, ok := doc["openapi"].(string)
			require.True(t, ok, "openapi field must be a string, got %T", doc["openapi"])
			assert.True(t, strings.HasPrefix(openapiVer, "3.0"), "openapi version %q must be 3.0.x", openapiVer)
			assert.NotNil(t, doc["paths"], "paths field must be present")
		})
	}
}

func TestGenerateDocsJSONOutputUnchanged(t *testing.T) {
	outDir := t.TempDir()
	outJSON := filepath.Join(outDir, "swagger.json")
	outYAML := filepath.Join(outDir, "swagger.yaml")

	jsonData := generateDocsTo(t, outJSON)
	// The JSON path must keep producing indented JSON, as before.
	require.True(t, strings.HasPrefix(string(jsonData), "{\n  "), "JSON output must be indented")

	var jsonDoc, yamlDoc any
	require.NoError(t, json.Unmarshal(jsonData, &jsonDoc))
	yamlData := generateDocsTo(t, outYAML)
	require.NoError(t, yaml.Unmarshal(yamlData, &yamlDoc))

	// Compare canonical forms: re-marshal both parsed documents with the
	// same indentation so key ordering and numeric representation (JSON
	// float64 vs YAML int) cannot cause false mismatches.
	jsonCanon, err := json.MarshalIndent(jsonDoc, "", "  ")
	require.NoError(t, err)
	yamlCanon, err := json.MarshalIndent(yamlDoc, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, string(jsonCanon), string(yamlCanon),
		"YAML and JSON outputs must describe the same OpenAPI document")
}
