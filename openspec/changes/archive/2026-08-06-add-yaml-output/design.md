## Context

See proposal.md — Why. Current state: `generateDocs` in `cmd/swagdoc/main.go` always serializes the spec (`OpenAPISpec`, an alias for `openapi3.T` from kin-openapi) with `json.MarshalIndent(spec, "", "  ")` and writes the bytes to `--output` (default `swagger.json`). The spec struct carries only `json:` tags. `swagger.yaml` is already gitignored, so the new output file needs no repo hygiene changes.

## Goals / Non-Goals

**Goals**

- Emit YAML for `--output` paths ending in `.yaml`/`.yml` (case-insensitive); JSON everywhere else, byte-identical to today.
- Serialization is testable without running the CLI.
- YAML content is structurally equivalent to the JSON content.

**Non-Goals**

- No new CLI flag for format selection (see Decisions).
- No hand-authored YAML templates; the YAML is a pure serialization of the same spec object.
- No changes to spec inference, sanitization, or capture.

## Decisions

### D1: Select format by output file extension

`generateDocs` computes the format from `strings.ToLower(filepath.Ext(output))`: `.yaml`/`.yml` → YAML, otherwise JSON.

- Alternative: a `--format json|yaml` flag. Rejected: it duplicates the extension signal, adds surface to the flag table and README, and creates a conflict policy (flag vs extension) with no real use case — the output is always a file, so the extension is unambiguous.
- Alternative: default YAML for all outputs. Rejected: breaking change for existing users.
- Assumption recorded: users who want YAML will name the file `*.yaml`/`*.yml`; the README documents this.

### D2: Serialize via JSON round-trip into `map[string]any`, then marshal with `gopkg.in/yaml.v3`

Steps for the YAML path: `json.Marshal(spec)` → `json.Unmarshal` into `map[string]any` → `yaml.Marshal` (yaml.v3), then write bytes.

- Why: `openapi3.T` is third-party with only `json:` tags. Marshaling it directly with yaml.v3 would emit Go field names (`Info`, `Components`) instead of OpenAPI keys (`info`, `components`), and would not honor `omitempty`. The JSON round-trip reuses the exact tag contract that produces today's output, so the YAML is the same document in a different format.
- Alternative: add `yaml:` tags to the spec struct. Impossible — the type lives in kin-openapi.
- Alternative: `sigs.k8s.io/yaml`. Rejected: it is a JSON↔YAML converter built on yaml.v2, pulls in Kubernetes module dependencies, and exists primarily for k8s manifests. yaml.v3 is the de-facto standard Go YAML library, is maintained, and has zero transitive dependencies.
- Note: yaml.v3 sorts map keys on marshal, and `json.MarshalIndent` sorts them too, so both formats are deterministic for a given spec — YAML diffs stay stable across runs.

### D3: Put serialization in `pkg/openapi`, keep `cmd/swagdoc` thin

Add to `pkg/openapi` an exported function, e.g. `MarshalSpec(spec *OpenAPISpec, format Format) ([]byte, error)` with `Format` constants (`FormatJSON`, `FormatYAML`); the existing JSON marshal moves inside it. `generateDocs` picks the format and calls it.

- Why: `pkg/openapi` already owns spec construction and its tests assert serialized output; a unit test there can assert JSON/YAML equivalence without spawning the CLI. `cmd/swagdoc` keeps doing I/O and flag handling only.
- Alternative: inline the round-trip in `cmd/swagdoc/main.go`. Rejected: untestable except through CLI integration tests, and it buries serialization logic next to flag plumbing.

### D4: Marshal before writing; errors abort without touching the file

Both formats marshal to an in-memory `[]byte` first and only then `os.WriteFile` — identical to the current JSON path. A marshal error returns the error up and no file is created or overwritten (satisfies "Serialization failures do not write a partial file").

### D5: New dependency: `gopkg.in/yaml.v3`

Added via `go get gopkg.in/yaml.v3`; recorded in `go.mod`. No version pinning beyond go.mod's normal require entry.

## Risks / Trade-offs

- [Numeric type drift in equivalence tests] JSON round-trip yields `float64` for numbers; yaml.v3 parses integers as `int`. A naive `reflect.DeepEqual` between parsed YAML and parsed JSON documents fails on `100` vs `100.0`. → Mitigation: the equivalence test compares canonical forms (re-marshal both parsed documents to JSON and compare strings) rather than raw Go types.
- [YAML of pathological values] Very large or control-character-laden example bodies could produce quoted/block scalars. → Mitigation: yaml.v3 emits valid YAML for all strings; the parse-back equivalence test covers this.
- [Cosmetic differences in emitted YAML vs hand-written conventions] Some teams prefer `on`/`off` or specific quoting. → Accepted: output is machine-generated; consumers parse it, they don't style it.
- [Spec grows and round-trip cost matters] One extra marshal+unmarshal of the spec object. → Negligible: the spec is small relative to the captured corpus that generation already processes.

## Migration Plan

- No migration: additive feature; default output unchanged.
- Rollback: revert the extension check and remove the yaml.v3 require from `go.mod`; JSON path is untouched.
- Docs: README generate-command section gains a note that `.yaml`/`.yml` outputs are YAML; "YAML output" is removed from Future Features.

## Open Questions

- Should a `--format` override flag be added later for outputs whose extension is neither `.yaml`/`.yml` nor JSON? Deferrable — the extension signal covers the file-output use case; the spec (D1's behavior) does not need to change if the flag is added later.
