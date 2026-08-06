package graphql

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

func graphqlTransaction(query string, variables map[string]any, response string, status int) proxy.APITransaction {
	payload := map[string]any{"query": query}
	if variables != nil {
		payload["variables"] = variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return proxy.APITransaction{
		Request: proxy.RequestData{
			Method:  http.MethodPost,
			Path:    "/graphql",
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Body:    body,
		},
		Response: proxy.ResponseData{
			StatusCode: status,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(response),
		},
	}
}

func TestGeneratorProducesNestedQuerySchema(t *testing.T) {
	generator := NewGenerator(Config{Title: "Test API", Description: "Observed schema", Version: "1.0.0"})
	generator.AddTransaction(graphqlTransaction(
		`query GetUser($id: ID!) { user(id: $id) { id name posts { title } } }`,
		map[string]any{"id": "1"},
		`{"data":{"user":{"id":"u1","name":"alice","posts":[{"title":"hello"}]}}}`,
		200,
	))

	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, fragment := range []string{
		"# Test API",
		"# Observed schema",
		"# Version: 1.0.0",
		"type Query {\n  user(id: ID!): User\n}",
		"type User {\n  id: ID\n  name: String\n  posts: [Post]\n}",
		"type Post {\n  title: String\n}",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("schema missing %q:\n%s", fragment, schema)
		}
	}
	if strings.Contains(schema, "alice") || strings.Contains(schema, "u1") || strings.Contains(schema, "hello") {
		t.Errorf("schema contains captured values:\n%s", schema)
	}
}

func TestGeneratorProducesMutationInput(t *testing.T) {
	generator := NewGenerator(Config{})
	generator.AddTransaction(graphqlTransaction(
		`mutation CreateUser($input: UserInput!) { createUser(input: $input) { id username } }`,
		map[string]any{"input": map[string]any{"username": "alice"}},
		`{"data":{"createUser":{"id":"u1","username":"alice"}}}`,
		200,
	))
	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, fragment := range []string{
		"type Mutation {\n  createUser(input: UserInput!): CreateUser\n}",
		"input UserInput {\n  username: String\n}",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("schema missing %q:\n%s", fragment, schema)
		}
	}
}

func TestGeneratorMergesArgumentsAndResponseFields(t *testing.T) {
	generator := NewGenerator(Config{})
	generator.AddTransaction(graphqlTransaction(
		`query { user(id: 1) { id name } }`,
		nil,
		`{"data":{"user":{"id":"one","name":"alice"}}}`,
		200,
	))
	generator.AddTransaction(graphqlTransaction(
		`query { user(active: true) { id email } }`,
		nil,
		`{"data":{"user":{"id":"two","email":"alice@example.com"}}}`,
		200,
	))
	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(schema, "user(active: Boolean, id: Int): User") {
		t.Fatalf("merged arguments missing:\n%s", schema)
	}
	for _, field := range []string{"id: ID", "name: String", "email: String"} {
		if !strings.Contains(schema, field) {
			t.Errorf("merged response field %q missing:\n%s", field, schema)
		}
	}
}

func TestGeneratorResolvesAliasesAndFragments(t *testing.T) {
	generator := NewGenerator(Config{})
	generator.AddTransaction(graphqlTransaction(
		`query { u: user { ...userFields } } fragment userFields on User { id name }`,
		nil,
		`{"data":{"u":{"id":"u1","name":"alice"}}}`,
		200,
	))
	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(schema, "user: User") || strings.Contains(schema, "u: User") {
		t.Fatalf("alias mapping is wrong:\n%s", schema)
	}
	if !strings.Contains(schema, "id: ID") || !strings.Contains(schema, "name: String") {
		t.Fatalf("fragment fields missing:\n%s", schema)
	}
}

func TestGeneratorSkipsErrorOnlyAndInvalidResponses(t *testing.T) {
	generator := NewGenerator(Config{})
	generator.AddTransaction(graphqlTransaction(
		`query { user { id } }`,
		nil,
		`{"errors":[{"message":"unauthorized"}]}`,
		200,
	))
	generator.AddTransaction(graphqlTransaction(
		`query { post { id } }`,
		nil,
		`not-json`,
		200,
	))
	generator.AddTransaction(graphqlTransaction(
		`query { hidden { token } }`,
		nil,
		`{"data":{"hidden":{"token":"secret"}}}`,
		500,
	))
	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if strings.Contains(schema, "type User") || strings.Contains(schema, "type Post") || strings.Contains(schema, "type Hidden") {
		t.Fatalf("invalid responses contributed types:\n%s", schema)
	}
	if strings.Contains(schema, "secret") || strings.Contains(schema, "unauthorized") {
		t.Fatalf("response values leaked:\n%s", schema)
	}
}

func TestGeneratorDeterministic(t *testing.T) {
	transactions := []proxy.APITransaction{
		graphqlTransaction(`query { zed { id } }`, nil, `{"data":{"zed":{"id":"z"}}}`, 200),
		graphqlTransaction(`query { alpha { name } }`, nil, `{"data":{"alpha":{"name":"a"}}}`, 200),
	}
	first := NewGenerator(Config{Title: "API", Version: "1"})
	second := NewGenerator(Config{Title: "API", Version: "1"})
	for _, transaction := range transactions {
		first.AddTransaction(transaction)
		second.AddTransaction(transaction)
	}
	one, err := first.Generate()
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	two, err := second.Generate()
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if one != two {
		t.Fatalf("generation is not deterministic:\n--- first\n%s\n--- second\n%s", one, two)
	}
}

func TestGeneratorSupportsSubscriptionAndLiteralList(t *testing.T) {
	generator := NewGenerator(Config{})
	generator.AddTransaction(graphqlTransaction(
		`subscription Events { events(ids: [1, 2]) { id message } }`,
		nil,
		`{"data":{"events":{"id":"e1","message":"hello"}}}`,
		200,
	))
	schema, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(schema, "type Subscription {\n  events(ids: [Int]): Events\n}") {
		t.Fatalf("subscription schema missing:\n%s", schema)
	}
}
