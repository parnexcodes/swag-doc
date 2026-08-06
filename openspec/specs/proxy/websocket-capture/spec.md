## Purpose

The proxy captures WebSocket connections — handshake metadata and the messages exchanged in each direction — so the documentation generators can describe socket-based endpoints, while forwarding the live connection to the client untouched.

## Requirements

### Requirement: WebSocket handshakes are captured as sessions

When a proxied request negotiates a WebSocket upgrade (request headers include a connection upgrade to `websocket`) and the target responds with `101 Switching Protocols`, the proxy MUST record a WebSocket session containing the handshake request path, query parameters, headers, subprotocols, response status, and response headers, instead of recording a plain HTTP transaction. Handshakes that do not complete (the target does not return 101) MUST be captured as ordinary HTTP transactions as today.

#### Scenario: Successful upgrade recorded as a WebSocket session
- **WHEN** a client sends an upgrade request through the proxy and the target responds 101
- **THEN** the captured data contains a WebSocket session whose request path, sanitized query parameters, sanitized headers, and 101 response status are recorded, and no plain HTTP transaction is recorded for that request

#### Scenario: Failed upgrade stays an HTTP transaction
- **WHEN** a client sends an upgrade request through the proxy and the target responds with a non-101 status
- **THEN** the exchange is captured exactly as a normal HTTP transaction with the actual response status

### Requirement: Messages exchanged over the connection are captured with direction

The proxy MUST record the messages exchanged over a captured WebSocket connection, each with its direction (client to server, or server to client), its payload, and the sequence in which it occurred. Text and binary data frames are captured; fragmented messages spanning multiple frames MUST be recorded as a single message. Control frames (ping, pong, close) MUST NOT be recorded as messages.

#### Scenario: Text messages recorded per direction
- **WHEN** a client sends two text messages and the server sends one text message over a captured connection
- **THEN** three messages are recorded, the client-to-server ones with client direction and the server-to-client one with server direction, each with its payload

#### Scenario: Fragmented messages are reassembled
- **WHEN** a message is sent split across multiple WebSocket data frames
- **THEN** it is recorded as one message whose payload equals the concatenation of the frame payloads

#### Scenario: Control frames are not recorded as messages
- **WHEN** a connection exchanges ping, pong, and close frames
- **THEN** no messages are recorded for those frames

### Requirement: WebSocket capture is bounded

Capture of a WebSocket connection MUST NOT retain unbounded data: messages larger than the per-message capture limit are truncated and marked truncated, and recording stops after the per-session message limit, while the live connection continues to be forwarded in full.

#### Scenario: Oversized message is truncated
- **WHEN** a message payload exceeds the configured per-message capture limit
- **THEN** only the first capture-limit bytes are retained, the message is marked as truncated, and the full payload is still forwarded to the recipient

#### Scenario: Message limit stops recording, not forwarding
- **WHEN** a connection exceeds the configured per-session message limit
- **THEN** no further messages are recorded for that connection, and frames continue to be forwarded unchanged until the connection closes

### Requirement: Live WebSocket traffic is forwarded unchanged

Capture MUST NOT alter, delay, or drop any frame forwarded between the client and the target: the connection must behave as if the proxy were absent. A WebSocket connection that is not being captured (e.g., capture limits reached or recording disabled) MUST still be proxied in full.

#### Scenario: Frames pass through unmodified
- **WHEN** a client and target exchange arbitrary WebSocket frames through the proxy
- **THEN** every frame payload reaches the recipient byte-identical to how it was sent, regardless of what was captured

### Requirement: WebSocket sessions persist separately from HTTP transactions

Captured WebSocket sessions MUST be stored in the data directory in files whose names are distinct from the HTTP session files (`session-*.json`), and MUST be readable back by the storage reader for generation. Session data MUST be flushed to disk following the same durability guarantees as HTTP transactions (bounded flush window, final flush on graceful shutdown).

#### Scenario: WebSocket sessions round-trip through storage
- **WHEN** the proxy captures WebSocket sessions and shuts down gracefully
- **THEN** a `ws-session-*.json` file exists in the data directory and reading it back yields all captured sessions with their handshakes and messages

#### Scenario: Session data survives graceful shutdown
- **WHEN** the proxy shuts down gracefully immediately after capturing a WebSocket session
- **THEN** the session is present in the persisted file after the process exits

### Requirement: Sensitive handshake data is redacted

Sensitive headers and query parameters of WebSocket handshakes MUST be redacted at capture time using the same rules as HTTP transactions, so credentials never reach disk.

#### Scenario: Authorization header is redacted
- **WHEN** a handshake request contains an `Authorization` header
- **THEN** the stored session contains a redaction placeholder for that header, not the credential value
