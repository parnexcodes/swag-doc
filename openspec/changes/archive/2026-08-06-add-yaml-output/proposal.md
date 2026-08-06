## Why

YAML output is an explicitly listed future feature in the README and a common request for OpenAPI tooling: YAML specs are easier to review in diffs, read by many CI and documentation pipelines, and preferred by teams that hand-edit generated docs. Today `swagdoc generate` can only emit JSON, forcing users to convert externally.

## What Changes

- `swagdoc generate` selects the output format from the `--output` file extension: `.yaml` or `.yml` (case-insensitive) produces YAML; any other extension produces JSON, preserving current behavior exactly.
- YAML serialization converts the generated spec (an `openapi3.T` from kin-openapi, JSON-tagged) via a JSON round-trip into `map[string]any` and marshals it with `gopkg.in/yaml.v3`.
- Adds `gopkg.in/yaml.v3` as a new dependency.
- Serialization errors are reported and no output file is written, matching the current JSON error path.
- README updated: YAML output documented in the generate command section and removed from Future Features.
- No breaking changes: existing invocations without a `.yaml`/`.yml` output path produce byte-identical JSON.

## Capabilities

### New Capabilities

- `generation/yaml-output`: Format selection from the output file extension, YAML serialization of the generated OpenAPI spec, and JSON/YAML equivalence of generated content.

### Modified Capabilities

None.
