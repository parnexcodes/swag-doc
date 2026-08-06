package graphql

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/parser"
)

type typeRegistry struct {
	defs map[string]*TypeDef
}

func newTypeRegistry() *typeRegistry {
	return &typeRegistry{defs: make(map[string]*TypeDef)}
}

// mergeObservedSchemas uses the existing JSON type inference for each
// observation and the existing schema merger for cross-observation merging.
func mergeObservedSchemas(values []any) (parser.Schema, bool) {
	merger := parser.NewSchemaMerger()
	count := 0
	for _, value := range values {
		if value == nil {
			continue
		}
		inferrer := parser.NewTypeInferrer(10)
		inferrer.AddSample("value", value)
		schema := inferrer.InferSchema("value")
		if schema == nil {
			continue
		}
		merger.AddSchema("value", "graphql", *schema)
		count++
	}
	if count == 0 {
		return parser.Schema{}, false
	}
	return merger.MergeSchemas("value", "graphql"), true
}

// schemaType converts an inferred JSON schema into an SDL reference and
// registers named object/input types. Values and examples in parser.Schema
// are intentionally ignored.
func (g *Generator) schemaType(fieldName, parent string, schema *parser.Schema, input bool, preferred string) (TypeRef, bool) {
	if schema == nil {
		return TypeRef{}, false
	}
	switch schema.Type {
	case "object":
		name := preferred
		if name == "" {
			name = NameFromIdentifier(fieldName)
		}
		name = g.uniqueTypeName(name, parent, schema, input)
		if existing, ok := g.types.defs[name]; ok {
			return TypeRef{Named: existing.Name}, true
		}
		definition := &TypeDef{
			Name:   name,
			Input:  input,
			Fields: make(map[string]FieldDef),
			Shape:  schemaShape(schema, input),
		}
		// Register before descending so nested references can safely refer to
		// this type if a future parser extension permits recursive values.
		g.types.defs[name] = definition
		propertyNames := make([]string, 0, len(schema.Properties))
		for propertyName := range schema.Properties {
			propertyNames = append(propertyNames, propertyName)
		}
		sort.Strings(propertyNames)
		for _, propertyName := range propertyNames {
			propertySchema := schema.Properties[propertyName]
			propertyType, ok := g.schemaType(propertyName, name, &propertySchema, input, "")
			if !ok {
				continue
			}
			definition.Fields[propertyName] = FieldDef{Name: propertyName, Type: propertyType}
		}
		return TypeRef{Named: name}, true
	case "array":
		if schema.Items == nil {
			return TypeRef{}, false
		}
		itemName := singularTypeName(fieldName)
		itemType, ok := g.schemaType(itemName, parent, schema.Items, input, preferred)
		if !ok {
			return TypeRef{}, false
		}
		return TypeRef{Elem: &itemType}, true
	case "string":
		if fieldName == "id" {
			return TypeRef{Named: "ID"}, true
		}
		return TypeRef{Named: "String"}, true
	case "integer":
		return TypeRef{Named: "Int"}, true
	case "number":
		return TypeRef{Named: "Float"}, true
	case "boolean":
		return TypeRef{Named: "Boolean"}, true
	default:
		return TypeRef{}, false
	}
}

func (g *Generator) uniqueTypeName(desired, parent string, schema *parser.Schema, input bool) string {
	shape := schemaShape(schema, input)
	if existing, ok := g.types.defs[desired]; !ok {
		return desired
	} else if existing.Shape == shape && existing.Input == input {
		return desired
	}

	base := NameFromIdentifier(parent) + desired
	if parent == "" || base == desired {
		base = desired + "Type"
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if existing, ok := g.types.defs[candidate]; !ok {
			return candidate
		} else if existing.Shape == shape && existing.Input == input {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", base, suffix)
	}
}

type shapeNode struct {
	Type       string               `json:"type,omitempty"`
	Properties map[string]shapeNode `json:"properties,omitempty"`
	Items      *shapeNode           `json:"items,omitempty"`
}

func schemaShape(schema *parser.Schema, input bool) string {
	if schema == nil {
		return fmt.Sprintf("%t:nil", input)
	}
	shape := shapeNode{Type: schema.Type}
	if len(schema.Properties) > 0 {
		shape.Properties = make(map[string]shapeNode, len(schema.Properties))
		for name, property := range schema.Properties {
			shape.Properties[name] = schemaShapeNode(&property)
		}
	}
	if schema.Items != nil {
		item := schemaShapeNode(schema.Items)
		shape.Items = &item
	}
	data, err := json.Marshal(shape)
	if err != nil {
		return fmt.Sprintf("%t:%s", input, schema.Type)
	}
	return fmt.Sprintf("%t:%s", input, data)
}

func schemaShapeNode(schema *parser.Schema) shapeNode {
	if schema == nil {
		return shapeNode{}
	}
	node := shapeNode{Type: schema.Type}
	if len(schema.Properties) > 0 {
		node.Properties = make(map[string]shapeNode, len(schema.Properties))
		for name, property := range schema.Properties {
			node.Properties[name] = schemaShapeNode(&property)
		}
	}
	if schema.Items != nil {
		item := schemaShapeNode(schema.Items)
		node.Items = &item
	}
	return node
}

func singularTypeName(name string) string {
	switch {
	case strings.HasSuffix(name, "ies") && len(name) > 3:
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "ses") && len(name) > 3:
		return strings.TrimSuffix(name, "es")
	case strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") && len(name) > 1:
		return strings.TrimSuffix(name, "s")
	default:
		return name
	}
}
