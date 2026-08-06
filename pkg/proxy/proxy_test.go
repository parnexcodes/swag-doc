package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyServer(t *testing.T) {
	// Create a test server that will act as our API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer apiServer.Close()

	// Track if the interceptor was called
	interceptorCalled := false

	// Create an interceptor function for testing
	interceptor := func(transaction APITransaction) {
		interceptorCalled = true

		// Verify the transaction data
		if transaction.Request.Method != "POST" {
			t.Errorf("Expected method POST, got %s", transaction.Request.Method)
		}

		if transaction.Request.Path != "/test" {
			t.Errorf("Expected path /test, got %s", transaction.Request.Path)
		}

		if transaction.Response.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, transaction.Response.StatusCode)
		}
	}

	// We're just testing the interceptor logic, not the actual proxy server
	// so we don't need to use the proxy server variable

	// Start the test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request
		reqData, err := captureRequest(r)
		if err != nil {
			t.Fatalf("Error capturing request: %v", err)
		}

		// Create a custom response writer to capture the response
		rw := newResponseWriter(w, DefaultMaxBodyBytes)

		// Make a request to the API server
		client := &http.Client{}
		apiReq, _ := http.NewRequest(r.Method, apiServer.URL+r.URL.Path, bytes.NewBuffer(reqData.Body))
		for key, values := range r.Header {
			for _, value := range values {
				apiReq.Header.Add(key, value)
			}
		}

		apiResp, err := client.Do(apiReq)
		if err != nil {
			t.Fatalf("Error making request to API server: %v", err)
		}
		defer func() { _ = apiResp.Body.Close() }()

		// Copy the API response to our response writer
		for key, values := range apiResp.Header {
			for _, value := range values {
				rw.Header().Add(key, value)
			}
		}
		rw.WriteHeader(apiResp.StatusCode)
		_, _ = io.Copy(rw, apiResp.Body)

		// Capture the response
		respData := captureResponse(rw)

		// Create a complete transaction and pass it to the interceptor
		transaction := APITransaction{
			Request:  reqData,
			Response: respData,
		}

		// Pass the transaction to the interceptor
		interceptor(transaction)
	}))
	defer server.Close()

	// Make a request to the proxy server
	client := &http.Client{}
	req, _ := http.NewRequest("POST", server.URL+"/test", bytes.NewBufferString(`{"test":"data"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Error making request to proxy server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Verify the interceptor was called
	if !interceptorCalled {
		t.Error("Interceptor was not called")
	}

	// Verify the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestFileStorageConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewFileStorage(tmpDir)
	require.NoError(t, err)
	defer func() { _ = storage.Close() }()

	var wg sync.WaitGroup
	numGoroutines := 10
	txPerGoroutine := 20

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < txPerGoroutine; j++ {
				tx := APITransaction{
					Request: RequestData{
						Method: "GET",
						Path:   "/test",
					},
					Response: ResponseData{
						StatusCode: 200,
						Body:       []byte(`{"ok":true}`),
					},
				}
				if err := storage.Store(tx); err != nil {
					t.Errorf("goroutine %d: Store error: %v", id, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all transactions were stored
	assert.Len(t, storage.transactions, numGoroutines*txPerGoroutine)

	// Close and read back
	_ = storage.Close()

	// Read the session file and verify it's valid JSON
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	var sessionFile string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			sessionFile = filepath.Join(tmpDir, f.Name())
			break
		}
	}
	require.NotEmpty(t, sessionFile)

	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var transactions []APITransaction
	err = json.Unmarshal(data, &transactions)
	require.NoError(t, err)
	assert.Len(t, transactions, numGoroutines*txPerGoroutine)
}

func TestFileStorageStoreAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewFileStorage(tmpDir)
	require.NoError(t, err)

	// Store some transactions
	for i := 0; i < 5; i++ {
		tx := APITransaction{
			Request: RequestData{
				Method: "GET",
				Path:   "/items",
			},
			Response: ResponseData{
				StatusCode: 200,
				Body:       []byte(`[{"id":1},{"id":2}]`),
			},
		}
		require.NoError(t, storage.Store(tx))
	}

	// Verify in-memory transactions
	assert.Len(t, storage.transactions, 5)

	// Close to finalize the file
	require.NoError(t, storage.Close())

	// Verify the file was written correctly
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	var sessionFile string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			sessionFile = filepath.Join(tmpDir, f.Name())
			break
		}
	}
	require.NotEmpty(t, sessionFile)

	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var transactions []APITransaction
	err = json.Unmarshal(data, &transactions)
	require.NoError(t, err)
	assert.Len(t, transactions, 5)
}

func TestFileStorageClear(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewFileStorage(tmpDir)
	require.NoError(t, err)

	// Store a transaction
	tx := APITransaction{
		Request:  RequestData{Method: "GET", Path: "/test"},
		Response: ResponseData{StatusCode: 200, Body: []byte(`{}`)},
	}
	require.NoError(t, storage.Store(tx))

	// Clear
	require.NoError(t, storage.Clear())

	// Verify cleared
	all, err := storage.GetAll()
	require.NoError(t, err)
	assert.Empty(t, all)

	// Store after Clear must fail cleanly (session file is gone)
	assert.Error(t, storage.Store(tx))

	_ = storage.Close()
}

func TestFileStorageCloseFlushesToDisk(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewFileStorage(tmpDir)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		tx := APITransaction{
			Request:  RequestData{Method: "GET", Path: "/flush"},
			Response: ResponseData{StatusCode: 200, Body: []byte(`{"n":` + strconv.Itoa(i) + `}`)},
		}
		require.NoError(t, storage.Store(tx))
	}

	// Close performs the final sync and finalizes the file
	require.NoError(t, storage.Close())

	// A second Close is a no-op
	require.NoError(t, storage.Close())

	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	var sessionFile string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			sessionFile = filepath.Join(tmpDir, f.Name())
		}
	}
	require.NotEmpty(t, sessionFile)

	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)
	var transactions []APITransaction
	require.NoError(t, json.Unmarshal(data, &transactions))
	assert.Len(t, transactions, 3)
}

func TestFileStorageFlushIntervalDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	// A zero interval disables the periodic flusher but Close still syncs
	storage, err := NewFileStorageWithFlushInterval(tmpDir, 0)
	require.NoError(t, err)

	tx := APITransaction{
		Request:  RequestData{Method: "GET", Path: "/no-flusher"},
		Response: ResponseData{StatusCode: 200, Body: []byte(`{}`)},
	}
	require.NoError(t, storage.Store(tx))
	require.NoError(t, storage.Close())

	data, err := os.ReadFile(storage.sessionFile)
	require.NoError(t, err)
	var transactions []APITransaction
	require.NoError(t, json.Unmarshal(data, &transactions))
	assert.Len(t, transactions, 1)
}

func TestFileStorageConcurrentWritesNotInterleaved(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewFileStorageWithFlushInterval(tmpDir, 0)
	require.NoError(t, err)

	const total = 500
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tx := APITransaction{
				Request:  RequestData{Method: "GET", Path: "/items"},
				Response: ResponseData{StatusCode: 200, Body: []byte(`{"id":` + strconv.Itoa(id) + `}`)},
			}
			if err := storage.Store(tx); err != nil {
				t.Errorf("Store error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	require.NoError(t, storage.Close())

	// The session file must parse as one valid array containing every
	// transaction exactly once — any interleaved or partial write would
	// make the file invalid JSON or drop entries.
	data, err := os.ReadFile(storage.sessionFile)
	require.NoError(t, err)
	var transactions []APITransaction
	require.NoError(t, json.Unmarshal(data, &transactions))
	assert.Len(t, transactions, total)

	seen := make(map[string]bool)
	for _, tx := range transactions {
		key := string(tx.Response.Body)
		require.False(t, seen[key], "duplicate transaction body %s", key)
		seen[key] = true
	}
}

func TestSanitizeHeaders(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer secret-token"},
		"Content-Type":  []string{"application/json"},
		"X-Api-Key":     []string{"my-api-key"},
		"Accept":        []string{"application/json"},
	}

	sanitized := sanitizeHeaders(headers)

	assert.Equal(t, []string{"__redacted__"}, sanitized["Authorization"])
	assert.Equal(t, []string{"__redacted__"}, sanitized["X-Api-Key"])
	assert.Equal(t, []string{"application/json"}, sanitized["Content-Type"])
	assert.Equal(t, []string{"application/json"}, sanitized["Accept"])
}

func TestSanitizeQueryParams(t *testing.T) {
	params := map[string][]string{
		"page":   {"1"},
		"limit":  {"10"},
		"token":  {"secret"},
		"active": {"true"},
		"price":  {"9.99"},
	}

	sanitized := sanitizeQueryParams(params)

	assert.Equal(t, []string{"__redacted__"}, sanitized["token"])
	assert.Equal(t, []string{"__integer__"}, sanitized["page"])
	assert.Equal(t, []string{"__integer__"}, sanitized["limit"])
	assert.Equal(t, []string{"__boolean__"}, sanitized["active"])
	assert.Equal(t, []string{"__number__"}, sanitized["price"])
}

func TestSanitizeQueryParamValue(t *testing.T) {
	assert.Equal(t, "__boolean__", sanitizeQueryParamValue("true"))
	assert.Equal(t, "__boolean__", sanitizeQueryParamValue("false"))
	assert.Equal(t, "__integer__", sanitizeQueryParamValue("42"))
	assert.Equal(t, "__integer__", sanitizeQueryParamValue("-5"))
	assert.Equal(t, "__number__", sanitizeQueryParamValue("3.14"))
	assert.Equal(t, "__string__", sanitizeQueryParamValue("hello"))
	assert.Equal(t, "__string__", sanitizeQueryParamValue(""))
}

func TestSanitizeJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
		empty bool
	}{
		{"empty", "", true, true},
		{"object", `{"name":"Alice","age":30}`, true, false},
		{"array", `[1,2,3]`, true, false},
		{"nested", `{"users":[{"id":1,"name":"Bob"}]}`, true, false},
		{"invalid", "not json", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sanitizeJSON([]byte(tt.input))
			if tt.valid {
				assert.NoError(t, err)
				if tt.empty {
					assert.Empty(t, result)
				} else {
					// Verify result is valid JSON
					var v interface{}
					assert.NoError(t, json.Unmarshal(result, &v))
				}
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestResponseWriterTruncatesAtLimit(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec, 10)

	body := []byte("0123456789ABCDEF")
	n, err := rw.Write(body)
	require.NoError(t, err)
	assert.Equal(t, len(body), n)

	// The full body is forwarded to the client
	assert.Equal(t, string(body), rec.Body.String())
	// Only the first 10 bytes are captured
	assert.Equal(t, "0123456789", rw.body.String())
	assert.True(t, rw.Truncated())

	// Further writes do not grow the captured body
	_, _ = rw.Write([]byte("GH"))
	assert.Equal(t, "0123456789", rw.body.String())
	assert.True(t, rw.Truncated())

	// Split writes: the marker is set as soon as the limit is crossed
	rec2 := httptest.NewRecorder()
	rw2 := newResponseWriter(rec2, 10)
	_, _ = rw2.Write([]byte("0123456789")) // exactly at the limit
	assert.False(t, rw2.Truncated())
	_, _ = rw2.Write([]byte("X"))
	assert.True(t, rw2.Truncated())
	assert.Equal(t, "0123456789", rw2.body.String())
}

func TestResponseWriterNoTruncation(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec, 100)
	_, _ = rw.Write([]byte("short"))
	assert.Equal(t, "short", rw.body.String())
	assert.False(t, rw.Truncated())
}

func TestResponseWriterUnlimited(t *testing.T) {
	rw := newResponseWriter(httptest.NewRecorder(), 0)
	_, _ = rw.Write([]byte("anything at all"))
	assert.Equal(t, "anything at all", rw.body.String())
	assert.False(t, rw.Truncated())
}

func TestCapturedTransactionTruncationMarker(t *testing.T) {
	// Exercise the capture path exactly as the proxy handler does.
	reqData, err := captureRequest(httptest.NewRequest("GET", "/big", nil))
	require.NoError(t, err)

	rw := newResponseWriter(httptest.NewRecorder(), 4)
	_, _ = rw.Write([]byte(`{"data":"very large payload"}`))

	respData := captureResponse(rw)
	tx := APITransaction{
		Request:           reqData,
		Response:          respData,
		ResponseTruncated: rw.Truncated(),
	}

	assert.True(t, tx.ResponseTruncated)
	assert.Equal(t, `{"da`, string(tx.Response.Body))

	// The marker survives JSON round-tripping (session file storage)
	data, err := json.Marshal(tx)
	require.NoError(t, err)
	var decoded APITransaction
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.True(t, decoded.ResponseTruncated)

	// Untruncated transactions omit the field entirely
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "response_truncated")

	tx2 := APITransaction{Request: reqData, Response: respData}
	data2, err := json.Marshal(tx2)
	require.NoError(t, err)
	assert.NotContains(t, string(data2), "response_truncated")
}

func TestFileStorageReaderNoSideEffects(t *testing.T) {
	tmpDir := t.TempDir()

	// Prepare a data directory with a session file
	writer, err := NewFileStorage(tmpDir)
	require.NoError(t, err)
	tx := APITransaction{
		Request:  RequestData{Method: "GET", Path: "/read"},
		Response: ResponseData{StatusCode: 200, Body: []byte(`{"ok":true}`)},
	}
	require.NoError(t, writer.Store(tx))
	require.NoError(t, writer.Close())

	before, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	// The reader must not create any file
	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	after, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after), "reader created or removed files")

	// And it still reads the captured transactions
	all, err := reader.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, "/read", all[0].Request.Path)

	// File contents are byte-for-byte unchanged
	for i, entry := range before {
		b1, err := os.ReadFile(filepath.Join(tmpDir, entry.Name()))
		require.NoError(t, err)
		b2, err := os.ReadFile(filepath.Join(tmpDir, after[i].Name()))
		require.NoError(t, err)
		assert.Equal(t, b1, b2)
	}
}

func TestFileStorageReaderIsReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)

	tx := APITransaction{
		Request:  RequestData{Method: "GET", Path: "/x"},
		Response: ResponseData{StatusCode: 200},
	}
	assert.Error(t, reader.Store(tx))
	assert.Error(t, reader.Clear())
	// Close on a reader is a no-op
	assert.NoError(t, reader.Close())

	// No session file was ever created
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestFileStorageGetAllMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewFileStorage(tmpDir)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		tx := APITransaction{
			Request:  RequestData{Method: "GET", Path: "/first"},
			Response: ResponseData{StatusCode: 200},
		}
		require.NoError(t, writer.Store(tx))
	}
	require.NoError(t, writer.Close())

	// A second session file from a separate writer process
	writer2, err := NewFileStorage(tmpDir)
	require.NoError(t, err)
	tx := APITransaction{
		Request:  RequestData{Method: "POST", Path: "/second"},
		Response: ResponseData{StatusCode: 201},
	}
	require.NoError(t, writer2.Store(tx))
	require.NoError(t, writer2.Close())

	// A legacy single-transaction file must still be readable
	legacy := APITransaction{
		Request:  RequestData{Method: "PUT", Path: "/legacy"},
		Response: ResponseData{StatusCode: 200},
	}
	legacyData, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "legacy.json"), legacyData, 0o644))

	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	all, err := reader.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, 5)

	paths := map[string]int{}
	for _, tx := range all {
		paths[tx.Request.Path]++
	}
	assert.Equal(t, 3, paths["/first"])
	assert.Equal(t, 1, paths["/second"])
	assert.Equal(t, 1, paths["/legacy"])
}
