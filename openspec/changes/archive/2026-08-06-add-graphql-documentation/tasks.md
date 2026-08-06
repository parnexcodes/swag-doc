## 1. GraphQL document parser

- [x] 1.1 Add `pkg/graphql/parser.go` with a tokenizer: names, strings (including block strings and escape sequences), int/float numbers, punctuation, `#` comments, commas as insignificant whitespace
- [x] 1.2 Add the recursive-descent document parser: operation definitions (query/mutation/subscription, optional name, variable definitions with declared type strings and defaults), selection sets (fields with alias/arguments, fragment spreads, inline fragments), fragment definitions with type conditions, and value literals (int/float/string/boolean/null/enum/list/object) in arguments and defaults; directives are lexed and structurally skipped; recursion depth bounded (design D2)
- [x] 1.3 Parser returns errors for malformed input (callers treat them as "not GraphQL"), and unit-test it: GraphQL-spec example documents, anonymous operations, aliases, nested selections, variable definitions/defaults, fragments, directives, block strings, and adversarial inputs (deep nesting, unterminated constructs, invalid tokens)

## 2. GraphQL transaction detection

- [x] 2.1 Add detection (design D1) in `pkg/graphql`: a transaction is GraphQL when the request body is `application/graphql` and parses as a document, or is JSON/`application/*+json` with a `query` string field that parses as a GraphQL document (keep the parsed `variables` object for input inference), or is `application/x-www-form-urlencoded` with a parseable `query` form field
- [x] 2.2 Unit-test detection: JSON body with `query` field detected, raw `application/graphql` body detected, non-GraphQL JSON (`{"query":"search term"}`) not detected, GET requests without a body not detected

## 3. Response-driven type inference

- [x] 3.1 Map response `data` shapes to SDL types reusing `pkg/parser` `TypeInferrer`/`SchemaMerger` (design D4): objects → named types (PascalCase of the field name), arrays → `[T]`, JSON scalars → `String`/`Int`/`Float`/`Boolean`, string fields named exactly `id` → `ID`, `null` → no type info, collision-free deterministic naming (parent-type suffix on conflict)
- [x] 3.2 Merge types across observations with `SchemaMerger`; skip responses without a `data` member (error-only responses); resolve response keys by field alias; resolve fragment spreads/inline fragments into their target type with a cycle guard
- [x] 3.3 Unit-test inference: nested objects, lists of objects, `id` → `ID`, field merging across observations, fragment-contributed fields, error-only responses contribute nothing, truncated/non-JSON bodies contribute nothing

## 4. SDL generator and serializer

- [x] 4.1 Add `pkg/graphql/generator.go`: operations map to root-type fields (`Query`/`Mutation`/`Subscription`, emitted only when observed), field names from the AST (not aliases or operation names); arguments from variable definitions with declared type strings preserved verbatim, literals typed by inference, object-typed variables → `input` types with fields from the observed `variables` value (zero-field input type when no value was captured); repeated observations merge into one field with each argument name once
- [x] 4.2 Add `pkg/graphql/serialize.go`: deterministic SDL emission — root types, then object types, then input types, all alphabetical; fields and arguments alphabetical; `#` comment header carrying title/description/version flags (no timestamp); value-free output by construction (identifiers and type names only, no enums/examples/literals)
- [x] 4.3 Unit-test generation: variable-typed arguments (`user(id: ID!)`), literal-typed arguments (`user(id: Int)`), mutation with `input UserInput` from variables, alias and anonymous-operation mapping, field merging, deterministic byte-identical output, no captured values in output (distinctive sample values absent), valid SDL structure per golden fixtures

## 5. CLI wiring

- [x] 5.1 Add `--format graphql` branch in `generateDocs` (new `generateGraphQLDocs` reading via `NewFileStorageReader`/`GetAll`, filtering GraphQL transactions, writing SDL text regardless of output extension); update the unknown-format error message to `must be openapi, asyncapi, or graphql`
- [x] 5.2 Update the `generation/asyncapi` "Unknown format is rejected" scenario example from `graphql` to `grpc` in main specs after archive (delta already in this change) and fix any tests referencing `--format graphql` as invalid
- [x] 5.3 Test CLI behavior: `--format graphql` produces a schema file from a data directory with GraphQL traffic, unknown formats error without writing, `--format openapi`/`asyncapi` outputs unchanged, serialization failure leaves no output file, `--cleanup` still works

## 6. Example and docs

- [x] 6.1 Add a minimal hand-rolled `/graphql` endpoint to `examples/simple_api` (query and mutation over users/posts types)
- [x] 6.2 Extend `make demo` to POST a GraphQL query and mutation through the proxy and run `swagdoc generate --format graphql --output schema.graphql`
- [x] 6.3 Update README: `--format` table row for `graphql`, a GraphQL section (detection, SDL output, transport support: POST bodies only; GET and WS-subscriptions unsupported), and remove GraphQL support from Future Features

## 7. Verification

- [x] 7.1 Run the mandatory suite: `go build ./...`, `go test ./...`, `golangci-lint run`, `golangci-lint fmt`
- [x] 7.2 Run `make demo` end to end and confirm `schema.graphql` (SDL containing the observed query/mutation operations) is produced alongside `swagger.json` and `asyncapi.json` from one capture
