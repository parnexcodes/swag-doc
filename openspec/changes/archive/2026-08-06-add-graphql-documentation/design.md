## Context

Current state that shapes this design (motivation in `proposal.md`):

- The proxy stores request bodies raw (`RequestData.Body`), but query parameters are value-sanitized at capture time (`sanitizeQueryParams` replaces values with type placeholders like `__integer__`). GraphQL-over-GET query text is therefore unrecoverable; only body-carrying requests (POST) can be detected.
- Generation reads through the read-only `proxy.NewFileStorageReader`; `GetAll()` returns HTTP transactions only (already skips `ws-session-*.json`). No storage changes are needed — GraphQL rides ordinary HTTP transactions.
- `generateDocs` in `cmd/swagdoc/main.go` branches on `--format` (`openapi` | `asyncapi`) and errors for unknown values before writing anything; `--cleanup` is handled after generation.
- `pkg/parser` provides reusable JSON inference: `TypeInferrer` (smart typing with format detection: uuid/email/date/datetime/…), `SchemaMerger` (merge observations), and the `Schema` model (type/properties/items/required/…). The AsyncAPI generator already reuses these, so SDL type inference should too.
- The project's established pattern for protocol families is a dedicated hand-rolled generator package with zero new dependencies (`pkg/asyncapi`, RFC 6455 frame parser in `pkg/proxy/websocket.go`), deterministic output, and golden-style tests.

## Goals / Non-Goals

**Goals:**
- Detect GraphQL transactions purely from captured request bodies, without path assumptions.
- A standalone `pkg/graphql` generator: hand-rolled GraphQL document parser + SDL emitter, reusing `pkg/parser` for response-shape inference.
- Deterministic, value-free SDL output; `--format graphql` wired into the existing CLI switch with unknown formats still rejected.
- Zero new dependencies; OpenAPI/AsyncAPI behavior untouched.

**Non-Goals:**
- GraphQL-over-GET: query params are destroyed at capture; changing redaction would weaken capture guarantees.
- GraphQL-over-WebSocket subscriptions: raw frames are AsyncAPI territory; SDL is built from HTTP transaction bodies only.
- Introspection-based schema extraction (requires a live server; SwagDoc works offline from traffic).
- Full GraphQL language coverage: directives are skipped, and interfaces, unions, enums, and custom scalars are never inferred (only what is observed: named object types, input types, lists, scalars).
- Non-null inference on response-observed fields (one observation cannot prove non-null; only variable-declared types carry `!`).
- Changing OpenAPI output for GraphQL endpoints.

## Decisions

### D1: Detection is body-content heuristics, not paths

A transaction is GraphQL iff its request body is:
- `Content-Type: application/graphql` — the raw body must parse as a GraphQL document; or
- `application/json` (or `application/*+json`) — parse JSON; a string field named `query` (the GraphQL-over-HTTP wire field for all operation kinds) must parse as a GraphQL document; the `variables` JSON object is kept for input-type inference; or
- `application/x-www-form-urlencoded` — a `query` form field must parse as a document (cheap, spec-supported).

The document parse is the gate: a REST body like `{"query":"search term"}` fails to parse (no operation keyword, invalid syntax) and is not GraphQL. No path matching (`/graphql` can live anywhere). Alternatives rejected: path/endpoint heuristics (wrong for GraphQL at arbitrary paths) and content-type alone (GraphQL is almost always `application/json`).

### D2: Hand-rolled GraphQL document parser in `pkg/graphql`

New package `pkg/graphql` with a tokenizer (names, strings incl. block strings, int/float numbers, punctuation, `#` comments, commas as insignificant) and a recursive-descent parser producing a small AST:

```go
type Document struct{ Operations []*Operation; Fragments []*Fragment }
type Operation struct{ Kind, Name string; Variables []*VariableDef; SelectionSet []*Field }
type VariableDef struct{ Name, Type string; HasDefault bool } // Type: raw type string, e.g. "ID!", "[String!]!"
type Field struct{ Alias, Name string; Arguments []*Argument; SelectionSet []*Field; FragmentSpreads []string; InlineFragments []*InlineFragment }
type Argument struct{ Name string; Value Value } // Value: literal or *VariableRef
```

Parse errors are returned, not fatal: a transaction that fails to parse is simply not GraphQL. Directives (`@skip(if: …)`) are lexed and structurally skipped (balanced parens), never evaluated. Depth is bounded (e.g. 100) to stay robust on adversarial bodies. Alternatives rejected: `graphql-go/graphql` and `vektah/gqlparser` dependencies (full server/client stacks we don't need; the repo's precedent is hand-rolled parsers, and our need — extract structure, never execute or validate — is small), and a regex-based extractor (breaks on nested braces, strings, and comments).

### D3: Operations map to root-type fields; arguments from variable definitions plus literals

- Each operation contributes its top-level fields to `type Query` / `type Mutation` / `type Subscription`; a root type is emitted only when an operation of that kind was observed. The field name is the AST field name — the alias is used only to match response keys (D4), and the operation name is not used in SDL at all.
- Arguments: variable definitions are the primary source. Declared type strings are carried through verbatim (`$id: ID!` → `id: ID!`, `$tags: [String!]` → `tags: [String!]`), preserving non-null/list wrappers the API actually declares. Literal arguments (`user(id: 1)`) add arguments absent from variable definitions, typed via the same JSON smart-typing used for responses (`1` → `Int`, `"x"` → `String`, `[1,2]` → `[Int]`, `null` literal → no type info, skipped). A variable definition and a literal for the same name → the declaration wins (authoritative). Object-typed variable declarations (`$input: UserInput!`) produce SDL `input` types — required, since SDL forbids object types as argument types — with fields inferred from the matching `variables` object value in the request body (nested objects → nested input types); with no observed variables value, the input type is emitted with zero fields and enriched by later observations.
- Merging: same root field across observations → one field; each argument name appears once (same name+type merges; conflicting inferred types resolve first-observed-wins, recorded in the code comment). Arguments are emitted in alphabetical order (stable across capture orders, see D5).

### D4: Response `data` drives type structure; reuse `pkg/parser`

Only 2xx responses whose body parses as JSON with a `data` object contribute. `{"errors": …}` without `data` is skipped entirely. For each root field of the parsed operations, look up `data[alias]` (alias resolution is required — the response is keyed by alias, e.g. `{ u: user }` returns `data.u`) and infer the return type from the value shape using `TypeInferrer`/`SchemaMerger`:

- object → named SDL object type; name = PascalCase of the field name (`user` → `User`, `getPosts` → `GetPosts`); properties become fields recursively.
- array → `[T]`; element object → named type as above.
- JSON scalars → `String` / `Int` (integral) / `Float` / `Boolean`; `null` → no type info (the field is omitted until a typed observation arrives).
- string fields named exactly `id` → `ID` (common GraphQL convention; conservative, only the exact name).
- Name collisions (two shapes → same derived name) disambiguate deterministically by appending the parent type name (e.g. `Post` vs `UserPosts`).
- Merging across observations uses the existing `SchemaMerger` rules (union of properties; conflict resolution consistent with OpenAPI output), so a type observed with disjoint fields converges to the union.
- Fragments: a fragment definition's selection set contributes its fields to the type named by its type condition (`fragment userFields on User { … }` → `User` gains those fields), and fragment spreads/inline fragments resolve the same way. The response body is flat (fragments are already expanded server-side), so field matching is by name. Cycle guard: visited-fragment set + depth bound.
- Response-observed fields are always emitted nullable; `!` appears only where the observed documents declared it (D3). Truncated/oversized response bodies that fail JSON parse simply contribute nothing (same envelope as OpenAPI inference).

### D5: SDL serialization is deterministic and value-free

The serializer emits standard SDL text: `type Query { … }`, `input UserInput { … }`, named object types, list/non-null wrappers. Ordering: root types (`Query`, `Mutation`, `Subscription`), then object types, then input types — alphabetical within each group; fields and arguments alphabetical. No timestamps anywhere. Output is plain text regardless of the `--output` extension (`.yaml`/`.yml` triggers no YAML conversion — SDL is not YAML; a `.graphql`/`.gql` extension is conventional but not required). A short `#` comment header (GraphQL comments are ignored tokens, so the document stays valid) carries the title/description/version flags, which SDL otherwise cannot express; no timestamp, so determinism holds. The serializer emits only identifiers and type names — captured values never reach output by construction (no enums, no examples, no literals).

### D6: CLI wiring

- `generateDocs` gains `case "graphql": genErr = generateGraphQLDocs(absOutput, dataDir, title, description, version)`; the unknown-format error message becomes `must be openapi, asyncapi, or graphql`. `--cleanup` and the read-only storage path (`NewFileStorageReader` + `GetAll`) are shared with the OpenAPI path; GraphQL transactions are filtered after load via D1 and fed to the generator.
- The `generation/asyncapi` spec's "Unknown format is rejected" scenario example changes from `graphql` to `grpc` (see `specs/generation/asyncapi/spec.md`).

## Risks / Trade-offs

- **Detection false positives**: a REST endpoint whose JSON `query` field happens to be valid GraphQL syntax would be documented. → The parse gate requires a full valid document (operation keyword, valid grammar); in practice REST search strings do not parse. Covered by a spec scenario.
- **Hand-rolled parser correctness**: subtle GraphQL grammar (block strings, nested lists/objects in arguments, Unicode escapes, comments inside strings). → Unit tests against GraphQL-spec examples plus adversarial inputs (deep nesting, huge strings, unterminated constructs); bounded depth and length.
- **SDL validity without a reference parser**: empty object/input types and empty argument lists are grammatically valid but may confuse strict external tooling. → Emit minimal-but-valid constructs; golden fixtures reviewed by hand; documented limitation.
- **Conservative typing**: response fields are always nullable and only `id` strings become `ID`, so the schema under-represents the API vs. introspection. → Accepted: SwagDoc documents observed traffic; non-null claims from single observations would be misleading.
- **`input` types with no fields** (variables value not captured, e.g. truncated request body): valid SDL but unhelpful until enriched. → Later observations with variables values enrich them; noted in README.
- **Oversized responses** lose type info for that observation. → Same envelope as OpenAPI generation (capture limits are shared); schema merges whatever parses.
- **GraphQL-over-GET and WS subscriptions are invisible** to this generator. → Documented in README as unsupported transport forms; AsyncAPI output still covers WS traffic.

## Open Questions

None.
