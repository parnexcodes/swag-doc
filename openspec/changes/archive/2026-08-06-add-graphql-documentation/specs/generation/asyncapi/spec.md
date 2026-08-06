## MODIFIED Requirements

### Requirement: AsyncAPI format is selected by a --format flag

`swagdoc generate` MUST accept a `--format` flag selecting the generator: `openapi` (the default, preserving current behavior exactly), `asyncapi`, or `graphql`. Any other value MUST produce an error.

#### Scenario: AsyncAPI output from --format asyncapi
- **WHEN** the user runs `swagdoc generate --format asyncapi --output asyncapi.json` against a data directory containing captured WebSocket sessions
- **THEN** the file `asyncapi.json` is created and parses as an AsyncAPI document

#### Scenario: OpenAPI remains the default format
- **WHEN** the user runs `swagdoc generate` without `--format`
- **THEN** the output is an OpenAPI document identical to previous behavior

#### Scenario: Unknown format is rejected
- **WHEN** the user runs `swagdoc generate --format grpc`
- **THEN** the command exits with an error and no output file is created
