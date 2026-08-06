# Repository Guide

## Toolchain and Verification

- Use Go 1.26; `go.mod` specifies Go 1.26.5 and CI installs via `go-version-file`.
- Run the CI-equivalent suite with `go test -v ./...`; coverage uses `go test -coverprofile=coverage.out ./...` followed by `go tool cover -func=coverage.out`.
- Run one test with `go test ./pkg/<package> -run '^TestName$' -v` (for example, `go test ./pkg/openapi -run '^TestGenerateSpec$' -v`).
- Build the shipped CLI with `go build -o bin/swagdoc ./cmd/swagdoc`. The release workflow uses GoReleaser for cross-platform binaries.
- Lint with `golangci-lint run` and format with `golangci-lint fmt`.

## Mandatory Post-Coding Verification

After making any code changes, you MUST run these commands in order and ensure they all pass before considering work complete:

1. **Build:** `go build ./...` — must compile without errors
2. **Test:** `go test ./...` — all tests must pass
3. **Lint:** `golangci-lint run` — must report 0 issues
4. **Format:** `golangci-lint fmt` — must report no formatting changes needed

Never skip any of these steps. If any step fails, fix the issue before moving on.

## Execution Flow

- `cmd/swagdoc/main.go` owns the Cobra CLI and the `proxy` and `generate` workflows.
- Proxy stores raw transaction bodies on disk; sensitive headers/query parameters are redacted at capture time.
- `pkg/proxy/FileStorage` writes each process's transactions to one timestamped `session-*.json` file using append-only writes for performance, and generation reads every JSON file in the selected data directory.
- The CLI generates specs through `pkg/openapi.OpenAPIGenerator`. Sanitization happens during generation, not at capture time.
- OpenAPI generation is multi-pass: detect path/auth/type patterns, build operations, then merge schemas across repeated templated endpoints. Test repeated samples and concrete-to-templated paths when changing inference.

## Local Demo and Artifacts

- `make example` runs the sample API on port 3000; `make proxy` builds and proxies port 8888 to it; `make generate` reads `./swagdoc-data` and writes `swagger.json`.
- `make demo` starts background servers, sends sample requests, generates `swagger.json`, and stops the servers. It requires `curl` and free ports 3000 and 8888.
- `make clean` deletes both `bin/` and `swagdoc-data/`. Captured sessions and generated `swagger.json`/`swagger.yaml` are intentionally gitignored.
- The release workflow uses GoReleaser, triggered on `v*` tag pushes only.
