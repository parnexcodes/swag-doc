## Context

Current state that shapes this design (motivation in `proposal.md`):

- `pkg/proxy.ProxyServer` delegates to `httputil.ReverseProxy` with a `responseWriter` wrapper that implements `http.Hijacker`. For upgrade requests Go's ReverseProxy hijacks the connection itself and writes the 101 response over the raw connection, so our wrapper never observes the handshake — capture records a default 200, empty-body transaction, and socket traffic is invisible.
- `FileStorage` appends JSON objects to one `session-<ts>-<pid>.json` array per process, with periodic `sync` and a final flush on close. `OpenAPIGenerator.LoadTransactionsFromDirectory` reads **every** `*.json` file in the data directory as `[]APITransaction`.
- Generation is multi-pass and kin-openapi based; path templating (`pkg/parser/path_patterns.go`), schema inference/merging (`pkg/parser`), and auth detection (`pkg/parser/auth.go`) already exist and are reusable. YAML output is produced by JSON round-trip + `yaml.v3` (see archived `add-yaml-output` change).
- CLI is cobra-based in `cmd/swagdoc/main.go`; `generateDocs` loads, generates, serializes.

## Goals / Non-Goals

**Goals:**
- A capture path that records WebSocket handshakes and messages without altering forwarded bytes.
- A standalone AsyncAPI generator that reuses existing inference (path templating, schema merging, auth detection) rather than duplicating it.
- Zero new dependencies; hand-rolled RFC 6455 frame parsing sized to the recording use case.
- Backward compatibility: default `--format openapi` output is byte-identical; OpenAPI generation ignores the new session files.

**Non-Goals:**
- permessage-deflate decoding of recorded payloads (compressed payloads are captured as-is and documented as binary).
- AsyncAPI 2.x output: 3.x is the current spec line, and the 2.x `subscribe`/`publish` channel shape is deliberately not emitted.
- Documenting messages beyond the wire payload (no replay, no client-side state tracking).

## Decisions

### D1: Intercept upgrades before ReverseProxy, drive the connection ourselves

In the proxy handler, detect `Connection: Upgrade` + `Upgrade: websocket` (case-insensitive) and route the request to a dedicated WebSocket path instead of `ReverseProxy.ServeHTTP`:

1. Forward the request with a per-proxy `http.Transport` via `RoundTrip`. Go's Transport natively supports upgrades: for such requests it returns the 101 `*http.Response` with `resp.Body` as an `io.ReadWriteCloser` over the raw target connection (the same mechanism `coder/websocket`-style clients rely on).
2. Write the 101 response and response headers to the client through the normal `ResponseWriter` (`WriteHeader(101)` then header copy), then `Hijack()` the client connection.
3. Run two pump goroutines (client→target, target→client) that read frames, write the identical bytes to the peer, and hand parsed frames to the recorder.

Alternatives rejected: post-hoc inspection of ReverseProxy's hijacked connection (ReverseProxy owns the conn — no hook); gorilla/websocket dependency (a full client/server stack we don't need, and its connection model fights raw byte-forwarding).

### D2: Forward raw bytes, parse only for recording

The pump reads a frame as an exact byte range (fixed header, extended length, masking key, payload), writes that same byte slice to the peer, and parses it in place for recording. Client→server frames are masked per RFC 6455 — we record the unmasked payload but forward the masked bytes untouched. Fragmentation is reassembled only in the recorder; each frame is forwarded individually. This makes byte-identical forwarding structural rather than a best effort.

### D3: New WebSocketSession types and separate ws-session files

New types in `pkg/proxy`:

```go
type WebSocketMessage struct {
    Direction string // "client_to_server" | "server_to_client"
    Opcode    int    // 1 text, 2 binary
    Payload   []byte // raw; sanitized at generation, like HTTP bodies
    Truncated bool
    Timestamp time.Time
}

type WebSocketSession struct {
    HandshakeRequest RequestData // reuse existing capture (sanitized query/headers)
    ResponseStatus   int
    ResponseHeaders  http.Header
    Subprotocol      string      // from the server's Sec-WebSocket-Protocol response
    Messages         []WebSocketMessage
}
```

`FileStorage` gains `StoreWebSocketSession`, writing to `ws-session-<ts>-<pid>.json` with the same append-only array + flush mechanics (extract the shared append-only JSON-array-file logic so both files sync through the existing flusher). Sessions are stored when the connection closes (a session is one connection; messages accumulate in memory, bounded by the caps in D4). The `Storage` interface grows the new method; the in-memory/test mocks implement it.

**Hijacked-connection shutdown caveat:** `http.Server.Shutdown` does not track hijacked connections, so in-flight sessions would be lost on SIGINT. `ProxyServer` keeps a registry of active sessions; on shutdown it closes those connections, which runs the pump close path, stores each session, then closes storage (final flush). Partial sessions are never stored.

### D4: Capture limits

New proxy flags, defaults tuned for memory safety:
- `--max-ws-message-bytes` (default 64 KiB): per-message payload capture limit; excess truncated, message marked `Truncated`, full payload still forwarded.
- `--max-ws-messages` (default 1000): per-session recording cap; once reached, frames continue to forward but recording stops (no unbounded growth on long-lived connections).

### D5: `pkg/asyncapi` — hand-rolled model, reuse the parser

New package with JSON-tagged structs mirroring AsyncAPI 3.1.0 — the current spec line (released January 2026) — including the 3.x operations model: top-level `channels` (keyed by channel id, carrying `address`, `parameters`, and a `messages` map) and top-level `operations` (keyed by operation id, each with `action: send|receive`, a `channel` `$ref`, per-operation `security`, and `messages` refs), with message bodies in `components.messages` and payload schemas in `components.schemas`. No new dependency; YAML via the same JSON-round-trip pattern as the OpenAPI YAML path.

`Generator` (mirroring `OpenAPIGenerator`'s shape):
- `AddSession(proxy.WebSocketSession)` / `LoadWebSocketSessionsFromDirectory` — reads only `ws-session-*.json` files.
- Channel templating reuses `pkg/parser` path-pattern inference, so `/chat/123` + `/chat/456` merge into `/chat/{id}`.
- Messages: per (channel, direction) group, merge payload schemas with the existing `SchemaMerger`; convert `parser.Schema` to a JSON-Schema-ish AsyncAPI payload schema. Message names are deterministic from capture order: `ClientMessage`, `ServerMessage`, `ClientMessage-2`, … on distinct schemas.
- Direction mapping (AsyncAPI 3.x operation actions, from the documented server application's perspective): client→server messages → operation with `action: receive`; server→client messages → operation with `action: send`. Operation ids are deterministic from the channel and action, e.g. `receiveChat` / `sendChat`.
- Security: run the existing `AuthDetector` over handshake requests; emit `components.securitySchemes` (bearerHttp / apiKey) and apply schemes on the operations of channels whose handshakes carried them (3.x moved `security` from channels to operations).
- Servers: single server from `--base-path`, scheme mapped `http→ws`, `https→wss` (and `ws`/`wss` as given); protocol `ws`/`wss` recorded.

### D6: CLI wiring

- `swagdoc generate --format openapi|asyncapi` (default `openapi`; unknown values error before writing). `generateDocs` branches on format; OpenAPI path unchanged.
- `OpenAPIGenerator.LoadTransactionsFromDirectory` skips files with the `ws-session-` prefix so mixed data directories leave OpenAPI output byte-identical (covered by a spec scenario).
- README: document `--format`, the two WS capture flags, and remove WebSocket documentation from Future Features.

## Risks / Trade-offs

- **permessage-deflate:** if the handshake negotiates compression, recorded text payloads are deflate-compressed and cannot be schema-inferred — they will be documented as binary. Forwarding is unaffected. Accepted; noted in README.
- **Hand-rolled frame parser:** correctness risk (masking, 64-bit lengths, interleaved control frames) — mitigated with dedicated unit tests plus an integration test echoing frames through the proxy.
- **Long-lived connections:** memory bounded by D4 caps; the session is only stored on close, so a process kill (`SIGKILL`) still loses in-flight sessions — same durability envelope as HTTP transactions (graceful shutdown only).
- **AsyncAPI 3.x tooling maturity:** 3.1.0 is the current spec line (released January 2026; 3.0.0 introduced the operations model in December 2024), but tooling built for the older 2.x `subscribe`/`publish` shape will not render the output. The model structs isolate the spec version in `pkg/asyncapi`, so a future minor bump (e.g. 3.2) stays contained.
- **`--format` flag naming:** a new flag could collide with future needs (e.g., gRPC/GraphQL from the README wishlist) — it generalizes cleanly (`--format grpc` later), so this is an investment, not a hack.
