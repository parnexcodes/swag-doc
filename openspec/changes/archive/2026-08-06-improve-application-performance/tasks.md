## 1. Baseline measurement

- [x] 1.1 Add synthetic transaction corpus builder (test helper, e.g. `pkg/openapi/bench_test.go`) producing configurable transactions: N total, distinct paths, distinct body shapes, and a repeated-bodies mode
- [x] 1.2 Add initial benchmarks on current code: `BenchmarkFileStorageStore` (sequential + 16-goroutine concurrent) in `pkg/proxy`, `BenchmarkGenerateSpec` (RepeatedBodies / DistinctBodies) in `pkg/openapi`; record baseline numbers
- [x] 1.3 Generate baseline `swagger.json` from a fixed corpus and save it for the output-identity diff in 4.2

## 2. Proxy capture path

- [x] 2.1 Replace per-transaction `file.Sync()` in `FileStorage.Store` with periodic flush (ticker, default 5s, configurable) plus `Sync()` in `Close()`; move `json.Marshal` outside the mutex so the critical section covers only write + bookkeeping
- [x] 2.2 Add/adjust storage tests: concurrent lossless store (all N transactions present, valid JSON array), flush-on-close (data on disk after graceful Close), no interleaved/partial transactions; update existing tests that assumed per-store sync
- [x] 2.3 Add `MaxBodyBytes` (default 10 MiB) to `ProxyServer`; make `responseWriter` truncate captured bytes at the limit while still forwarding the full response; add `ResponseTruncated` marker to `APITransaction` (JSON `response_truncated`, omitted when false); wire `--max-body-bytes` flag on `swagdoc proxy`
- [x] 2.4 Add truncation tests: oversize response is truncated at the limit with marker set, proxy memory stays bounded, generation still succeeds on a truncated transaction
- [x] 2.5 Avoid the request-body copy in `captureRequest` (`io.NopCloser(bytes.NewReader(bodyBytes))` instead of `bytes.NewBuffer`)

## 3. Generation path

- [x] 3.1 Add direct object sanitizer (unmarshal + placeholder walk, no marshal roundtrip) and a content-keyed cache in `OpenAPIGenerator`; rewire every `sanitizeBodyForSchema` call site in `generateAPI` (pass 1, skip branch, request/response body paths) to the cache; remove `sanitizeBodyForSchema`
- [x] 3.2 Add generation tests proving cache correctness: identical spec output with repeated bodies vs. single body, and equality of sanitized objects with the previous behavior on representative bodies
- [x] 3.3 Hoist all fixed regexes in `pkg/parser/path_patterns.go` to package level; replace the basePath-anchored dynamic regexes in `detectCommonRESTPatterns` with `strings.HasPrefix` checks + shared UUID regex
- [x] 3.4 Add `formatCache` to `TypeInferrer` (cleared in `ResetSamples`) and hoist the 9 format regexes in `pkg/parser/smart_type.go` to package level; `time.Parse` fallbacks run only on regex miss (unchanged)
- [x] 3.5 Add `NewFileStorageReader(baseDir)` (no session file creation; `Store`/`Close`/`Clear` guarded for nil file); switch `generateDocs` to the reader and `defer Close`; stream `GetAll` with `os.Open` + `json.Decoder` file-by-file, preserving skip-on-parse-error tolerance
- [x] 3.6 Add tests: `generate` leaves the data directory byte-for-byte unchanged, reader constructor creates no file, `GetAll` handles multiple/large files with bounded memory

## 4. Verification and docs

- [x] 4.1 Re-run benchmarks and compare with baseline: store bench improves by an order of magnitude (fsync removed), RepeatedBodies is a small constant multiple of DistinctBodies, both scale ~linearly with corpus size
- [x] 4.2 Diff `swagger.json` from the fixed corpus (1.3) against the post-change output — must be identical
- [x] 4.3 Profile `generate` on a large synthetic corpus with pprof; confirm per-unique-body processing (sanitize cost tracks distinct bodies, not total transactions)
- [x] 4.4 Run the mandatory suite: `go build ./...`, `go test ./...`, `golangci-lint run`, `golangci-lint fmt`; run `make demo` end-to-end
- [x] 4.5 Document `--max-body-bytes` in README.md (and the flush interval in the proxy section if storage options are documented there)
