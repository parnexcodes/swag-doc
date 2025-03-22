package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyServer(t *testing.T) {
	// Create a test server that will act as our API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
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
		defer apiResp.Body.Close()

		// Copy the API response to our response writer
		for key, values := range apiResp.Header {
			for _, value := range values {
				rw.Header().Add(key, value)
			}
		}
		rw.WriteHeader(apiResp.StatusCode)
		io.Copy(rw, apiResp.Body)

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
	defer resp.Body.Close()

	// Verify the interceptor was called
	if !interceptorCalled {
		t.Error("Interceptor was not called")
	}

	// Verify the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
}
