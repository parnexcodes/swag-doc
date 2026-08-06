## Context

Current state (see proposal.md - Why for motivation):

- `pkg/proxy/storage.go` — `FileStorage.Store` calls `file.Sync()` (fsync) on every transaction inside a global mutex, and `json.Marshal` runs inside the same critical section. `GetAll` uses `os.ReadFile` + `json.Unmarshal` (double allocation per file).
- `pkg/proxy/proxy.go` — `responseWriter` buffers the entire response body in `bytes.Buffer` with no cap; `captureRequest` copies the request body twice (`io.ReadAll` then `bytes.NewBuffer`).
- `pkg/openapi/openapi.go` — `generateAPI` calls `sanitizeBodyForSchema` (unmarshal → recursive placeholder rewrite → marshal → unmarshal) 4–6 times per transaction across its two passes and the skip-branch. `maybeDecodeBase64` and `convertPlaceholders` add more full-body passes.
- `pkg/parser/path_patterns.go` — `regexp.MustCompile` is called inside hot functions (`TemplatizePath` per segment per transaction, `templatizeSinglePath`, `inferParameterType`, `detectCommonRESTPatterns`).
- `pkg/parser/smart_type.go` — `inferStringFormat` runs 7 regexes plus `time.Parse` fallbacks per string value per sample.
- `cmd/swagdoc/main.go` — `generateDocs` calls `proxy.NewFileStorage(dataDir)` purely to read; the constructor creates a new empty `session-*.json` file on every run and the handle is never closed.

## Goals / Non-Goals

**Goals:**
- Proxy: no synchronous disk flush in the store path; bounded capture memory for large responses; lossless concurrent capture.
- Generate: one parse/sanitize per unique body per run; no regex compilation in hot loops; memoized string-format inference; read-only access to the data directory; benchmark suite that measures all of the above.
- Verified end-to-end: identical `swagger.json` output on a fixed corpus before/after, plus measured before/after speedups.

**Non-Goals:**
- No new external dependencies (standard library + existing kin-openapi only).
- No parallelization of generation (the multi-pass structure stays single-threaded; parallelism is future work).
- No change to sanitization semantics, schema-inference rules, or generated spec content.
- No capture limit on request bodies (they must be forwarded intact to the target).
- No change to the schema-merging algorithm.
- No new CLI flags except the response capture limit.

## Decisions

### 1. Replace per-transaction fsync with periodic + shutdown flush

`FileStorage` gains a `flushInterval` option (default 5s) with a ticker goroutine calling `file.Sync()`, plus a final `Sync()` inside `Close()` before the file closes. `Store` performs only the write. Rationale: capture data is regenerable, so a bounded durability window is acceptable; this removes the dominant per-request latency. Alternatives considered: sync every N transactions (uneven latency spikes), sync-on-close only (unbounded loss window), a dedicated writer goroutine + channel (ordering complexity with `Clear`, no benefit once fsync is gone). Chosen: ticker + close sync — bounded, simple, keeps `Store` synchronous.

`Store` marshals the transaction before acquiring the mutex, so the critical section covers only the separator write + body write + bookkeeping append. Concurrency remains safe: writes stay serialized, each transaction is one contiguous JSON object (spec: "Capture writes are serialized safely").

### 2. Bounded response capture via `MaxBodyBytes`

`ProxyServer` gains `MaxBodyBytes` (default 10 MiB, wired to a new `--max-body-bytes` flag on `swagdoc proxy`). `responseWriter` tracks remaining budget: `Write` forwards everything to the client but copies at most the remaining budget into the capture buffer, setting a `truncated` flag when the budget is exhausted. `APITransaction` gains a `ResponseTruncated bool` field (JSON `response_truncated`, omitted when false so old session files stay compatible). `captureResponse` sets it. Generation treats a truncated body exactly like any body: if truncation cut mid-JSON, `sanitizeBodyForSchema` fails to parse and the body contributes no schema — identical to today's handling of unparseable bodies. Rationale: streaming to a temp file adds I/O complexity for a rare case; the limit is the simple bound. Alternative: spool to disk past the limit (more machinery, no behavioral gain at the default).

### 3. One JSON parse per unique body in generation

Replace `sanitizeBodyForSchema` (which marshals the sanitized map back to bytes, only to be unmarshaled again by every caller) with a direct object sanitizer `sanitizeBodyObject(data []byte) (interface{}, bool)` that unmarshals once and returns the placeholder map/array. `OpenAPIGenerator` caches results keyed by body content (`map[string]interface{}` keyed by `string(body)`), so identical bodies across transactions sanitize once per run. All call sites in `generateAPI` (pass 1 samples, pass-2 skip branch, request body, response body) read from the cache. Sharing is safe because every consumer copies before mutating: `convertPlaceholders` builds new maps, `parseJSONBody` stores the converted copy as the example, and `ApplyTypeInference` reads samples without mutating them.

Memory trade-off: the cache holds one decoded object per distinct body; bounded by the response capture limit per body and by corpus diversity. This satisfies the spec requirement that repeated bodies do not multiply processing cost.

### 4. Hoist regexes out of hot loops in `pkg/parser/path_patterns.go`

Package-level compiled regexes for the fixed patterns (UUID, numeric segment, date, slug, version segment). The two basePath-anchored regexes in `detectCommonRESTPatterns` (`reIDPath`, `reUUID`) are replaced with string prefix checks (`strings.HasPrefix`) plus the shared UUID regex, eliminating dynamic `regexp.Compile` per call. Behavior is identical — the same patterns, compiled once. `TemplatizePath`'s per-segment UUID match uses the package-level regex.

### 5. Memoize string-format inference in `pkg/parser/smart_type.go`

`TypeInferrer` gains `formatCache map[string]string` (cleared in `ResetSamples`); `inferStringFormat` consults it before running the pattern loop. All 9 format regexes move to package-level vars. The `time.Parse` fallbacks run only when no regex matches (as today). Cache size is bounded by distinct string values across at most `maxSamples` (10) samples per field.

### 6. Read-only data access for `generate`

Add `NewFileStorageReader(baseDir) (*FileStorage, error)` — same struct with `file == nil` and no session file created. `GetAll` works unchanged; `Store`/`Close`/`Clear` are guarded for nil file (return error / no-op appropriately). `generateDocs` switches to the reader constructor and calls `Close` via defer. `GetAll` streams each file with `os.Open` + `json.Decoder`, decoding file-by-file into the accumulator so only one file's bytes are live at a time (spec: memory bounded by one file). Unparseable files continue to be skipped silently, preserving current tolerance of partially-written session files.

### 7. Avoid the request-body copy in `captureRequest`

`r.Body = io.NopCloser(bytes.NewReader(bodyBytes))` instead of `bytes.NewBuffer(bodyBytes)` — `bytes.Reader` does not copy. Behavior unchanged; the upstream proxy still receives the full body.

### 8. Benchmark suite as the acceptance harness

- `pkg/proxy/bench_test.go`: `BenchmarkFileStorageStore` (sequential and 16-goroutine concurrent) — before/after delta quantifies the fsync removal.
- `pkg/openapi/bench_test.go`: synthetic corpus builder (configurable transactions, distinct paths, distinct body shapes, repeated-bodies mode). Benchmarks: `BenchmarkGenerateSpec/RepeatedBodies` and `BenchmarkGenerateSpec/DistinctBodies` — the repeated-body corpus must cost a small constant multiple of the distinct-body corpus (spec: work bounded per unique body), and both must scale ~linearly (spec: linear bound).
- Corpus builder lives in a shared test helper so `generate` verification can reuse it.
- Verification procedure (in tasks): capture before/after timings and `pprof` CPU profiles; diff `swagger.json` from a fixed corpus to prove output identity.

## Risks / Trade-offs

- **Durability window**: a crash can lose the last ≤5s of captures (previously zero-loss). Accepted: capture data is regenerable; flush on graceful shutdown preserved. The interval is configurable for users who want tighter bounds.
- **Truncation changes output for pathological bodies**: bodies over the limit were previously processed whole (slowly); now they are cut at the limit. Output changes only for bodies whose schema inference depended on bytes past the limit — practically, huge bodies were already a correctness/performance hazard. Recorded as a spec scenario.
- **Cache memory**: the content-keyed sanitize cache holds one decoded object per distinct body; bounded per body by the capture limit, but a corpus of many huge distinct bodies costs corpus-sized memory. Mitigated by the 10 MiB default limit; revisit with a streaming path only if profiles demand it.
- **Output-identity risk**: refactoring the sanitize path must not change generated specs. Mitigation: the fixed-corpus before/after diff is a mandatory verification step; Go map marshaling is deterministic (sorted keys), so no ordering drift is expected.
- **`GetAll` during active capture**: `generate` reading a session file the proxy is still writing can see a partial file; existing skip-on-parse-error behavior already tolerates this — unchanged.

## Open Questions

None blocking. Flag defaults (flush interval 5s, capture limit 10 MiB) are chosen conservatively; both are configurable at the API level, with the capture limit also exposed as a CLI flag.
