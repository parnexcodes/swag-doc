package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/parnexcodes/swag-doc/internal/testcorpus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
