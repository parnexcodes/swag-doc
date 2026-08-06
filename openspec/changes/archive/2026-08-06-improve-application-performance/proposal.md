## Why

Both halves of the application carry avoidable constant-factor overhead that grows with traffic and captured data volume. The proxy blocks every request on a disk `fsync` and serializes all writes under one mutex; generation re-parses and re-sanitizes every request/response body 4-6 times per transaction and recompiles regexes inside hot loops. At realistic volumes (thousands of transactions, multi-KB bodies) `swagdoc generate` takes minutes and proxied requests incur per-request disk latency.

## What Changes

- **Proxy capture path** (`pkg/proxy`):
  - Remove the per-transaction `file.Sync()` in `FileStorage.Store`; sync periodically (interval + on `Close`) instead of on every write. Capture data is regenerable, so crash-durability is not required per write. **Note:** durability semantics change from "every transaction is fsynced immediately" to "transactions are flushed within the sync interval".
  - Shrink the `FileStorage` critical section: marshal outside the mutex; keep the write + bookkeeping locked.
  - Cap buffered response bodies in `responseWriter` (configurable max bytes, default e.g. 10 MiB) so a single large/streamed response cannot balloon proxy memory; truncated bodies are recorded as such.
  - Avoid re-allocating the request body in `captureRequest` (`io.ReadAll` + `bytes.NewBuffer` copy): restore `r.Body` from the already-read slice.

- **Generation path** (`pkg/openapi`, `pkg/parser`, `cmd/swagdoc`):
  - Sanitize each unique body once per generation run and reuse the result across passes. `sanitizeBodyForSchema` is deterministic, so a per-transaction cache is behavior-preserving. This removes the dominant repeated unmarshal/marshal/unmarshal cost in `generateAPI`.
  - Hoist every `regexp.MustCompile` out of hot functions in `pkg/parser/path_patterns.go` (`TemplatizePath`, `templatizeSinglePath`, `inferParameterType`, `detectCommonRESTPatterns`) into package-level compiled regexes. `TemplatizePath` currently recompiles a UUID regex per segment per transaction.
  - Memoize string-format inference in `pkg/parser/smart_type.go`: `inferStringFormat` runs 7 regexes plus `time.Parse` calls per string value per sample; cache results per value.
  - Stop creating a session file when generating: `generateDocs` calls `proxy.NewFileStorage(dataDir)` purely to read, which creates a new empty `session-*.json` on every run and never closes the file. Add a read-only load path (no constructor side effect) and close the file handle.
  - Stream-decode session files in `GetAll` (`json.Decoder` over the open file) instead of `os.ReadFile` + `json.Unmarshal` double buffering.
  - Add Go benchmarks (`pkg/proxy`, `pkg/openapi`) on synthetic transaction corpora so performance is measured, not assumed, and regressions are caught.

- **No breaking user-facing changes**: CLI flags, output format, and generated spec content stay identical. Only internal behavior and timing change.

## Capabilities

### New Capabilities

- `proxy/performance`: Capture-path performance requirements — per-request storage overhead, write durability policy, and bounded response buffering while proxying.
- `generation/performance`: Generation-path performance requirements — bounded per-body processing, regex/format-inference caching, read-only data loading, and benchmarked throughput.

### Modified Capabilities

None — `openspec/specs/` currently contains no capabilities.
