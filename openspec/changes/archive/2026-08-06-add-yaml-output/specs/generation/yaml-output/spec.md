## Purpose

The documentation generator can emit the OpenAPI specification in YAML as well as JSON, with the format selected by the output file extension, so teams can produce YAML specs for diff-friendly review and tooling without external conversion.

## ADDED Requirements

### Requirement: Output format is selected by file extension

`swagdoc generate` MUST serialize the generated OpenAPI spec as YAML when the `--output` path ends in `.yaml` or `.yml` (case-insensitive) and as JSON for any other output path. The default output path (`swagger.json`) MUST continue to produce JSON.

#### Scenario: YAML output from .yaml path
- **WHEN** the user runs `swagdoc generate --output swagger.yaml` against a data directory with captured transactions
- **THEN** the file `swagger.yaml` is created and parses as valid YAML containing the generated OpenAPI spec

#### Scenario: YAML output from .yml path
- **WHEN** the user runs `swagdoc generate --output docs.yml`
- **THEN** the file `docs.yml` is created and parses as valid YAML containing the generated OpenAPI spec

#### Scenario: Uppercase extension is recognized
- **WHEN** the user runs `swagdoc generate --output SWAGGER.YAML`
- **THEN** the output file is serialized as YAML

#### Scenario: JSON remains the default format
- **WHEN** the user runs `swagdoc generate --output api.json` or `swagdoc generate --output api-docs` (no YAML extension)
- **THEN** the output file is serialized as JSON, identical to previous behavior

### Requirement: YAML output is equivalent to JSON output

For the same captured transactions, the generated YAML spec MUST describe the same OpenAPI document as the generated JSON spec: identical info, paths, operations, parameters, schemas, and examples; only the serialization format differs.

#### Scenario: YAML and JSON describe the same document
- **WHEN** generation runs twice on the same data directory, once with `--output spec.json` and once with `--output spec.yaml`
- **THEN** parsing `spec.yaml` as YAML and parsing `spec.json` as JSON yields structurally equal documents

### Requirement: Serialization failures do not write a partial file

If YAML or JSON serialization of the generated spec fails, the command MUST report an error and MUST NOT create or overwrite the output file.

#### Scenario: Failed serialization leaves no output file
- **WHEN** serialization of the spec fails for any reason during `swagdoc generate --output spec.yaml`
- **THEN** the command exits with an error and no `spec.yaml` file exists or is modified

### Requirement: Generated YAML is valid OpenAPI YAML

The YAML output MUST be a single YAML document that, when parsed, contains an `openapi` version field of `3.0.x` and the `paths` field with the generated operations.

#### Scenario: Output parses as an OpenAPI 3.0 document
- **WHEN** a YAML output file is parsed and inspected
- **THEN** it contains an `openapi` field starting with `3.0` and a `paths` object listing the generated endpoints
