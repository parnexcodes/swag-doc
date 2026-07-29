# SwagDoc Codebase Review Report

**Date:** 2026-07-29  
**Reviewer:** Automated Deep Review  
**Scope:** Full codebase analysis against project intent, edge cases, correctness, and completeness

---

## Executive Summary

SwagDoc aims to **automatically generate OpenAPI/Swagger documentation from captured API traffic** by acting as a reverse proxy, analyzing requests/responses, and producing a spec — all without code changes. While the high-level architecture is sound and tests pass, the implementation has **fundamental flaws** that undermine its core purpose: the sanitization layer destroys the data needed for accurate schema generation, the path pattern detection is fragile, and many critical edge cases are unhandled. The tool works for toy demos but will produce poor or incorrect documentation for real-world APIs.

---

## Table of Contents

1. [Critical Issues (Breaks Core Intent)](#1-critical-issues-breaks-core-intent)
2. [Major Issues (Significantly Impacts Quality)](#2-major-issues-significantly-impacts-quality)
3. [Minor Issues (Code Quality / Robustness)](#3-minor-issues-code-quality--robustness)
4. [Missing Features vs. Stated Intent](#4-missing-features-vs-stated-intent)
5. [Test Coverage Gaps](#5-test-coverage-gaps)
6. [Recommendations](#6-recommendations)

---

## 1. Critical Issues (Breaks Core Intent)

### 1.1 Sanitization Destroys Type Information Needed for Schema Generation

**Files:** `pkg/proxy/proxy.go:189-234`, `pkg/openapi/openapi.go:733-771`

The proxy sanitizes all JSON values to placeholders (`"__string__"`, `"__integer__"`, etc.) before storage. The generator then tries to convert these placeholders back. This round-trip is lossy and fundamentally broken:

- **Arrays lose diversity:** `sanitizeValue` keeps only the first element of arrays (`proxy.go:221`). If an API returns `[{"id":1,"name":"Alice"}, {"id":2,"email":"bob@test.com"}]`, the second object's unique fields (`email`) are silently discarded. The schema will never see `email` as a field.
- **Placeholder conversion creates fake values:** `convertPlaceholders` turns `"__string__"` into the literal string `"string"`, `"__integer__"` into `int64(0)`, etc. (`openapi.go:752-767`). These become the "examples" in the generated spec. Users will see `"example": "string"` instead of real examples.
- **Nested arrays are flattened:** If a response is `{"data": [{"tags": ["a","b"]}]}`, the inner array `["a","b"]` becomes `["__string__"]` (one element), and then the generator sees only one sample.
- **No null preservation:** `sanitizeValue` returns `nil` for `nil`, but the generator's `parseJSONBody` skips null fields entirely, losing nullable field information.

**Impact:** The core value proposition — "analyze HTTP requests and responses to infer API structure" — is undermined because the analysis happens on degraded data.

### 1.2 Duplicate / Dead Code Paths for Spec Generation

**Files:** `pkg/parser/parser.go:464-473`, `pkg/openapi/generator.go`, `pkg/openapi/openapi.go`

There are **three separate code paths** that can generate OpenAPI specs:

1. `parser.GenerateOpenAPISpec()` — standalone function in `parser.go`
2. `openapi.Generator` struct in `generator.go` — wraps `OpenAPIGenerator`
3. `openapi.OpenAPIGenerator` in `openapi.go` — the actual implementation used by CLI

The CLI uses path #3. Paths #1 and #2 are **never called by the CLI** and appear to be dead code or leftover from earlier iterations. `parser.GenerateOpenAPISpec` generates a `map[string]interface{}` (not the kin-openapi struct), uses different logic, and produces different output. This creates confusion about which is the "real" generator.

**Impact:** Maintenance burden, confusion, and the dead paths have their own bugs (e.g., `parser.go` doesn't handle path templating, doesn't detect auth, doesn't merge schemas).

### 1.3 Path Pattern Detection Requires Multiple Observations — Fails on Single Requests

**Files:** `pkg/parser/path_patterns.go:53-57`, `pkg/parser/path_patterns.go:238-262`

The `PathPatternDetector.AnalyzePatterns()` method skips groups with fewer than 2 paths:

```go
if len(pathGroup) < 2 {
    continue  // Need at least 2 paths to detect a pattern
}
```

And `TemplatizePath` only matches if a pattern was previously detected:

```go
if _, exists := d.patterns[pattern]; exists {
    // ...
}
```

**Consequence:** If a user only hits `GET /users/123` once (never hitting `/users/456`), the path will NOT be templatized. The generated spec will have `/users/123` as a literal path instead of `/users/{id}`. This is the **most common real-world scenario** — users don't hit the same endpoint with multiple IDs during a capture session.

The `detectCommonRESTPatterns` partially addresses this for numeric IDs and UUIDs, but only for the **first two path segments**. Paths like `/api/v1/users/123/posts/456` won't have `456` templatized unless multiple observations exist.

### 1.4 `--group-by-path` and `--tag-mapping` / `--version-prefix` Flags Are Ignored by the CLI

**File:** `cmd/swagdoc/main.go:97-100`

The `generateCmd.RunE` function calls `generateDocs()` but does NOT pass `generateUsePathGroups`, `generateTagMapping`, or `generateVersionPrefix`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    return generateDocs(generateOutput, generateDataDir, generateTitle, generateDescription,
        generateVersion, generateBasePath, generateCleanup)
    // Missing: generateUsePathGroups, generateTagMapping, generateVersionPrefix
},
```

Inside `generateDocs`, these flags ARE used (lines 331, 343-353), but they rely on **package-level variables** which are set by cobra. So this actually works by accident via global state, but the function signature is misleading and fragile. If anyone calls `generateDocs` programmatically with different values, those values would be ignored in favor of the globals.

**Impact:** The `generateDocs` function is not properly parameterized — it silently depends on global state.

---

## 2. Major Issues (Significantly Impacts Quality)

### 2.1 Response Body Sanitization Loses Array-of-Objects Structure

**File:** `pkg/proxy/proxy.go:217-222`

```go
case []interface{}:
    if len(v) == 0 {
        return []interface{}{}
    }
    return []interface{}{sanitizeValue(v[0])}
```

Only the first array element is kept. For `GET /users` returning 50 users with different fields, only user #1's fields appear in the schema. Fields that are `null` in user #1 but populated in others are lost entirely.

### 2.2 Base64 Detection Heuristic Is Unreliable

**File:** `pkg/openapi/openapi.go:113-127`

```go
func maybeDecodeBase64(data []byte) ([]byte, error) {
    if len(data) > 0 && data[0] == 'W' {
        decoded, err := base64.StdEncoding.DecodeString(string(data))
```

The check `data[0] == 'W'` is a heuristic that assumes base64-encoded JSON starts with `W` (because JSON objects/arrays starting with `{` or `[` encode to base64 strings starting with `W`). This is:
- **Fragile:** Legitimate JSON responses starting with `W` (e.g., `{"Widget": ...}`) would be incorrectly treated as base64.
- **Incomplete:** Not all base64 JSON starts with `W`. The base64 of `{"a":1}` is `eyJhIjox` which starts with `e`, not `W`.
- **Silent failure:** If base64 decoding fails, it falls through silently, which is correct behavior but the initial heuristic is still wrong.

### 2.3 `strings.Title` Is Deprecated

**File:** `pkg/openapi/openapi.go:1010`

```go
func capitalize(s string) string {
    if s == "" {
        return "default"
    }
    return strings.Title(s)
}
```

`strings.Title` has been deprecated since Go 1.18 because it doesn't handle Unicode properly. It also produces incorrect results for already-capitalized segments (e.g., `"API"` stays `"API"` but `"api"` becomes `"Api"`, while `"my-api"` becomes `"My-Api"` instead of `"My API"`).

### 2.4 Proxy Doesn't Handle Non-JSON Content Types

**File:** `pkg/proxy/proxy.go:189-201`

`sanitizeJSON` is called on ALL request/response bodies regardless of content type. If the API returns `text/html`, `application/xml`, `multipart/form-data`, or binary data (`image/png`), `json.Unmarshal` will fail and the body will be silently replaced with `[]byte{}`. The proxy **does not preserve non-JSON bodies at all**.

**Impact:** Any API that returns XML, HTML, file downloads, or form data will have empty response bodies in the generated spec.

### 2.5 Proxy Consumes Full Request/Response Body Into Memory

**File:** `pkg/proxy/proxy.go:116`, `proxy.go:165-168`

Both `captureRequest` and the `responseWriter.Write` method buffer the entire body in memory. For large responses (file downloads, large JSON payloads), this causes:
- High memory usage proportional to response size
- No streaming — the entire response must be buffered before being forwarded to the client
- Potential OOM for large file transfers

### 2.6 No Graceful Shutdown for Proxy Server

**File:** `pkg/proxy/proxy.go:108`

```go
return http.ListenAndServe(addr, handler)
```

Uses `http.ListenAndServe` which doesn't support graceful shutdown. In-flight requests are killed when the process exits. Should use `http.Server` with `Shutdown()`. Also no signal handling (SIGINT/SIGTERM) for cleanup.

### 2.7 All Error Responses Generate Separate Schema Entries

**File:** `pkg/openapi/openapi.go:243-538`

When the same endpoint returns `200` and `404` (or `400`, `500`), each status code gets its own response entry with potentially different schemas. The schema merging logic (lines 540-596) only merges across transactions with the **same status code**, not across different status codes for the same endpoint. This is partially correct, but error response bodies (which often have a different schema like `{"error": "message"`) are included without being distinguished from the success schema.

### 2.8 Query Parameter Sanitization Loses Type Information

**File:** `pkg/proxy/proxy.go:288-298`

All non-sensitive query parameter values are replaced with `"__string__"`, losing any information about whether the parameter is numeric, boolean, etc. The generator then always creates `string` type schemas for query parameters.

### 2.9 `generateDocs` Writes Session File on Every Transaction

**File:** `pkg/proxy/storage.go:47-62`

```go
func (s *FileStorage) Store(transaction APITransaction) error {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    s.transactions = append(s.transactions, transaction)
    data, err := json.MarshalIndent(s.transactions, "", "  ")
    // ...
    return os.WriteFile(s.sessionFile, data, 0644)
}
```

Every single transaction triggers a full JSON re-serialization and file write of ALL accumulated transactions. For high-traffic APIs, this is O(n²) I/O. Should batch writes or use append-friendly format.

---

## 3. Minor Issues (Code Quality / Robustness)

### 3.1 `ioutil.ReadFile` / `ioutil.ReadDir` Are Deprecated

**File:** `pkg/openapi/openapi.go:72`, `openapi.go:88`

`ioutil.ReadFile` and `ioutil.ReadDir` have been deprecated since Go 1.16. Should use `os.ReadFile` and `os.ReadDir`.

### 3.2 Error from `captureRequest` Body Read Is Silently Ignored

**File:** `pkg/proxy/proxy.go:116`

```go
bodyBytes, _ = io.ReadAll(r.Body)
```

The error from reading the request body is silently discarded.

### 3.3 `responseWriter` Doesn't Implement `http.Flusher` / `http.Hijacker`

**File:** `pkg/proxy/proxy.go:142-169`

The custom `responseWriter` wraps `http.ResponseWriter` but doesn't implement `http.Flusher`, `http.Hijacker`, or `http.Pusher`. This breaks:
- WebSocket upgrades (require `Hijacker`)
- SSE/Server-Sent Events (require `Flusher`)
- HTTP/2 server push

### 3.4 Inconsistent Header Case Handling

**File:** `pkg/parser/auth.go:64`

```go
if strings.HasPrefix(strings.ToLower(header), "x-") && !contains(apiKeyHeaders, header) {
```

`contains` does case-sensitive comparison, but `apiKeyHeaders` has mixed case entries (`"X-API-Key"`, `"api-key"`). If the actual header is `x-api-key`, it won't match `"X-API-Key"` in the slice, so the custom header detection will incorrectly trigger for known API key headers.

### 3.5 Release Workflow Runs on PRs But Also Triggers on Tags

**File:** `.github/workflows/release.yml:4-7`

```yaml
on:
  push:
    tags: [ "v*" ]
  pull_request:
    branches: [ "master" ]
```

The `release` job (which creates a GitHub Release with `softprops/action-gh-release`) runs on **both** tag pushes and PRs. Running the release step on a PR will fail or create unwanted draft releases.

### 3.6 `inferParameterType` Returns "id" for Empty Values

**File:** `pkg/parser/path_patterns.go:151-168`

When `values` is empty, `allNumeric` is vacuously true (no values to check), so it returns `"id"` / `"Numeric identifier"`. This is incorrect — empty values should return a generic parameter.

### 3.7 Session File Name Collisions

**File:** `pkg/proxy/storage.go:37`

```go
sessionFile := filepath.Join(baseDir, fmt.Sprintf("session-%s.json", time.Now().Format("20060102-150405")))
```

If two proxy instances start within the same second, they'll write to the same file, causing data corruption.

### 3.8 `capitalize` Doesn't Handle Hyphens or Underscores

**File:** `pkg/openapi/openapi.go:1006-1011`

Path segments like `user-management` become `User-Management` (hyphen preserved) instead of `User Management`. Segments like `user_management` stay `User_Management`.

### 3.9 Missing `Content-Type` Header Handling in Proxy

**File:** `pkg/proxy/proxy.go:72-103`

The proxy doesn't set `X-Forwarded-For`, `X-Forwarded-Proto`, or `X-Forwarded-Host` headers. Some backend APIs rely on these for correct behavior.

### 3.10 `parseJSONBody` Marks All Object Properties as Required

**File:** `pkg/openapi/openapi.go:688`

```go
schema.Required = append(schema.Required, key)
```

Every property found in a JSON object is added to the `Required` list. This is incorrect — just because a field appeared in one response doesn't mean it's required. Optional fields that happened to be non-null in the captured sample will be incorrectly marked as required.

---

## 4. Missing Features vs. Stated Intent

| Feature (from README/How It Works) | Status | Notes |
|---|---|---|
| "Automatically detects data types" | **Broken** | Sanitization destroys actual values; type detection works on placeholders |
| "Infers data types from observed values" | **Broken** | Values are replaced with `"__string__"` etc. before inference |
| "URL paths and parameters" | **Partial** | Path params require 2+ observations; single-request APIs fail |
| "HTTP methods" | **Working** | Method detection works correctly |
| "Request/response bodies" | **Broken for non-JSON** | Only JSON is handled; XML, form-data, binary are dropped |
| "Status codes" | **Working** | Status codes are captured correctly |
| "Content types" | **Partial** | Content-Type is captured but non-JSON bodies aren't |
| "Path parameter detection and templating" | **Fragile** | Needs multiple observations; fails for single-hit endpoints |
| "Authentication flow documentation" | **Partial** | Detects Bearer/API-key headers but sanitization redacts them |
| "No code changes required" | **Working** | Proxy-based approach works |
| YAML output | **Missing** | Only JSON output; README mentions `swagger.yaml` in `.gitignore` but no YAML generation |
| WebSocket support | **Not implemented** | Listed as future feature |
| gRPC support | **Not implemented** | Listed as future feature |

---

## 5. Test Coverage Gaps

### 5.1 What's Tested

- Unit tests for `parser` package: schema inference, path patterns, schema merging, type inferrer
- Unit tests for `openapi` package: generator creation, spec generation, tag extraction, content type parsing
- Basic proxy integration test

### 5.2 What's NOT Tested

| Gap | Impact |
|---|---|
| **End-to-end flow** (proxy capture → generate → validate output) | No test verifies the full pipeline produces valid OpenAPI |
| **Sanitization round-trip** | No test checks that sanitized data can be correctly deserialized back |
| **Non-JSON content types** | No tests for XML, form-data, binary responses |
| **Large payloads** | No tests for memory behavior with large responses |
| **Concurrent proxy requests** | No tests for race conditions in `FileStorage.Store` |
| **Multiple sessions** | No tests for `GetAll` reading from multiple session files |
| **Error responses** (4xx, 5xx) | No tests verifying error schemas are generated correctly |
| **Empty/null fields** | No tests for APIs returning partial objects |
| **Deeply nested JSON** | No tests for 3+ levels of nesting |
| **Path with query parameters** | No end-to-end test capturing and generating docs with query params |
| **OpenAPI spec validity** | No test validates generated spec against OpenAPI 3.0 schema |
| **YAML output** | Not implemented, not tested |
| **`--cleanup` flag** | No test verifies data directory is cleaned |
| **`--tag-mapping` end-to-end** | No test verifies custom tags appear in output |
| **Duplicate endpoint detection** | No test for same path/method seen multiple times |
| **Graceful degradation** | No test for malformed JSON, empty bodies, missing headers |
| **`Generator` in `generator.go`** | No tests for the wrapper class |
| **`logger` package** | No tests |

---

## 6. Recommendations

### Priority 1: Fix the Core Pipeline

1. **Don't sanitize before storage.** Store raw transactions (optionally encrypted) and sanitize only during generation. Or store a dual representation: raw (for examples) + sanitized (for schema structure).
2. **Fix array handling.** Keep all array elements, or at least a representative sample (e.g., first 5), not just the first one.
3. **Add a real end-to-end test.** Spin up the proxy, send requests, generate docs, validate the output against the OpenAPI 3.0 JSON schema using `kin-openapi`'s validator.

### Priority 2: Improve Path Detection

4. **Heuristic-based single-observation templatization.** For a path like `/users/123`, if the segment `123` is numeric and the previous segment is a known resource (`users`, `products`, etc.), templatize it to `/users/{id}` even with one observation.
5. **Support deeper nesting.** `TemplatizePath` currently only handles `{id}` in position 1. Support arbitrary depth.

### Priority 3: Robustness

6. **Handle non-JSON content types.** At minimum, log that a non-JSON response was encountered and skip it gracefully. Ideally, support XML → JSON Schema conversion.
7. **Implement graceful shutdown** for the proxy server.
8. **Use `http.Server`** instead of `http.ListenAndServe` for timeout configuration and shutdown support.
9. **Fix the release workflow** to only create releases on tag pushes, not PRs.

### Priority 4: Code Quality

10. **Remove dead code.** Either remove `parser.GenerateOpenAPISpec` and `openapi.Generator` or unify them with the active `OpenAPIGenerator`.
11. **Replace deprecated APIs.** `ioutil.ReadFile` → `os.ReadFile`, `strings.Title` → `golang.org/x/text/cases`.
12. **Fix `capitalize`** to handle hyphens/underscores properly.
13. **Don't mark all properties as required.** Only mark a property as required if it appears in ALL observed samples for that endpoint.
14. **Fix `responseWriter`** to implement `http.Flusher` for SSE support.

### Priority 5: Testing

15. **Add OpenAPI schema validation** to tests — validate that generated specs are valid OpenAPI 3.0.
16. **Add concurrent access tests** for `FileStorage`.
17. **Add tests for the `logger` package.**
18. **Add integration tests** that run the full proxy → generate pipeline.

---

## Appendix: File-by-File Summary

| File | Lines | Issues Found |
|---|---|---|
| `cmd/swagdoc/main.go` | 403 | Global state dependency, missing function params |
| `pkg/proxy/proxy.go` | 312 | Sanitization breaks schema inference, no graceful shutdown, memory buffering, missing Flusher |
| `pkg/proxy/storage.go` | 138 | O(n²) writes, session collisions, no file locking |
| `pkg/proxy/proxy_test.go` | 116 | Doesn't test actual proxy, just reimplements logic |
| `pkg/openapi/openapi.go` | 1011 | Dead code, deprecated APIs, base64 heuristic, all-required bug |
| `pkg/openapi/generator.go` | 108 | Dead code, never tested |
| `pkg/openapi/openapi_test.go` | 427 | Good coverage but doesn't test sanitization round-trip |
| `pkg/parser/parser.go` | 473 | Dead code path, duplicate logic |
| `pkg/parser/auth.go` | 270 | Case-sensitivity bug in header detection |
| `pkg/parser/smart_type.go` | 632 | Works well but operates on already-sanitized data |
| `pkg/parser/path_patterns.go` | 299 | Needs 2+ observations, shallow depth |
| `pkg/parser/schema_merger.go` | 302 | Reasonable implementation |
| `pkg/logger/logger.go` | 121 | No tests, cosmetic issues only |
| `.github/workflows/release.yml` | 106 | Triggers releases on PRs |
