## 1. Dependency

- [x] 1.1 Add `gopkg.in/yaml.v3` to `go.mod` via `go get gopkg.in/yaml.v3`

## 2. Serialization in pkg/openapi

- [x] 2.1 Add `Format` type with `FormatJSON`/`FormatYAML` constants and exported `MarshalSpec(spec *OpenAPISpec, format Format) ([]byte, error)` in `pkg/openapi`; move the existing `json.MarshalIndent(spec, "", "  ")` logic into the JSON branch
- [x] 2.2 Implement the YAML branch: `json.Marshal` the spec, `json.Unmarshal` into `map[string]any`, then `yaml.Marshal` (yaml.v3); return the bytes without touching the filesystem

## 3. CLI wiring in cmd/swagdoc

- [x] 3.1 In `generateDocs` (`cmd/swagdoc/main.go`), derive the format from `strings.ToLower(filepath.Ext(output))`: `.yaml`/`.yml` → `FormatYAML`, otherwise `FormatJSON`
- [x] 3.2 Replace the inline `json.MarshalIndent` call with `openapi.MarshalSpec(spec, format)`; keep marshal-before-write ordering so failures never create/overwrite the output file

## 4. Tests

- [x] 4.1 Add `pkg/openapi` unit tests: YAML output parses as a single YAML document with `openapi: 3.0.x` and `paths`; JSON and YAML outputs of the same spec are structurally equal when compared via canonical form (re-marshal both parsed docs to JSON and compare)
- [x] 4.2 Add/extend `cmd/swagdoc` tests: `generate --output <name>.yaml` and `.yml` produce parseable YAML; `--output <name>.json` output is byte-identical to previous JSON behavior; uppercase `.YAML` is treated as YAML
- [x] 4.3 Add a test that a serialization failure propagates as an error before any file write (no partial/empty output file created)

## 5. Documentation

- [x] 5.1 Update README: note in the Generate Command section that `.yaml`/`.yml` output paths produce YAML; remove "YAML output" from Future Features

## 6. Verification

- [x] 6.1 Run `go build ./...` — must compile
- [x] 6.2 Run `go test ./...` — all tests pass
- [x] 6.3 Run `golangci-lint run` — 0 issues
- [x] 6.4 Run `golangci-lint fmt` — no formatting changes needed
- [x] 6.5 Smoke test end to end: capture a session (e.g. `make demo` or proxy + sample requests), run `swagdoc generate --output swagger.yaml`, and confirm the file parses as valid YAML describing the expected endpoints
