package asyncapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/parser"
	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// Config configures the AsyncAPI generator.
type Config struct {
	Title       string
	Description string
	Version     string
	BasePath    string
}

// Generator generates AsyncAPI documents from captured WebSocket sessions.
type Generator struct {
	config   Config
	sessions []proxy.WebSocketSession
}

// NewGenerator creates a generator with the given configuration.
func NewGenerator(config Config) *Generator {
	return &Generator{config: config}
}

// AddSession adds a captured WebSocket session to be analyzed.
func (g *Generator) AddSession(session proxy.WebSocketSession) {
	g.sessions = append(g.sessions, session)
}

// LoadWebSocketSessionsFromFile loads sessions from a ws-session JSON file
// (either an array of sessions or a single session object).
func (g *Generator) LoadWebSocketSessionsFromFile(filename string) error {
	sessions, err := proxy.ReadWebSocketSessionsFromFile(filename)
	if err != nil {
		return err
	}
	g.sessions = append(g.sessions, sessions...)
	return nil
}

// LoadWebSocketSessionsFromDirectory loads sessions from all ws-session-*.json
// files in a directory. HTTP session files are ignored.
func (g *Generator) LoadWebSocketSessionsFromDirectory(dirPath string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".json") || !strings.HasPrefix(name, "ws-session-") {
			continue
		}
		if err := g.LoadWebSocketSessionsFromFile(filepath.Join(dirPath, name)); err != nil {
			return err
		}
	}
	return nil
}

// payloadSample is one observed message payload of a channel.
type payloadSample struct {
	schema   parser.Schema
	isJSON   bool
	isBinary bool
}

// channelState accumulates the observations for one templated channel path.
type channelState struct {
	path     string
	sessions []proxy.WebSocketSession
	samples  map[string][]payloadSample // direction -> samples in capture order
}

// Generate produces an AsyncAPI document from the collected sessions.
func (g *Generator) Generate() (*Document, error) {
	doc := &Document{
		AsyncAPI: AsyncAPIVersion,
		Info: Info{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
		},
		Channels:   map[string]Channel{},
		Operations: map[string]Operation{},
		Components: Components{
			Schemas:         map[string]map[string]any{},
			Messages:        map[string]Message{},
			SecuritySchemes: map[string]SecurityScheme{},
		},
	}

	if g.config.BasePath != "" {
		serverURL, protocol := wsServerURL(g.config.BasePath)
		doc.Servers = map[string]Server{
			"default": {URL: serverURL, Protocol: protocol, ProtocolVersion: "13"},
		}
	}

	// First pass: infer path patterns across all sessions.
	pathDetector := parser.NewPathPatternDetector()
	for _, session := range g.sessions {
		pathDetector.AddPath(session.HandshakeRequest.Path)
	}
	pathDetector.AnalyzePatterns()

	// Group sessions by templated channel address, preserving first-seen order.
	channels := map[string]*channelState{}
	var channelOrder []string
	for _, session := range g.sessions {
		templated := pathDetector.TemplatizePath(session.HandshakeRequest.Path)
		if templated == "" {
			templated = session.HandshakeRequest.Path
		}
		state, ok := channels[templated]
		if !ok {
			state = &channelState{
				path:    templated,
				samples: map[string][]payloadSample{},
			}
			channels[templated] = state
			channelOrder = append(channelOrder, templated)
		}
		state.sessions = append(state.sessions, session)
	}

	// Build each channel and its operations from its sessions.
	for _, path := range channelOrder {
		state := channels[path]
		channel, receive, send := g.buildChannel(state, doc)
		doc.Channels[path] = channel
		tag := channelTag(path)
		if receive != nil {
			doc.Operations["receive"+tag] = *receive
		}
		if send != nil {
			doc.Operations["send"+tag] = *send
		}
	}

	if len(doc.Components.Schemas) == 0 {
		doc.Components.Schemas = nil
	}
	if len(doc.Components.Messages) == 0 {
		doc.Components.Messages = nil
	}
	if len(doc.Components.SecuritySchemes) == 0 {
		doc.Components.SecuritySchemes = nil
	}
	return doc, nil
}

// buildChannel derives the channel object and its two operations from the
// observed sessions. Messages sent by the client become a "receive"
// operation (the documented application receives them); messages sent by the
// server become a "send" operation. Operations are nil when their direction
// has no captured messages.
func (g *Generator) buildChannel(state *channelState, doc *Document) (Channel, *Operation, *Operation) {
	tag := channelTag(state.path)
	channel := Channel{
		Address:  state.path,
		Messages: map[string]Ref{},
	}

	// Path parameters for templated address segments.
	if params := channelParameters(state.path); len(params) > 0 {
		channel.Parameters = params
	}

	// Collect payload samples per direction, in capture order.
	for _, session := range state.sessions {
		for _, msg := range session.Messages {
			sample := payloadSample{isBinary: msg.Opcode == proxy.WSOpcodeBinary}
			if schema, ok := payloadToSchema(msg.Payload); ok {
				sample.schema = schema
				sample.isJSON = true
			} else if msg.Opcode == proxy.WSOpcodeBinary {
				sample.schema = parser.Schema{Type: "string", Format: "binary"}
			} else {
				sample.schema = parser.Schema{Type: "string"}
			}
			state.samples[msg.Direction] = append(state.samples[msg.Direction], sample)
		}
	}

	// Security schemes detected on this channel's handshakes apply to both
	// of the channel's operations (3.x moved security off channels).
	security := g.channelSecurity(state.sessions, doc.Components.SecuritySchemes)

	var receive, send *Operation
	if ids := g.buildMessages(state.samples[proxy.DirectionClientToServer], "ClientMessage", tag, doc); len(ids) > 0 {
		receive = &Operation{
			Action:   "receive",
			Channel:  Ref{Ref: "#/channels" + state.path},
			Messages: messageRefs(ids),
			Security: security,
		}
		registerChannelMessages(&channel, ids)
	}
	if ids := g.buildMessages(state.samples[proxy.DirectionServerToClient], "ServerMessage", tag, doc); len(ids) > 0 {
		send = &Operation{
			Action:   "send",
			Channel:  Ref{Ref: "#/channels" + state.path},
			Messages: messageRefs(ids),
			Security: security,
		}
		registerChannelMessages(&channel, ids)
	}
	return channel, receive, send
}

// registerChannelMessages adds the message references to the channel's
// messages map.
func registerChannelMessages(channel *Channel, ids []string) {
	for _, id := range ids {
		channel.Messages[id] = Ref{Ref: "#/components/messages/" + id}
	}
}

func messageRefs(ids []string) []Ref {
	refs := make([]Ref, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, Ref{Ref: "#/components/messages/" + id})
	}
	return refs
}

// buildMessages groups samples by schema equality (first-seen order) into
// messages named <tag><base>, <tag><base>-2, ... and registers each message
// and its payload schema in components. It returns the registered message
// ids in capture order, or nil when there are no samples.
func (g *Generator) buildMessages(samples []payloadSample, base, tag string, doc *Document) []string {
	if len(samples) == 0 {
		return nil
	}

	var groups [][]payloadSample
	for _, sample := range samples {
		placed := false
		for i := range groups {
			if parser.AreSchemasEqual(groups[i][0].schema, sample.schema) {
				groups[i] = append(groups[i], sample)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, []payloadSample{sample})
		}
	}

	// Group members are schema-equal, so any member's schema represents the
	// group; registering one message per group keeps names deterministic.
	ids := make([]string, 0, len(groups))
	for i, group := range groups {
		id := tag + base
		if i > 0 {
			id += "-" + strconv.Itoa(i+1)
		}
		doc.Components.Schemas[id+"Payload"] = schemaToMap(group[0].schema)
		doc.Components.Messages[id] = Message{
			Name:        id,
			ContentType: messageContentType(group),
			Payload:     map[string]any{"$ref": "#/components/schemas/" + id + "Payload"},
		}
		ids = append(ids, id)
	}
	return ids
}

// messageContentType derives the content type of one message group:
// application/json for JSON payloads, application/octet-stream for binary
// payloads, and text/plain for plain text.
func messageContentType(group []payloadSample) string {
	anyJSON := false
	allBinary := true
	for _, sample := range group {
		if sample.isJSON {
			anyJSON = true
		}
		if !sample.isBinary {
			allBinary = false
		}
	}
	switch {
	case anyJSON:
		return "application/json"
	case allBinary:
		return "application/octet-stream"
	default:
		return "text/plain"
	}
}

// channelSecurity detects authentication on the channel's handshakes and
// registers schemes in components, returning the operation security refs.
func (g *Generator) channelSecurity(sessions []proxy.WebSocketSession, schemes map[string]SecurityScheme) []Ref {
	detector := parser.NewAuthDetector()
	for _, session := range sessions {
		reqHeader := make(http.Header)
		for k, v := range session.HandshakeRequest.Headers {
			if len(v) > 0 {
				reqHeader.Set(k, v[0])
			}
		}
		reqURL, _ := url.Parse(session.HandshakeRequest.Path)
		if session.HandshakeRequest.QueryParams != nil {
			reqURL.RawQuery = session.HandshakeRequest.QueryParams.Encode()
		}
		respHeader := make(http.Header)
		for k, v := range session.ResponseHeaders {
			if len(v) > 0 {
				respHeader.Set(k, v[0])
			}
		}
		detector.AnalyzeTransaction(&http.Request{Header: reqHeader, URL: reqURL}, &http.Response{Header: respHeader})
	}

	var refs []Ref
	for _, scheme := range detector.GetAuthSchemes() {
		name := securitySchemeName(scheme)
		if _, exists := schemes[name]; !exists {
			schemes[name] = toSecurityScheme(scheme)
		}
		refs = append(refs, Ref{Ref: "#/components/securitySchemes/" + name})
	}
	return refs
}

func toSecurityScheme(scheme parser.AuthScheme) SecurityScheme {
	switch scheme.Type {
	case "apiKey":
		return SecurityScheme{Type: "apiKey", In: scheme.In, Name: scheme.Name}
	default:
		return SecurityScheme{Type: "http", Scheme: scheme.Scheme, BearerFormat: scheme.Format}
	}
}

func securitySchemeName(scheme parser.AuthScheme) string {
	switch scheme.Type {
	case "http":
		if scheme.Scheme == "" {
			return "httpAuth"
		}
		return scheme.Scheme + "Auth"
	case "apiKey":
		return "apiKey" + capitalize(scheme.In) + sanitizeIdentifier(scheme.Name)
	default:
		return sanitizeIdentifier(scheme.Type) + "Auth"
	}
}

// payloadToSchema parses a JSON payload into a schema. It reports false for
// non-JSON payloads.
func payloadToSchema(data []byte) (parser.Schema, bool) {
	var obj any
	if err := json.Unmarshal(data, &obj); err != nil {
		return parser.Schema{}, false
	}
	return schemaFromValue(obj), true
}

func schemaFromValue(v any) parser.Schema {
	switch val := v.(type) {
	case map[string]any:
		props := make(map[string]parser.Schema, len(val))
		for k, item := range val {
			props[k] = schemaFromValue(item)
		}
		return parser.Schema{Type: "object", Properties: props}
	case []any:
		var items *parser.Schema
		if len(val) > 0 {
			s := schemaFromValue(val[0])
			items = &s
		}
		return parser.Schema{Type: "array", Items: items}
	case string:
		return parser.Schema{Type: "string"}
	case float64:
		if val == float64(int64(val)) {
			return parser.Schema{Type: "integer", Format: "int64"}
		}
		return parser.Schema{Type: "number", Format: "double"}
	case bool:
		return parser.Schema{Type: "boolean"}
	default:
		return parser.Schema{Type: "string"}
	}
}

// schemaToMap converts a parser.Schema into a JSON-Schema-style map for
// AsyncAPI payloads. Examples are intentionally omitted: captured payloads
// are stored raw and may contain sensitive values.
func schemaToMap(s parser.Schema) map[string]any {
	m := make(map[string]any)
	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Nullable {
		m["nullable"] = true
	}
	if s.Items != nil {
		m["items"] = schemaToMap(*s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}
	return m
}

// channelParameters derives AsyncAPI path parameters from a templated
// channel address.
func channelParameters(path string) map[string]ChannelParameter {
	params := map[string]ChannelParameter{}
	for _, segment := range strings.Split(path, "/") {
		if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
			continue
		}
		name := segment[1 : len(segment)-1]
		schema := map[string]any{"type": "string"}
		if _, err := strconv.Atoi(name); err == nil {
			schema["type"] = "integer"
		}
		params[name] = ChannelParameter{Schema: schema}
	}
	return params
}

// channelTag derives a document-unique identifier prefix from a channel
// address for message and schema naming, e.g. "/chat/{id}" -> "ChatId".
// The whole address is used so channels sharing a first segment do not
// collide (e.g. "/chat/{id}" and "/chat/archive").
func channelTag(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "Root"
	}
	var b strings.Builder
	for _, segment := range strings.Split(trimmed, "/") {
		if seg := sanitizeIdentifier(segment); seg != "" {
			b.WriteString(capitalize(seg))
		}
	}
	if b.Len() == 0 {
		return "Root"
	}
	return b.String()
}

// wsServerURL converts a base path URL to its WebSocket form, returning the
// URL and the protocol.
func wsServerURL(basePath string) (string, string) {
	u, err := url.Parse(basePath)
	if err != nil || u.Scheme == "" {
		return basePath, "ws"
	}
	protocol := "ws"
	switch u.Scheme {
	case "https", "wss":
		protocol = "wss"
	}
	u.Scheme = protocol
	return u.String(), protocol
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
