## Why

GraphQL support is an explicitly listed future feature in the README, and today SwagDoc produces nothing useful for GraphQL APIs: GraphQL traffic (normally POSTs to a single endpoint such as `/graphql` with a JSON body carrying `query`, `variables`, and `operationName`) is captured like any other HTTP transaction, and OpenAPI generation emits one opaque `POST /graphql` entry with a generic sanitized body schema. The actual operations, their arguments, and the shape of the types they return are lost, so teams running SwagDoc in front of GraphQL APIs get no documentation of the API's real surface.

The canonical documentation for a GraphQL API is its schema. The WebSocket work already established the pattern of one dedicated generator per protocol family selected by `--format`, and that flag was explicitly designed to generalize to GraphQL and gRPC. Extending it to emit a GraphQL schema (SDL) from observed traffic is the missing piece.

## What Changes

- **GraphQL transaction detection**: a captured HTTP transaction is recognized as GraphQL when its request body is `application/graphql`, or is JSON/URL-encoded with a `query`/`mutation`/`subscription` field whose value parses as a GraphQL operation document. Non-GraphQL transactions are ignored by the new generator and never affect its output. Detection is body-based only — no path assumptions, and GraphQL-over-GET is out of scope because query parameters are value-sanitized at capture time.
- **New `pkg/graphql` generator**: a hand-rolled GraphQL document parser (no new dependencies, same rationale as the hand-rolled RFC 6455 frame parser) extracts operations, root fields, aliases, arguments (variable definitions and literals), and fragment spreads; response `data` shapes are mapped to SDL types using the existing `pkg/parser` JSON inference, merged across observations.
- **`swagdoc generate --format graphql`** writes a deterministic GraphQL SDL schema document: `type Query`/`Mutation`/`Subscription` root fields per observed operation, arguments with non-null/list wrappers preserved from variable declarations, object-typed variables emitted as `input` types (fields inferred from the observed `variables` values), named object types for response shapes (string fields named `id` map to `ID`), lists, and scalars. Output is plain text regardless of the `--output` extension, contains only names and type structures (never captured values), and is byte-identical for the same input.
- **Existing behavior preserved**: `--format openapi` and `--format asyncapi` output is unchanged (OpenAPI byte-identical — GraphQL transactions remain ordinary HTTP transactions there); unknown `--format` values still error before writing.
- **`generation/asyncapi` spec update**: its "Unknown format is rejected" scenario currently uses `--format graphql` as the invalid example; it is updated to `--format grpc` because `graphql` becomes a valid value.
- **README updated**: GraphQL schema generation documented (generate flag, output format, supported/unsupported GraphQL transport forms) and GraphQL removed from Future Features.

No breaking changes. No new dependencies.

## Capabilities

### New Capabilities

- `generation/graphql`: GraphQL SDL schema generation from captured GraphQL traffic — transaction detection, operation-to-root-field mapping, response-driven type inference, and deterministic, value-free SDL output.

### Modified Capabilities

- `generation/asyncapi`: the `--format` flag requirement now lists `graphql` as a valid format, and the unknown-format scenario's example changes from `graphql` to `grpc`.
