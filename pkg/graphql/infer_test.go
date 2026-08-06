package graphql

import (
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/parser"
)

func TestMergeObservedSchemas(t *testing.T) {
	merged, ok := mergeObservedSchemas([]any{
		map[string]any{"id": float64(1), "name": "alice"},
		map[string]any{"id": float64(2), "email": "alice@example.com"},
	})
	if !ok {
		t.Fatal("mergeObservedSchemas() returned no schema")
	}
	if merged.Type != "object" {
		t.Fatalf("merged type = %q, want object", merged.Type)
	}
	for _, name := range []string{"id", "name", "email"} {
		if _, ok := merged.Properties[name]; !ok {
			t.Errorf("merged properties missing %q: %#v", name, merged.Properties)
		}
	}
}

func TestMergeObservedSchemasSkipsNull(t *testing.T) {
	if _, ok := mergeObservedSchemas([]any{nil}); ok {
		t.Fatal("mergeObservedSchemas() accepted only-null observations")
	}
}

func TestSchemaTypeBuildsNestedSDLTypes(t *testing.T) {
	generator := NewGenerator(Config{})
	schema := parser.Schema{
		Type: "object",
		Properties: map[string]parser.Schema{
			"id": {Type: "string"},
			"posts": {
				Type: "array",
				Items: &parser.Schema{Type: "object", Properties: map[string]parser.Schema{
					"title": {Type: "string"},
				}},
			},
		},
	}
	ref, ok := generator.schemaType("user", "", &schema, false, "")
	if !ok || ref.String() != "User" {
		t.Fatalf("schemaType() = %q, ok=%v", ref.String(), ok)
	}
	user := generator.types.defs["User"]
	if user == nil || user.Fields["id"].Type.String() != "ID" {
		t.Fatalf("User definition = %#v", user)
	}
	if user.Fields["posts"].Type.String() != "[Post]" {
		t.Fatalf("posts type = %q", user.Fields["posts"].Type.String())
	}
	if post := generator.types.defs["Post"]; post == nil || post.Fields["title"].Type.String() != "String" {
		t.Fatalf("Post definition = %#v", post)
	}
}

func TestSchemaTypeCollisionUsesParentName(t *testing.T) {
	generator := NewGenerator(Config{})
	first := parser.Schema{Type: "object", Properties: map[string]parser.Schema{"value": {Type: "string"}}}
	second := parser.Schema{Type: "object", Properties: map[string]parser.Schema{"value": {Type: "boolean"}}}
	if _, ok := generator.schemaType("address", "User", &first, false, ""); !ok {
		t.Fatal("first schemaType() failed")
	}
	ref, ok := generator.schemaType("address", "Post", &second, false, "")
	if !ok || ref.String() != "PostAddress" {
		t.Fatalf("second schemaType() = %q, ok=%v", ref.String(), ok)
	}
}
