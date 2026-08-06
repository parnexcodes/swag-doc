## Purpose

Performance contract for the traffic-capture half of the application: proxy request handling must be decoupled from disk persistence, memory usage must stay bounded when large responses are captured, and concurrent captures must be lossless.

## ADDED Requirements

### Requirement: Transactions persist without per-request disk flush

Storing a captured transaction must not perform a synchronous disk flush in the request path. Captured data must reach disk within a bounded time window and must always be flushed to disk on graceful shutdown.

#### Scenario: Store returns without waiting for disk
- **WHEN** a transaction is stored while the proxy is running
- **THEN** the store call completes without forcing a disk flush, and the transaction is durably written within the configured flush window

#### Scenario: Graceful shutdown flushes all transactions
- **WHEN** the proxy shuts down gracefully
- **THEN** every stored transaction is flushed to the session file before the process exits

### Requirement: Large response bodies are captured with bounded memory

Response bodies larger than the capture limit must be truncated rather than buffered in full, the stored transaction must record that truncation occurred, and generation must still process the truncated transaction without error.

#### Scenario: Oversized response body is truncated
- **WHEN** a proxied response body exceeds the configured capture limit (default 10 MiB)
- **THEN** only the first capture-limit bytes are retained, the transaction is marked as truncated, and proxy memory usage stays bounded by the limit

#### Scenario: Truncated transaction still generates
- **WHEN** generation processes a transaction whose body was truncated
- **THEN** generation succeeds and produces the same output as for a transaction whose body was exactly the capture limit

### Requirement: Concurrent capture is lossless

All transactions captured while the proxy handles concurrent requests must be stored without loss, and the resulting session file must be valid JSON.

#### Scenario: Concurrent requests are all persisted
- **WHEN** N requests are handled concurrently through the proxy
- **THEN** all N transactions are present in the session file after graceful shutdown, and the file parses as a valid JSON array

### Requirement: Capture writes are serialized safely

Transactions stored from concurrent handlers must be written atomically with respect to one another; a transaction must never appear partially written or interleaved in the session file.

#### Scenario: No interleaved writes
- **WHEN** multiple handlers store transactions at the same time
- **THEN** each transaction appears in the session file as one complete, contiguous JSON object
