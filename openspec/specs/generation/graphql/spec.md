# GraphQL Specification

## Purpose

The documentation generator can emit a GraphQL schema (SDL) describing the queries, mutations, and types observed in captured GraphQL traffic, so GraphQL APIs get standard, tool-consumable documentation alongside the existing OpenAPI and AsyncAPI output.

## Requirements

### Requirement: GraphQL format is selected by a --format flag

`swagdoc generate` MUST accept a `--format graphql` value that produces a GraphQL SDL schema document from the captured traffic. The `openapi` and `asyncapi` formats MUST behave exactly as before this change, and any other `--format` value MUST produce an error without creating or modifying an output file.

#### Scenario: SDL output from --format graphql
- **WHEN** the user runs `swagdoc generate --format graphql --output schema.graphql` against a data directory containing captured GraphQL transactions
- **THEN** the file `schema.graphql` is created and contains a GraphQL SDL schema describing the observed operations, with a `type Query` definition

#### Scenario: Other formats are unchanged
- **WHEN** the user runs `swagdoc generate` (OpenAPI) and `swagdoc generate --format asyncapi` on the same data directory
- **THEN** both outputs are identical to what the tool produced before this change, with the OpenAPI output byte-identical

#### Scenario: Unknown format is rejected
- **WHEN** the user runs `swagdoc generate --format grpc`
- **THEN** the command exits with an error and no output file is created

### Requirement: GraphQL transactions are detected from captured traffic

A captured HTTP transaction MUST be treated as a GraphQL transaction when its request body is delivered with the `application/graphql` content type, or is JSON or URL-encoded and carries a `query`, `mutation`, or `subscription` field whose value parses as a GraphQL operation document. Transactions that are not GraphQL MUST be ignored by the generator and MUST NOT contribute anything to the schema.

#### Scenario: JSON body with a query field is detected
- **WHEN** a captured transaction has `Content-Type: application/json` and a body of `{"query":"query GetUser { user { id } }","variables":{"id":1}}`
- **THEN** the transaction is treated as GraphQL and its operation contributes a `user` field to the generated schema

#### Scenario: application/graphql body is detected
- **WHEN** a captured transaction has `Content-Type: application/graphql` and the raw body `query GetUser { user { id } }`
- **THEN** the transaction is treated as GraphQL and contributes a `user` field to the generated schema

#### Scenario: Non-GraphQL JSON bodies are not detected
- **WHEN** a captured transaction has a JSON body with a `query` string that is not a GraphQL document, such as `{"query":"search term"}`
- **THEN** the transaction is not treated as GraphQL and the generated schema is unaffected by it

#### Scenario: REST traffic never appears in the schema
- **WHEN** the data directory contains both GraphQL transactions and ordinary REST transactions
- **THEN** the generated schema contains only content derived from the GraphQL transactions

### Requirement: Operations become root-type fields

Each observed operation MUST contribute its top-level fields to the SDL root type matching its operation kind — `type Query` for queries, `type Mutation` for mutations, `type Subscription` for subscriptions — with each root type emitted only when an operation of that kind was observed. Fields MUST be named by the underlying field name, not by an alias or the operation name. Arguments MUST come from the operation's variable definitions with their declared types preserved (including non-null and list wrappers) and from literal argument values typed by inference. Object-typed variables MUST be emitted as SDL `input` types whose fields are inferred from the observed `variables` values. Repeated observations of the same root field MUST merge into a single field, with each argument name appearing once.

#### Scenario: Variable-defined arguments produce typed parameters
- **WHEN** a captured query is `query GetUser($id: ID!) { user(id: $id) { id name } }`
- **THEN** the schema contains `type Query` with a `user(id: ID!)` field whose return type is a named object type with `id` and `name` fields

#### Scenario: Mutations produce a Mutation root type
- **WHEN** a captured mutation is `mutation CreateUser($input: UserInput!) { createUser(input: $input) { id } }` with variables `{"input":{"username":"alice"}}`
- **THEN** the schema contains `type Mutation` with a `createUser(input: UserInput!)` field, and an `input UserInput` type with a `username` field

#### Scenario: Literal arguments are typed by inference
- **WHEN** a captured anonymous query is `query { user(id: 1) { id } }`
- **THEN** the schema contains `user(id: Int)` on `type Query`

#### Scenario: Aliased and anonymous operations still map to the underlying field
- **WHEN** a captured query is `query { u: user { id } }`
- **THEN** the schema contains a `user` field on `type Query`, named for the underlying field rather than the alias

#### Scenario: Repeated observations merge into one field
- **WHEN** the same root field is observed multiple times with different argument sets
- **THEN** the schema contains one field carrying the union of the observed arguments

### Requirement: Response data defines field and type structure

The value of `data.<rootField>` in a successful GraphQL response MUST determine the return type of the corresponding root field, with object shapes emitted as named SDL object types (recursively), arrays as `[T]`, and JSON scalars mapped to `String`, `Int`, `Float`, or `Boolean`. String-typed fields named `id` MUST map to the `ID` scalar. Types observed for the same field or type MUST merge across observations, and fragment spreads and inline fragments MUST contribute the referenced fields to the fragment's target type. Responses without a `data` member MUST NOT contribute type information.

#### Scenario: Nested objects produce named types
- **WHEN** a captured response contains `{"data":{"user":{"id":1,"name":"alice","posts":[{"title":"hello"}]}}}`
- **THEN** the schema contains a `user` field returning a named `User` type with `id`, `name`, and `posts` fields, plus a `Post` type with a `title` field, with `posts` declared as a list of `Post`

#### Scenario: String id fields map to the ID scalar
- **WHEN** a captured response contains a string value under a field named `id`
- **THEN** that field is declared with the `ID` scalar type

#### Scenario: Observations merge into a single type
- **WHEN** two responses for the same root field carry disjoint fields on the same nested type
- **THEN** the schema contains one type with the union of the observed fields

#### Scenario: Fragment spreads contribute fields
- **WHEN** a captured document is `query { user { ...userFields } } fragment userFields on User { id name }`
- **THEN** the generated `User` type contains `id` and `name` fields

#### Scenario: Error-only responses contribute nothing
- **WHEN** a captured response is `{"errors":[{"message":"unauthorized"}]}` with no `data` member
- **THEN** the schema gains no fields or types from that response

### Requirement: Output is deterministic and contains no captured values

For the same captured data, generation MUST produce byte-identical output, with definitions emitted in a stable order. The SDL output MUST contain only names and type structures — it MUST NOT contain captured values, sample literals, or enum values inferred from observed data.

#### Scenario: Repeated generation is byte-identical
- **WHEN** generation runs twice on the same data directory
- **THEN** both output files are byte-identical

#### Scenario: Observed values never appear in the output
- **WHEN** captured traffic contains distinctive values such as `"secret-token-xyz"` or `42` in bodies and responses
- **THEN** the generated schema contains none of those values, only type and field names and type structure

### Requirement: Serialization failures do not write a partial file

If serialization of the GraphQL schema fails for any reason, the command MUST report an error and MUST NOT create or overwrite the output file.

#### Scenario: Failed serialization leaves no output file
- **WHEN** serialization of the schema fails during `swagdoc generate --format graphql --output schema.graphql`
- **THEN** the command exits with an error and no `schema.graphql` file exists or is modified
