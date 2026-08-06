// Package asyncapi generates AsyncAPI 3.1.0 documents from captured
// WebSocket sessions.
package asyncapi

// AsyncAPIVersion is the AsyncAPI specification version emitted by the
// generator.
const AsyncAPIVersion = "3.1.0"

// Document is an AsyncAPI 3.1.0 document. The JSON tags are the source of
// truth for both JSON and YAML serialization.
//
// The 3.x model separates channels (addressable components) from operations
// (what the documented application does): channels declare the messages
// exchanged on an address, and a top-level operations map references them
// with an action of "send" or "receive".
type Document struct {
	AsyncAPI   string               `json:"asyncapi"`
	Info       Info                 `json:"info"`
	Servers    map[string]Server    `json:"servers,omitempty"`
	Channels   map[string]Channel   `json:"channels"`
	Operations map[string]Operation `json:"operations"`
	Components Components           `json:"components,omitempty"`
}

// Info carries document metadata.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Server describes a WebSocket server endpoint.
type Server struct {
	URL             string `json:"url"`
	Protocol        string `json:"protocol"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
}

// Ref is a $ref pointer object.
type Ref struct {
	Ref string `json:"$ref"`
}

// Channel describes one WebSocket endpoint: an addressable component with
// the messages that can be exchanged on it.
type Channel struct {
	Address    string                      `json:"address,omitempty"`
	Messages   map[string]Ref              `json:"messages,omitempty"`
	Parameters map[string]ChannelParameter `json:"parameters,omitempty"`
}

// ChannelParameter describes a path parameter of a channel address.
type ChannelParameter struct {
	Schema map[string]any `json:"schema"`
}

// Operation is an operation the documented application MUST implement.
// Action is "send" (the application sends to the channel) or "receive" (the
// application receives from the channel).
type Operation struct {
	Action   string `json:"action"`
	Channel  Ref    `json:"channel"`
	Messages []Ref  `json:"messages,omitempty"`
	Security []Ref  `json:"security,omitempty"`
}

// Message describes one message type exchanged on a channel.
type Message struct {
	Name        string         `json:"name"`
	ContentType string         `json:"contentType,omitempty"`
	Payload     map[string]any `json:"payload"`
}

// Components holds reusable schemas, messages, and security schemes.
type Components struct {
	Schemas         map[string]map[string]any `json:"schemas,omitempty"`
	Messages        map[string]Message        `json:"messages,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme is an AsyncAPI security scheme.
type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	In           string `json:"in,omitempty"`
	Name         string `json:"name,omitempty"`
}
