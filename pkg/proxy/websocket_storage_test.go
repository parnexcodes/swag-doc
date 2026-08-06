package proxy

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorageWebSocketSessionRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir)
	require.NoError(t, err)

	session := WebSocketSession{
		HandshakeRequest: RequestData{
			Method:    "GET",
			Path:      "/chat/42",
			Headers:   http.Header{"Sec-WebSocket-Key": {"abc"}},
			Timestamp: time.Now(),
		},
		ResponseStatus:  http.StatusSwitchingProtocols,
		ResponseHeaders: http.Header{"Sec-WebSocket-Accept": {"xyz"}},
		Subprotocol:     "chat",
		Messages: []WebSocketMessage{
			{Direction: DirectionClientToServer, Opcode: WSOpcodeText, Payload: []byte(`{"text":"hi"}`), Timestamp: time.Now()},
			{Direction: DirectionServerToClient, Opcode: WSOpcodeText, Payload: []byte(`{"text":"yo"}`), Timestamp: time.Now()},
		},
	}
	require.NoError(t, storage.StoreWebSocketSession(session))

	// An HTTP transaction stored alongside must stay in its own file.
	tx := APITransaction{
		Request:  RequestData{Method: "GET", Path: "/users"},
		Response: ResponseData{StatusCode: 200},
	}
	require.NoError(t, storage.Store(tx))

	require.NoError(t, storage.Close())

	// A second Close is a no-op.
	require.NoError(t, storage.Close())

	// Read back through a fresh reader: only the WebSocket session.
	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	sessions, err := reader.GetAllWebSocketSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "/chat/42", sessions[0].HandshakeRequest.Path)
	assert.Equal(t, http.StatusSwitchingProtocols, sessions[0].ResponseStatus)
	assert.Equal(t, "chat", sessions[0].Subprotocol)
	require.Len(t, sessions[0].Messages, 2)
	assert.Equal(t, `{"text":"hi"}`, string(sessions[0].Messages[0].Payload), "payloads are stored raw")
	assert.Equal(t, DirectionServerToClient, sessions[0].Messages[1].Direction)

	// The HTTP transaction must be readable from the same directory and the
	// WebSocket session must not leak into GetAll.
	transactions, err := reader.GetAll()
	require.NoError(t, err)
	require.Len(t, transactions, 1)
	assert.Equal(t, "/users", transactions[0].Request.Path)

	// The ws file is a valid JSON array of sessions.
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	var wsFile string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), wsSessionPrefix) {
			wsFile = filepath.Join(tmpDir, f.Name())
		}
	}
	require.NotEmpty(t, wsFile)
	data, err := os.ReadFile(wsFile)
	require.NoError(t, err)
	var parsed []WebSocketSession
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Len(t, parsed, 1)
}

func TestFileStorageWebSocketSessionsFlushOnClose(t *testing.T) {
	tmpDir := t.TempDir()
	// Disable the periodic flusher: Close must still sync the ws file.
	storage, err := NewFileStorageWithFlushInterval(tmpDir, 0)
	require.NoError(t, err)

	require.NoError(t, storage.StoreWebSocketSession(WebSocketSession{
		HandshakeRequest: RequestData{Method: "GET", Path: "/ws"},
		ResponseStatus:   101,
	}))
	require.NoError(t, storage.Close())

	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	sessions, err := reader.GetAllWebSocketSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "/ws", sessions[0].HandshakeRequest.Path)
}

func TestFileStorageReaderRejectsWebSocketWrites(t *testing.T) {
	tmpDir := t.TempDir()
	reader, err := NewFileStorageReader(tmpDir)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	err = reader.StoreWebSocketSession(WebSocketSession{})
	assert.Error(t, err)
}
