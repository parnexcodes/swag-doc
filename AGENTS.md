# Repository Guide

## Toolchain and Verification

- Use Go 1.23; `go.mod` specifies Go 1.23.4 and CI installs Go 1.23.
- Run the CI-equivalent suite with `go test -v ./...`; coverage uses `go test -coverprofile=coverage.out ./...` followed by `go tool cover -func=coverage.out`.
- Run one test with `go test ./pkg/<package> -run '^TestName$' -v` (for example, `go test ./pkg/openapi -run '^TestGenerateSpec$' -v`).
- Build the shipped CLI with `go build -o bin/swagdoc ./cmd/swagdoc` or `make build`. The release workflow instead writes platform binaries in the repository root.
- Format changed Go files with `gofmt`; there is no repository-specific lint or formatter configuration.

## Mandatory Post-Coding Verification

After making any code changes, you MUST run these commands in order and ensure they all pass before considering work complete:

1. **Build:** `go build ./...` — must compile without errors
2. **Test:** `go test ./...` — all tests must pass
3. **Lint:** `golangci-lint run` — must report 0 issues
4. **Format:** `golangci-lint fmt` — must report no formatting changes needed

Never skip any of these steps. If any step fails, fix the issue before moving on.

## Execution Flow

- `cmd/swagdoc/main.go` owns the Cobra CLI and the `proxy` and `generate` workflows.
- Proxy traffic is sanitized before persistence: JSON values become type placeholders, sensitive headers/query parameters are redacted, and arrays retain up to five representative items. Preserve this privacy boundary when changing capture or storage.
- `pkg/proxy/FileStorage` writes each process's transactions to one timestamped `session-*.json` file using append-only writes for performance, and generation reads every JSON file in the selected data directory.
- The CLI generates specs through `pkg/openapi.OpenAPIGenerator`. `pkg/parser/parser.go` also exposes an older standalone `GenerateOpenAPISpec`; do not assume changes there affect CLI output unless the active generator calls the changed helper.
- OpenAPI generation is multi-pass: detect path/auth/type patterns, build operations, then merge schemas across repeated templated endpoints. Test repeated samples and concrete-to-templated paths when changing inference.

## Local Demo and Artifacts

- `make example` runs the sample API on port 3000; `make proxy` builds and proxies port 8888 to it; `make generate` reads `./swagdoc-data` and writes `swagger.json`.
- `make demo` starts background servers, sends sample requests, generates `swagger.json`, and stops the servers. It requires `curl` and free ports 3000 and 8888.
- `make clean` deletes both `bin/` and `swagdoc-data/`. Captured sessions and generated `swagger.json`/`swagger.yaml` are intentionally gitignored.
- The cross-platform release workflow runs on both `v*` tag pushes and pull requests to `master`; actual GitHub releases are intended for tagged runs.
