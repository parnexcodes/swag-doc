## Purpose

The documentation generator can emit an AsyncAPI document describing the WebSocket endpoints and messages observed in captured traffic, so socket-based APIs get standard, tool-consumable documentation alongside the existing OpenAPI output.

## Requirements

### Requirement: AsyncAPI format is selected by a --format flag

`swagdoc generate` MUST accept a `--format` flag selecting the generator: `openapi` (the default, preserving current behavior exactly) or `asyncapi`. Any other value MUST produce an error.

#### Scenario: AsyncAPI output from --format asyncapi
- **WHEN** the user runs `swagdoc generate --format asyncapi --output asyncapi.json` against a data directory containing captured WebSocket sessions
- **THEN** the file `asyncapi.json` is created and parses as an AsyncAPI document

#### Scenario: OpenAPI remains the default format
- **WHEN** the user runs `swagdoc generate` without `--format`
- **THEN** the output is an OpenAPI document identical to previous behavior

#### Scenario: Unknown format is rejected
- **WHEN** the user runs `swagdoc generate --format graphql`
- **THEN** the command exits with an error and no output file is created

### Requirement: Channels are derived from captured WebSocket sessions

Each distinct WebSocket endpoint observed in the captured sessions MUST become one AsyncAPI channel, keyed by a channel identifier derived from the endpoint, with the templated endpoint path as the channel `address`. Endpoint paths MUST be templated with the same path-pattern inference used for OpenAPI (numeric IDs and UUIDs become `{param}` segments), so repeated sessions against the same resource merge into a single channel.

#### Scenario: Repeated sessions merge into one templated channel
- **WHEN** captured sessions contain upgrades to `/chat/123` and `/chat/456` (numeric IDs) and none to other `/chat/*` paths
- **THEN** the document contains a single channel with address `/chat/{id}` describing both sessions, with the `{id}` path parameter declared

#### Scenario: Distinct endpoints produce distinct channels
- **WHEN** captured sessions contain upgrades to `/chat/123` and `/notifications`
- **THEN** the document contains separate channels with addresses `/chat/{id}` and `/notifications`

### Requirement: Operations and messages are inferred from captured frames

The document MUST declare a top-level `operations` map matching the observed directions: messages sent by the client MUST appear as an operation with `action: receive` (the documented application receives them) and messages sent by the server MUST appear as an operation with `action: send`, each referencing its channel and listing the messages it handles. Message objects MUST live in `components.messages` and carry a payload schema inferred from the observed payloads: JSON payloads produce typed schemas (merged across observations like OpenAPI response schemas), and non-JSON payloads produce a `string` or `binary` schema matching their content type. Message names MUST be deterministic for the same capture input.

#### Scenario: Both directions produce send and receive operations
- **WHEN** a session records client-to-server messages and server-to-client messages on the same channel
- **THEN** the document declares a `receive` operation for the client-to-server messages and a `send` operation for the server-to-client messages, each referencing the channel and listing its messages with payload schemas

#### Scenario: JSON payloads produce typed schemas
- **WHEN** multiple JSON messages with the same structure are observed
- **THEN** their payload schema is a typed object schema merged from the observations, consistent with the OpenAPI generator's schema inference

#### Scenario: Binary payloads produce a binary schema
- **WHEN** a captured message has a non-JSON binary payload
- **THEN** its payload schema is a `binary` schema with the observed content type

### Requirement: Generated document is valid AsyncAPI 3.1.0

The AsyncAPI output MUST be a single AsyncAPI 3.1.0 document: it parses as valid JSON (or YAML) containing an `asyncapi` field with value `3.1.0`, an `info` section carrying the title, description, and version from the generate flags, `channels` with the derived channels, `operations` referencing them, and shared messages and schemas under `components`. YAML output follows the same `--output` extension rules as OpenAPI generation (`.yaml`/`.yml` produce YAML, everything else JSON).

#### Scenario: JSON output parses as AsyncAPI 3.1.0
- **WHEN** an AsyncAPI JSON output file is parsed
- **THEN** it contains `asyncapi: "3.1.0"`, `info`, `channels`, `operations`, and `components` holding the generated payload schemas and messages

#### Scenario: YAML output for .yaml paths
- **WHEN** the user runs `swagdoc generate --format asyncapi --output asyncapi.yaml`
- **THEN** the file is created and parses as a single YAML document containing the same AsyncAPI content

#### Scenario: Serialization failure writes no partial file
- **WHEN** serialization of the AsyncAPI document fails for any reason
- **THEN** the command exits with an error and no output file is created or modified

### Requirement: Security schemes are detected from handshake authentication

Authentication observed in handshake requests (Bearer tokens, API keys in headers or query parameters) MUST be expressed as AsyncAPI security schemes in `components.securitySchemes`, applied to the operations whose channels' handshakes carried them.

#### Scenario: Bearer handshake auth produces a security scheme
- **WHEN** captured handshakes carry `Authorization: Bearer <token>`
- **THEN** the document contains a `bearer` security scheme and the affected operations reference it

### Requirement: WebSocket captures do not affect OpenAPI output

Running OpenAPI generation over a data directory that contains WebSocket session files MUST produce output identical to running it over the same directory without those files.

#### Scenario: Mixed data directory yields identical OpenAPI output
- **WHEN** the user runs `swagdoc generate` (OpenAPI format) on a directory containing both `session-*.json` and `ws-session-*.json` files, and again on the same HTTP sessions without the WebSocket files
- **THEN** both runs produce byte-identical output
