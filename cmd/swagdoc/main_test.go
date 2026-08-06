package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parnexcodes/swag-doc/internal/testcorpus"
	"github.com/parnexcodes/swag-doc/pkg/proxy"
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
		"http://localhost:8080", false, true, nil, nil, "openapi"))

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
		"Test API", "Test Description", "1.0.0", "http://localhost:8080", false, true, nil, nil, "openapi")
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
		"http://localhost:8080", false, true, nil, nil, "openapi"))

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

// writeWSSessionFile writes a WebSocket session file into a data dir.
func writeWSSessionFile(t *testing.T, dir, name string, sessions []proxy.WebSocketSession) {
	t.Helper()
	data, err := json.Marshal(sessions)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
}

func TestGenerateDocsOpenAPIIgnoresWebSocketSessions(t *testing.T) {
	// The same HTTP corpus with and without a ws-session file must produce
	// byte-identical OpenAPI output.
	build := func(includeWS bool) []byte {
		outDir := t.TempDir()
		dataDir := t.TempDir()
		corpus := testcorpus.Build(testcorpus.Options{N: 20, Paths: 4, DistinctBodies: 5, RepeatedBodies: true})
		require.NoError(t, testcorpus.WriteSessionFile(dataDir, "session-test.json", corpus))
		if includeWS {
			writeWSSessionFile(t, dataDir, "ws-session-test.json", []proxy.WebSocketSession{{
				HandshakeRequest: proxy.RequestData{Method: "GET", Path: "/chat/1"},
				ResponseStatus:   101,
				Messages: []proxy.WebSocketMessage{{
					Direction: proxy.DirectionClientToServer,
					Opcode:    proxy.WSOpcodeText,
					Payload:   []byte(`{"msg":"hi"}`),
				}},
			}})
		}
		out := filepath.Join(outDir, "swagger.json")
		require.NoError(t, generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
			"http://localhost:8080", false, true, nil, nil, "openapi"))
		data, err := os.ReadFile(out)
		require.NoError(t, err)
		return data
	}

	withWS := build(true)
	withoutWS := build(false)
	assert.Equal(t, withoutWS, withWS, "WebSocket session files must not affect OpenAPI output")
}

func TestGenerateDocsAsyncAPIOutput(t *testing.T) {
	dataDir := t.TempDir()
	writeWSSessionFile(t, dataDir, "ws-session-test.json", []proxy.WebSocketSession{
		{
			HandshakeRequest: proxy.RequestData{Method: "GET", Path: "/chat/123"},
			ResponseStatus:   101,
			Messages: []proxy.WebSocketMessage{
				{Direction: proxy.DirectionClientToServer, Opcode: proxy.WSOpcodeText, Payload: []byte(`{"msg":"hi"}`)},
				{Direction: proxy.DirectionServerToClient, Opcode: proxy.WSOpcodeText, Payload: []byte(`{"reply":"yo"}`)},
			},
		},
	})
	// A plain HTTP session file must be ignored by the AsyncAPI generator.
	corpus := testcorpus.Build(testcorpus.Options{N: 5, Paths: 2, DistinctBodies: 2, RepeatedBodies: true})
	require.NoError(t, testcorpus.WriteSessionFile(dataDir, "session-test.json", corpus))

	outDir := t.TempDir()
	out := filepath.Join(outDir, "asyncapi.json")
	require.NoError(t, generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:3000", false, true, nil, nil, "asyncapi"))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "3.1.0", doc["asyncapi"])
	assert.Contains(t, doc, "info")
	assert.Contains(t, doc, "channels")
	assert.Contains(t, doc, "operations", "3.x documents declare top-level operations")

	// YAML output follows the extension rule.
	outYAML := filepath.Join(outDir, "asyncapi.yaml")
	require.NoError(t, generateDocs(outYAML, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:3000", false, true, nil, nil, "asyncapi"))
	yamlData, err := os.ReadFile(outYAML)
	require.NoError(t, err)
	var yamlDoc map[string]any
	require.NoError(t, yaml.Unmarshal(yamlData, &yamlDoc))
	assert.Equal(t, "3.1.0", yamlDoc["asyncapi"])
}

func TestGenerateDocsRejectsUnknownFormat(t *testing.T) {
	dataDir := t.TempDir()
	corpus := testcorpus.Build(testcorpus.Options{N: 2, Paths: 1, DistinctBodies: 1, RepeatedBodies: true})
	require.NoError(t, testcorpus.WriteSessionFile(dataDir, "session-test.json", corpus))

	outDir := t.TempDir()
	out := filepath.Join(outDir, "out.json")
	err := generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil, "grpc")
	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.True(t, os.IsNotExist(statErr), "no output file must be created for an unknown format")
}
func writeGraphQLTransactionFile(t *testing.T, dir string) {
	t.Helper()
	query := map[string]any{
		"query":     `query GetUser($id: ID!) { user(id: $id) { id name } }`,
		"variables": map[string]any{"id": "1"},
	}
	requestBody, err := json.Marshal(query)
	require.NoError(t, err)
	transactions := []proxy.APITransaction{{
		Request: proxy.RequestData{
			Method:  http.MethodPost,
			Path:    "/graphql",
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Body:    requestBody,
		},
		Response: proxy.ResponseData{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"data":{"user":{"id":"u1","name":"alice"}}}`),
		},
	}}
	data, err := json.Marshal(transactions)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session-graphql.json"), data, 0o644))
}

func TestGenerateDocsGraphQLOutput(t *testing.T) {
	dataDir := t.TempDir()
	writeGraphQLTransactionFile(t, dataDir)
	outDir := t.TempDir()
	firstPath := filepath.Join(outDir, "schema.graphql")
	secondPath := filepath.Join(outDir, "schema-second.graphql")

	require.NoError(t, generateDocs(firstPath, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil, "graphql"))
	require.NoError(t, generateDocs(secondPath, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil, "graphql"))

	first, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	second, err := os.ReadFile(secondPath)
	require.NoError(t, err)
	assert.Equal(t, first, second, "GraphQL generation must be deterministic")
	assert.Contains(t, string(first), "type Query")
	assert.Contains(t, string(first), "user(id: ID!): User")
	assert.NotContains(t, string(first), "alice")
	assert.NotContains(t, string(first), "u1")
}

func TestGenerateDocsGraphQLCleanup(t *testing.T) {
	dataDir := t.TempDir()
	writeGraphQLTransactionFile(t, dataDir)
	out := filepath.Join(t.TempDir(), "schema.graphql")
	require.NoError(t, generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", true, true, nil, nil, "graphql"))
	_, err := os.Stat(dataDir)
	assert.Error(t, err)
}

func TestGenerateGraphQLDocsWriteFailureLeavesNoOutput(t *testing.T) {
	dataDir := t.TempDir()
	writeGraphQLTransactionFile(t, dataDir)
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("blocker"), 0o644))
	out := filepath.Join(blocker, "schema.graphql")
	err := generateDocs(out, dataDir, "Test API", "Test Description", "1.0.0",
		"http://localhost:8080", false, true, nil, nil, "graphql")
	require.Error(t, err)
	_, statErr := os.Stat(out)
	assert.Error(t, statErr, "failed generation must not create output")
}
