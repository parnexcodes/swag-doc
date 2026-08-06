## Why

WebSocket API documentation is an explicitly listed future feature in the README, and today SwagDoc produces nothing useful for socket-based APIs: the proxy passes WebSocket connections through transparently (`responseWriter` implements `http.Hijacker`), but the 101 handshake is written over the hijacked connection by `httputil.ReverseProxy` and never reaches capture — the endpoint is recorded as a misleading 200 OK transaction with an empty body, and OpenAPI 3.0 has no native WebSocket model to document it with anyway. Teams running SwagDoc in front of socket-heavy APIs get zero documentation for that traffic.

## What Changes

- The proxy captures WebSocket connections in addition to HTTP transactions: handshake metadata (path, query params, headers, subprotocols, response status) plus the messages exchanged in each direction (text/binary frames, direction, payload), with bounded capture limits (per-message bytes, per-session message count) and frame-level handling of RFC 6455 (masking, fragmentation, ping/pong/close).
- A new `WebSocketSession` record type is persisted in `ws-session-*.json` files in the data directory, kept separate from the existing HTTP `session-*.json` files. OpenAPI generation MUST ignore `ws-session-*.json` files so existing OpenAPI output stays byte-identical.
- A new AsyncAPI generator (`pkg/asyncapi`) emits an AsyncAPI 3.1.0 document (the latest spec release, January 2026) from captured WebSocket sessions: one channel per templated endpoint path (reusing the existing path-pattern inference for numeric IDs/UUIDs), operations with `action: receive`/`send` inferred from observed frame directions, payload schemas inferred by the existing schema parser, and security schemes reused from the existing auth detection. JSON/YAML selection reuses the existing `--output` extension behavior.
- `swagdoc generate --format asyncapi` selects the AsyncAPI generator; `--format openapi` is the default and current invocations behave exactly as before.
- README updated: WebSocket documentation documented in the generate command section and removed from Future Features.

No breaking changes. No new dependencies.

## Capabilities

### New Capabilities

- `proxy/websocket-capture`: Handshake and message-frame capture for WebSocket connections, with bounded memory and separate on-disk storage, while the live connection is still forwarded untouched.
- `generation/asyncapi`: AsyncAPI 3.1.0 document generation from captured WebSocket sessions, including channel templating, operation and message inference, payload schemas, and JSON/YAML serialization.

### Modified Capabilities

None.
