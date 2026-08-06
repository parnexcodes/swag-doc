package graphql

import (
	"sort"
	"strings"
)

func serializeGenerator(g *Generator) string {
	var blocks []string
	if header := serializeHeader(g.config); header != "" {
		blocks = append(blocks, header)
	}

	for _, rootName := range []string{"query", "mutation", "subscription"} {
		root := g.roots[rootName]
		if root == nil {
			continue
		}
		name := rootSDLName(rootName)
		blocks = append(blocks, serializeRoot(name, root))
	}

	var objectNames, inputNames []string
	for name, definition := range g.types.defs {
		if definition.Input {
			inputNames = append(inputNames, name)
		} else {
			objectNames = append(objectNames, name)
		}
	}
	sort.Strings(objectNames)
	sort.Strings(inputNames)
	for _, name := range objectNames {
		blocks = append(blocks, serializeType(g.types.defs[name]))
	}
	for _, name := range inputNames {
		blocks = append(blocks, serializeType(g.types.defs[name]))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.TrimRight(strings.Join(blocks, "\n\n"), "\n") + "\n"
}

func serializeHeader(config Config) string {
	var lines []string
	if config.Title != "" {
		lines = append(lines, commentLines(config.Title)...)
	}
	if config.Description != "" {
		lines = append(lines, commentLines(config.Description)...)
	}
	if config.Version != "" {
		lines = append(lines, "# Version: "+config.Version)
	}
	return strings.Join(lines, "\n")
}

func commentLines(value string) []string {
	parts := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, "# "+part)
	}
	return lines
}

func rootSDLName(name string) string {
	switch name {
	case "mutation":
		return "Mutation"
	case "subscription":
		return "Subscription"
	default:
		return "Query"
	}
}

func serializeRoot(name string, root *rootState) string {
	var fields []string
	for fieldName, state := range root.Fields {
		if !state.HasType {
			continue
		}
		fields = append(fields, serializeField(fieldName, state.Args, state.Type))
	}
	sort.Strings(fields)
	return serializeDefinition("type", name, fields)
}

func serializeType(definition *TypeDef) string {
	kind := "type"
	if definition.Input {
		kind = "input"
	}
	fieldDefs := sortedFieldDefs(definition.Fields)
	fields := make([]string, 0, len(fieldDefs))
	for _, field := range fieldDefs {
		fields = append(fields, "  "+field.Name+": "+field.Type.String())
	}
	return serializeDefinition(kind, definition.Name, fields)
}

func serializeDefinition(kind, name string, fields []string) string {
	var b strings.Builder
	b.WriteString(kind)
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString(" {\n")
	for _, field := range fields {
		b.WriteString(field)
		b.WriteByte('\n')
	}
	b.WriteString("}")
	return b.String()
}

func serializeField(name string, args map[string]ArgDef, result TypeRef) string {
	var argNames []string
	for argName := range args {
		argNames = append(argNames, argName)
	}
	sort.Strings(argNames)
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(name)
	if len(argNames) > 0 {
		b.WriteByte('(')
		for i, argName := range argNames {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(argName)
			b.WriteString(": ")
			b.WriteString(args[argName].Type.String())
		}
		b.WriteByte(')')
	}
	b.WriteString(": ")
	b.WriteString(result.String())
	return b.String()
}
