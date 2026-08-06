## 1. WebSocket frame layer

- [x] 1.1 Add `pkg/proxy/websocket.go` with an RFC 6455 frame reader: parses FIN/RSV/opcode, mask bit, 7/16/64-bit lengths, and masking key from `bufio.Reader`, returning the exact byte range of each frame
- [x] 1.2 Add frame writer helper that writes a captured frame's bytes verbatim (no re-encoding, so forwarding is byte-identical)
- [x] 1.3 Unit-test the frame reader: text/binary opcodes, 16-bit and 64-bit lengths, masked (client) and unmasked (server) frames, fragmentation, interleaved ping/pong/close control frames, invalid opcode/RSV errors

## 2. WebSocket capture pipeline

- [x] 2.1 Detect upgrade requests in the proxy handler (`Connection: Upgrade` + `Upgrade: websocket`, case-insensitive) and route them away from `ReverseProxy.ServeHTTP`
- [x] 2.2 Forward the upgrade request with a per-proxy `http.Transport` `RoundTrip`; verify the 101 response arrives with `resp.Body` usable as `io.ReadWriteCloser`
- [x] 2.3 Write the 101 response and response headers to the client via `WriteHeader(101)` + header copy, then `Hijack()` the client connection
- [x] 2.4 Add the two-direction pump: read frame byte ranges, write them verbatim to the peer, feed parsed frames to the recorder; tear down both connections on EOF/error
- [x] 2.5 Add the recorder: unmask client frames for recording, reassemble fragmented messages, record direction/opcode/payload/timestamp, skip ping/pong/close
- [x] 2.6 Apply capture limits: per-message truncation at `MaxWSMessageBytes` (marked `Truncated`, full payload still forwarded) and stop recording after `MaxWSMessages` while forwarding continues
- [x] 2.7 Record the negotiated subprotocol from the server's `Sec-WebSocket-Protocol` response header and the handshake response status/headers
- [x] 2.8 Track active sessions in `ProxyServer`; on graceful shutdown close them so each session is stored before storage final flush
- [x] 2.9 Integration-test end-to-end capture through the proxy: echo-style WS server, verify byte-identical forwarding, message recording per direction, truncation, and message-cap behavior

## 3. WebSocket session storage

- [x] 3.1 Define `WebSocketMessage` and `WebSocketSession` types in `pkg/proxy` and extend the `Storage` interface with `StoreWebSocketSession`
- [x] 3.2 Extract the shared append-only JSON-array-file logic in `FileStorage` and add `ws-session-<ts>-<pid>.json` handling with the same flush/durability guarantees; implement `StoreWebSocketSession` in `FileStorage` and the reader
- [x] 3.3 Update test mocks for the extended `Storage` interface
- [x] 3.4 Test session round-trip: store sessions, read them back, verify handshake + messages survive, and verify graceful-shutdown flush

## 4. AsyncAPI generator

- [x] 4.1 Create `pkg/asyncapi` with JSON-tagged AsyncAPI 3.1.0 model structs (document, info, servers, channels with address/parameters, top-level operations with action send/receive, messages, components) and JSON/YAML serialization (YAML via JSON round-trip, same pattern as OpenAPI)
- [x] 4.2 Add `Generator.AddSession` and `LoadWebSocketSessionsFromDirectory` reading only `ws-session-*.json` files
- [x] 4.3 Derive channels with `pkg/parser` path templating so repeated numeric-ID/UUID sessions merge into one `{param}` channel
- [x] 4.4 Infer messages per (channel, direction): merge JSON payload schemas with `SchemaMerger`, map non-JSON payloads to string/binary, deterministic names from capture order
- [x] 4.5 Map client→server messages to an operation with `action: receive` and server→client to `action: send`, each referencing its channel; emit `components.schemas` and `components.messages` for payloads
- [x] 4.6 Detect handshake auth with `pkg/parser` `AuthDetector` and emit per-operation `securitySchemes`
- [x] 4.7 Map `--base-path` to an AsyncAPI server entry with `ws`/`wss` protocol and version fields
- [x] 4.8 Unit-test generation: channel merging, message inference, send/receive mapping, security schemes, deterministic output, JSON/YAML serialization

## 5. CLI and OpenAPI isolation

- [x] 5.1 Add `--format` flag to `swagdoc generate` (default `openapi`; unknown values error before writing any file) and branch `generateDocs`
- [x] 5.2 Add `--max-ws-message-bytes` and `--max-ws-messages` proxy flags and wire them into `ProxyServer`
- [x] 5.3 Make `OpenAPIGenerator.LoadTransactionsFromDirectory` skip `ws-session-*.json` files
- [x] 5.4 Test: mixed data directory produces byte-identical OpenAPI output; `--format asyncapi` produces a parseable AsyncAPI 3.1.0 document (JSON and YAML paths)

## 6. Example and docs

- [x] 6.1 Add a WebSocket endpoint to `examples/simple_api` and extend `make demo` to exercise it through the proxy
- [x] 6.2 Update README: `--format`, WS capture flags, AsyncAPI output section; remove WebSocket documentation from Future Features

## 7. Verification

- [x] 7.1 Run the mandatory suite: `go build ./...`, `go test ./...`, `golangci-lint run`, `golangci-lint fmt`
- [x] 7.2 Run `make demo` (or the equivalent end-to-end flow) and confirm both `swagger.json` and the AsyncAPI output are produced from one capture
