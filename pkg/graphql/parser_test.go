package graphql

import (
	"strings"
	"testing"
)

func TestParseDocument(t *testing.T) {
	source := `
		# operation with aliases, variables, values, directives, and fragments
		query GetUser($id: ID!, $label: String = "default") @cache(if: true) {
			u: user(id: $id, filter: {active: true, tags: ["a", ACTIVE]}) @include(if: true) {
				...userFields
				... on User { email }
			}
		}
		fragment userFields on User { id name }
		mutation CreateUser($input: UserInput!) {
			createUser(input: $input) { id }
		}
	`

	document, err := ParseDocument(source)
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	if len(document.Operations) != 2 {
		t.Fatalf("operations = %d, want 2", len(document.Operations))
	}
	query := document.Operations[0]
	if query.Kind != "query" || query.Name != "GetUser" {
		t.Fatalf("query metadata = %#v", query)
	}
	if len(query.Variables) != 2 || query.Variables[0].Type != "ID!" || !query.Variables[1].HasDefault {
		t.Fatalf("variables = %#v", query.Variables)
	}
	if len(query.SelectionSet) != 1 || query.SelectionSet[0].Alias != "u" || query.SelectionSet[0].Name != "user" {
		t.Fatalf("selection = %#v", query.SelectionSet)
	}
	user := query.SelectionSet[0]
	if len(user.Arguments) != 2 || user.Arguments[0].Value.Kind != ValueVariable {
		t.Fatalf("user arguments = %#v", user.Arguments)
	}
	if len(user.SelectionSet) != 2 || len(user.SelectionSet[0].FragmentSpreads) != 1 || len(user.SelectionSet[1].InlineFragments) != 1 {
		t.Fatalf("nested selections = %#v", user.SelectionSet)
	}
	if len(document.Fragments) != 1 || document.Fragments[0].TypeCondition != "User" {
		t.Fatalf("fragments = %#v", document.Fragments)
	}
	if document.Operations[1].Kind != "mutation" {
		t.Fatalf("second operation = %#v", document.Operations[1])
	}
}

func TestParseAnonymousAndValues(t *testing.T) {
	document, err := ParseDocument(`{
		search(text: "hello", count: -2, ratio: 1.5e2, enabled: false,
			missing: null, status: ACTIVE, tags: ["one", "two"],
			filter: {name: "alice"}) { id }
	}`)
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	if len(document.Operations) != 1 || document.Operations[0].Kind != "query" || document.Operations[0].Name != "" {
		t.Fatalf("anonymous operation = %#v", document.Operations)
	}
	args := document.Operations[0].SelectionSet[0].Arguments
	wantKinds := []ValueKind{ValueString, ValueInt, ValueFloat, ValueBoolean, ValueNull, ValueEnum, ValueList, ValueObject}
	if len(args) != len(wantKinds) {
		t.Fatalf("argument count = %d, want %d", len(args), len(wantKinds))
	}
	for i, want := range wantKinds {
		if args[i].Value.Kind != want {
			t.Errorf("argument %d kind = %q, want %q", i, args[i].Value.Kind, want)
		}
	}
}

func TestParseBlockStringAndEscapes(t *testing.T) {
	document, err := ParseDocument(`query { search(text: """hello\""" world
second line""") { id } }`)
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	value := document.Operations[0].SelectionSet[0].Arguments[0].Value
	if value.Kind != ValueString || !strings.Contains(value.Text, `"""`) || !strings.Contains(value.Text, "second line") {
		t.Fatalf("block string value = %#v", value)
	}

	document, err = ParseDocument(`query { search(text: "line\nvalue\u0021") { id } }`)
	if err != nil {
		t.Fatalf("escaped ParseDocument() error = %v", err)
	}
	if got := document.Operations[0].SelectionSet[0].Arguments[0].Value.Text; got != "line\nvalue!" {
		t.Fatalf("escaped string = %q", got)
	}
}

func TestParseTypeReference(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "ID!", want: "ID!"},
		{input: "[String!]!", want: "[String!]!"},
		{input: "[[User]]", want: "[[User]]"},
	} {
		ref, err := ParseTypeReference(test.input)
		if err != nil {
			t.Errorf("ParseTypeReference(%q) error = %v", test.input, err)
			continue
		}
		if got := ref.String(); got != test.want {
			t.Errorf("ParseTypeReference(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseDocumentErrors(t *testing.T) {
	cases := []string{
		``,
		`query MissingBrace { user { id }`,
		`query { user(text: "unterminated) { id } }`,
		`query { user( }`,
		`query MissingSelection`,
		`query { user { } }`,
		`query($id: [ID!) { user(id: $id) { id } }`,
		`query { user @skip( { id } }`,
	}
	for _, source := range cases {
		if _, err := ParseDocument(source); err == nil {
			t.Errorf("ParseDocument(%q) error = nil, want error", source)
		}
	}
}

func TestParseDocumentDepthLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for range maxParseDepth + 1 {
		b.WriteString("node{")
	}
	b.WriteString("id")
	for range maxParseDepth + 1 {
		b.WriteString("}")
	}
	b.WriteString("}")
	if _, err := ParseDocument(b.String()); err == nil {
		t.Fatal("ParseDocument() error = nil for deeply nested document")
	}
}

func TestNameFromIdentifier(t *testing.T) {
	for input, want := range map[string]string{
		"user":       "User",
		"user_posts": "UserPosts",
		"":           "Anonymous",
	} {
		if got := NameFromIdentifier(input); got != want {
			t.Errorf("NameFromIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}
