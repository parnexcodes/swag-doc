package asyncapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/proxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func testSession(path, auth string, msgs ...proxy.WebSocketMessage) proxy.WebSocketSession {
	headers := http.Header{}
	if auth != "" {
		headers.Set("Authorization", auth)
	}
	return proxy.WebSocketSession{
		HandshakeRequest: proxy.RequestData{Method: "GET", Path: path, Headers: headers},
		ResponseStatus:   101,
		Messages:         msgs,
	}
}

func c2s(payload string) proxy.WebSocketMessage {
	return proxy.WebSocketMessage{Direction: proxy.DirectionClientToServer, Opcode: proxy.WSOpcodeText, Payload: []byte(payload)}
}

func s2c(payload string) proxy.WebSocketMessage {
	return proxy.WebSocketMessage{Direction: proxy.DirectionServerToClient, Opcode: proxy.WSOpcodeText, Payload: []byte(payload)}
}

func TestGenerateChannelsAndOperationsMapping(t *testing.T) {
	g := NewGenerator(Config{Title: "Chat API", Version: "1.0.0", BasePath: "http://localhost:3000"})
	// Repeated sessions against numeric IDs must merge into one channel.
	g.AddSession(testSession("/chat/123", "",
		c2s(`{"msg":"hi"}`),
		s2c(`{"reply":"yo"}`),
	))
	g.AddSession(testSession("/chat/456", "",
		c2s(`{"msg":"hello"}`),
		s2c(`{"reply":"hey"}`),
	))

	doc, err := g.Generate()
	require.NoError(t, err)

	assert.Equal(t, AsyncAPIVersion, doc.AsyncAPI)
	assert.Equal(t, "Chat API", doc.Info.Title)

	require.Len(t, doc.Channels, 1, "numeric-ID sessions merge into one channel")
	channel, ok := doc.Channels["/chat/{id}"]
	require.True(t, ok, "channel key is the templated path")
	assert.Equal(t, "/chat/{id}", channel.Address)
	assert.Contains(t, channel.Parameters, "id", "templated address declares its parameter")

	// Client messages become a receive operation.
	receive, ok := doc.Operations["receiveChatId"]
	require.True(t, ok)
	assert.Equal(t, "receive", receive.Action)
	assert.Equal(t, "#/channels/chat/{id}", receive.Channel.Ref)
	assert.Equal(t, []Ref{{Ref: "#/components/messages/ChatIdClientMessage"}}, receive.Messages)

	// Server messages become a send operation.
	send, ok := doc.Operations["sendChatId"]
	require.True(t, ok)
	assert.Equal(t, "send", send.Action)
	assert.Equal(t, "#/channels/chat/{id}", send.Channel.Ref)
	assert.Equal(t, []Ref{{Ref: "#/components/messages/ChatIdServerMessage"}}, send.Messages)

	// The channel lists its messages; the message objects live in components
	// and reference typed payload schemas merged from both observations.
	assert.Equal(t, map[string]Ref{
		"ChatIdClientMessage": {Ref: "#/components/messages/ChatIdClientMessage"},
		"ChatIdServerMessage": {Ref: "#/components/messages/ChatIdServerMessage"},
	}, channel.Messages)

	clientMsg, ok := doc.Components.Messages["ChatIdClientMessage"]
	require.True(t, ok)
	assert.Equal(t, "ChatIdClientMessage", clientMsg.Name)
	assert.Equal(t, "application/json", clientMsg.ContentType)
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/ChatIdClientMessagePayload"}, clientMsg.Payload)

	clientSchema, ok := doc.Components.Schemas["ChatIdClientMessagePayload"]
	require.True(t, ok)
	assert.Equal(t, "object", clientSchema["type"])
	props := clientSchema["properties"].(map[string]any)
	assert.Equal(t, "string", props["msg"].(map[string]any)["type"])
}

func TestGenerateDistinctEndpointsProduceDistinctChannels(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g.AddSession(testSession("/chat/123", "", c2s(`{"msg":"hi"}`)))
	g.AddSession(testSession("/notifications", "", s2c(`{"n":1}`)))

	doc, err := g.Generate()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/chat/{id}", "/notifications"}, keysOf(doc.Channels))
	assert.ElementsMatch(t, []string{"receiveChatId", "sendNotifications"}, keysOf(doc.Operations))
}

func TestGenerateMultipleMessageTypesPerDirection(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g.AddSession(testSession("/events", "",
		c2s(`{"kind":"a","n":1}`),
		c2s(`{"kind":"b","flag":true}`),
		s2c(`{"ack":true}`),
	))

	doc, err := g.Generate()
	require.NoError(t, err)

	// Two distinct client schemas: two messages on the receive operation.
	receive := doc.Operations["receiveEvents"]
	require.NotNil(t, receive)
	assert.Equal(t, "receive", receive.Action)
	assert.ElementsMatch(t, []Ref{
		{Ref: "#/components/messages/EventsClientMessage"},
		{Ref: "#/components/messages/EventsClientMessage-2"},
	}, receive.Messages)
	assert.Contains(t, doc.Components.Messages, "EventsClientMessage")
	assert.Contains(t, doc.Components.Messages, "EventsClientMessage-2")
	assert.Contains(t, doc.Components.Schemas, "EventsClientMessagePayload")
	assert.Contains(t, doc.Components.Schemas, "EventsClientMessage-2Payload")

	// The identical server schema yields a single send message.
	send := doc.Operations["sendEvents"]
	require.NotNil(t, send)
	assert.Equal(t, "send", send.Action)
	assert.Equal(t, []Ref{{Ref: "#/components/messages/EventsServerMessage"}}, send.Messages)
}

func TestGenerateIsDeterministic(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g.AddSession(testSession("/chat/123", "", c2s(`{"msg":"hi"}`), s2c(`{"reply":"yo"}`)))
	first, err := g.Generate()
	require.NoError(t, err)

	g2 := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g2.AddSession(testSession("/chat/123", "", c2s(`{"msg":"hi"}`), s2c(`{"reply":"yo"}`)))
	second, err := g2.Generate()
	require.NoError(t, err)

	firstJSON, err := Marshal(first, FormatJSON)
	require.NoError(t, err)
	secondJSON, err := Marshal(second, FormatJSON)
	require.NoError(t, err)
	assert.Equal(t, firstJSON, secondJSON, "same input must produce identical output")
}

func TestGenerateSecuritySchemes(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g.AddSession(testSession("/secure", "Bearer abc123", c2s(`{"msg":"hi"}`), s2c(`{"ok":true}`)))
	g.AddSession(testSession("/open", "", c2s(`{"msg":"hi"}`)))

	doc, err := g.Generate()
	require.NoError(t, err)

	scheme, ok := doc.Components.SecuritySchemes["bearerAuth"]
	require.True(t, ok)
	assert.Equal(t, "http", scheme.Type)
	assert.Equal(t, "bearer", scheme.Scheme)

	secureOp := doc.Operations["receiveSecure"]
	require.NotNil(t, secureOp)
	assert.Equal(t, []Ref{{Ref: "#/components/securitySchemes/bearerAuth"}}, secureOp.Security)
	assert.Equal(t, []Ref{{Ref: "#/components/securitySchemes/bearerAuth"}}, doc.Operations["sendSecure"].Security,
		"both operations of an authenticated channel carry the scheme")

	openOp := doc.Operations["receiveOpen"]
	require.NotNil(t, openOp)
	assert.Empty(t, openOp.Security, "unauthenticated channel has no security requirement")
}

func TestGenerateServerProtocolMapping(t *testing.T) {
	for _, tc := range []struct {
		basePath  string
		wantURL   string
		wantProto string
	}{
		{"http://localhost:3000", "ws://localhost:3000", "ws"},
		{"https://api.example.com", "wss://api.example.com", "wss"},
		{"wss://api.example.com", "wss://api.example.com", "wss"},
	} {
		g := NewGenerator(Config{Title: "API", Version: "1.0.0", BasePath: tc.basePath})
		doc, err := g.Generate()
		require.NoError(t, err)
		server, ok := doc.Servers["default"]
		require.True(t, ok)
		assert.Equal(t, tc.wantURL, server.URL)
		assert.Equal(t, tc.wantProto, server.Protocol)
		assert.Equal(t, "13", server.ProtocolVersion)
	}
}

func TestGenerateBinaryAndPlainTextPayloads(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	g.AddSession(testSession("/bin", "",
		proxy.WebSocketMessage{Direction: proxy.DirectionClientToServer, Opcode: proxy.WSOpcodeBinary, Payload: []byte{0x01, 0x02}},
	))
	g.AddSession(testSession("/plain", "",
		c2s("just some text"),
	))

	doc, err := g.Generate()
	require.NoError(t, err)

	binMsg := doc.Components.Messages["BinClientMessage"]
	assert.Equal(t, "application/octet-stream", binMsg.ContentType)
	binSchema := doc.Components.Schemas["BinClientMessagePayload"]
	assert.Equal(t, "string", binSchema["type"])
	assert.Equal(t, "binary", binSchema["format"])

	plainMsg := doc.Components.Messages["PlainClientMessage"]
	assert.Equal(t, "text/plain", plainMsg.ContentType)
	plainSchema := doc.Components.Schemas["PlainClientMessagePayload"]
	assert.Equal(t, "string", plainSchema["type"])
	assert.NotContains(t, plainSchema, "format")
}

func TestGenerateYAMLMatch(t *testing.T) {
	g := NewGenerator(Config{Title: "API", Version: "1.0.0", BasePath: "http://localhost:3000"})
	g.AddSession(testSession("/chat/1", "", c2s(`{"msg":"hi"}`)))
	doc, err := g.Generate()
	require.NoError(t, err)

	yamlData, err := Marshal(doc, FormatYAML)
	require.NoError(t, err)
	var yamlDoc map[string]any
	require.NoError(t, yaml.Unmarshal(yamlData, &yamlDoc))
	assert.Equal(t, AsyncAPIVersion, yamlDoc["asyncapi"])
	assert.Contains(t, yamlDoc, "channels")
	assert.Contains(t, yamlDoc, "operations")

	jsonData, err := Marshal(doc, FormatJSON)
	require.NoError(t, err)
	var jsonDoc map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &jsonDoc))
	assert.Equal(t, yamlDoc, jsonDoc, "YAML and JSON describe the same document")
}

func TestLoadWebSocketSessionsFromDirectory(t *testing.T) {
	dir := t.TempDir()

	// A ws-session file and an HTTP session file in the same directory.
	wsData, err := json.Marshal([]proxy.WebSocketSession{testSession("/ws/1", "", c2s(`{"m":1}`))})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ws-session-test.json"), wsData, 0o644))

	httpData, err := json.Marshal([]proxy.APITransaction{{
		Request: proxy.RequestData{Method: "GET", Path: "/users"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session-test.json"), httpData, 0o644))

	g := NewGenerator(Config{Title: "API", Version: "1.0.0"})
	require.NoError(t, g.LoadWebSocketSessionsFromDirectory(dir))
	require.Len(t, g.sessions, 1, "HTTP session files are ignored")
	assert.Equal(t, "/ws/1", g.sessions[0].HandshakeRequest.Path)
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
