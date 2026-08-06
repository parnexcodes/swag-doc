package graphql

import "sort"

// TypeRef is a GraphQL type reference. Elem is set for list types; Named is
// set for named and scalar types. NonNull applies to this node, not its child.
type TypeRef struct {
	Named   string
	Elem    *TypeRef
	NonNull bool
}

func (t TypeRef) String() string {
	var value string
	if t.Elem != nil {
		value = "[" + t.Elem.String() + "]"
	} else {
		value = t.Named
	}
	if t.NonNull {
		value += "!"
	}
	return value
}

func (t TypeRef) IsZero() bool {
	return t.Named == "" && t.Elem == nil
}

// ArgDef is a root-field argument. Declared marks arguments whose type came
// from a variable definition and therefore takes precedence over literals.
type ArgDef struct {
	Name     string
	Type     TypeRef
	Declared bool
}

// FieldDef is a field in an SDL object or input type.
type FieldDef struct {
	Name string
	Type TypeRef
}

// TypeDef is a named object or input type.
type TypeDef struct {
	Name   string
	Input  bool
	Fields map[string]FieldDef
	Shape  string
}

func sortedFieldDefs(fields map[string]FieldDef) []FieldDef {
	result := make([]FieldDef, 0, len(fields))
	for _, field := range fields {
		result = append(result, field)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
