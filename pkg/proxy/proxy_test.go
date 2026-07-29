package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		rw := newResponseWriter(w)

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

	_ = storage.Close()
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
