package graphql

import (
	"encoding/json"
	"sort"

	"github.com/parnexcodes/swag-doc/pkg/parser"
	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// Config configures GraphQL SDL generation.
type Config struct {
	Title       string
	Description string
	Version     string
}

type rootFieldState struct {
	Name    string
	Args    map[string]ArgDef
	Samples []any
	Type    TypeRef
	HasType bool
}

type rootState struct {
	Fields map[string]*rootFieldState
}

// Generator builds a GraphQL SDL schema from captured HTTP transactions.
type Generator struct {
	config Config
	roots  map[string]*rootState

	inputNames   map[string]bool
	inputSamples map[string][]any
	types        *typeRegistry
}

// NewGenerator creates a GraphQL SDL generator.
func NewGenerator(config Config) *Generator {
	return &Generator{
		config:       config,
		roots:        make(map[string]*rootState),
		inputNames:   make(map[string]bool),
		inputSamples: make(map[string][]any),
		types:        newTypeRegistry(),
	}
}

// AddTransaction adds a captured transaction. Non-GraphQL transactions are
// ignored by design.
func (g *Generator) AddTransaction(transaction proxy.APITransaction) {
	request, ok := ParseRequest(transaction)
	if !ok {
		return
	}
	g.addParsedTransaction(transaction, request)
}

func (g *Generator) addParsedTransaction(transaction proxy.APITransaction, request *ParsedRequest) {
	fragments := make(map[string]*Fragment, len(request.Document.Fragments))
	for _, fragment := range request.Document.Fragments {
		fragments[fragment.Name] = fragment
	}

	var response map[string]any
	if transaction.Response.StatusCode >= 200 && transaction.Response.StatusCode < 300 {
		var envelope map[string]any
		if err := json.Unmarshal(transaction.Response.Body, &envelope); err == nil {
			if data, ok := envelope["data"].(map[string]any); ok {
				response = data
			}
		}
	}

	for _, operation := range request.Document.Operations {
		root := g.roots[operation.Kind]
		if root == nil {
			root = &rootState{Fields: make(map[string]*rootFieldState)}
			g.roots[operation.Kind] = root
		}
		variableDefs := make(map[string]VariableDef, len(operation.Variables))
		for _, definition := range operation.Variables {
			if definition == nil {
				continue
			}
			variableDefs[definition.Name] = *definition
			g.recordInputVariable(*definition, request.Variables)
		}

		fields := expandSelections(operation.SelectionSet, fragments, make(map[string]bool), 0)
		for _, field := range fields {
			if field == nil || field.Name == "" {
				continue
			}
			state := root.Fields[field.Name]
			if state == nil {
				state = &rootFieldState{Name: field.Name, Args: make(map[string]ArgDef)}
				root.Fields[field.Name] = state
			}
			for _, argument := range field.Arguments {
				if definition, ok := variableDefinitionFor(argument.Value, variableDefs); ok {
					ref, err := ParseTypeReference(definition.Type)
					if err == nil {
						g.addArgument(state, ArgDef{Name: argument.Name, Type: ref, Declared: true})
					}
					continue
				}
				if ref, ok := g.literalType(argument.Name, argument.Value); ok {
					g.addArgument(state, ArgDef{Name: argument.Name, Type: ref})
				}
			}
			if response != nil {
				responseKey := field.Name
				if field.Alias != "" {
					responseKey = field.Alias
				}
				if value, ok := response[responseKey]; ok && value != nil {
					state.Samples = append(state.Samples, value)
				}
			}
		}
	}
}

func (g *Generator) recordInputVariable(def VariableDef, variables map[string]any) {
	ref, err := ParseTypeReference(def.Type)
	if err != nil || isBuiltinType(ref.Named) {
		return
	}
	g.inputNames[ref.Named] = true
	if variables == nil {
		return
	}
	value, ok := variables[def.Name]
	if !ok {
		return
	}
	for _, sample := range inputValues(ref, value) {
		if sample != nil {
			g.inputSamples[ref.Named] = append(g.inputSamples[ref.Named], sample)
		}
	}
}

func inputValues(ref TypeRef, value any) []any {
	if ref.Elem != nil {
		values, ok := value.([]any)
		if !ok {
			return nil
		}
		var result []any
		for _, item := range values {
			result = append(result, inputValues(*ref.Elem, item)...)
		}
		return result
	}
	return []any{value}
}

func variableDefinitionFor(value Value, definitions map[string]VariableDef) (VariableDef, bool) {
	if value.Kind != ValueVariable {
		return VariableDef{}, false
	}
	definition, ok := definitions[value.VarName]
	return definition, ok
}

func (g *Generator) addArgument(state *rootFieldState, argument ArgDef) {
	if state.Args == nil {
		state.Args = make(map[string]ArgDef)
	}
	existing, ok := state.Args[argument.Name]
	if !ok || argument.Declared && !existing.Declared {
		state.Args[argument.Name] = argument
	}
}

func (g *Generator) literalType(name string, value Value) (TypeRef, bool) {
	goValue := value.ToGoValue()
	if goValue == nil {
		return TypeRef{}, false
	}
	inferrer := parser.NewTypeInferrer(10)
	inferrer.AddSample("value", goValue)
	schema := inferrer.InferSchema("value")
	if schema == nil {
		return TypeRef{}, false
	}
	preferred := ""
	if schema.Type == "object" {
		preferred = NameFromIdentifier(name) + "Input"
	}
	return g.schemaType(name, "", schema, true, preferred)
}

// Generate produces deterministic GraphQL SDL.
func (g *Generator) Generate() (string, error) {
	g.prepareDeclaredInputs()

	rootNames := []string{"query", "mutation", "subscription"}
	for _, rootName := range rootNames {
		root := g.roots[rootName]
		if root == nil {
			continue
		}
		fieldNames := make([]string, 0, len(root.Fields))
		for name := range root.Fields {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)
		for _, fieldName := range fieldNames {
			state := root.Fields[fieldName]
			schema, ok := mergeObservedSchemas(state.Samples)
			if !ok {
				continue
			}
			ref, ok := g.schemaType(fieldName, "", &schema, false, "")
			if !ok {
				continue
			}
			state.Type = ref
			state.HasType = true
		}
	}

	inputNames := make([]string, 0, len(g.inputNames))
	for name := range g.inputNames {
		inputNames = append(inputNames, name)
	}
	sort.Strings(inputNames)
	for _, name := range inputNames {
		definition := g.types.defs[name]
		if definition == nil {
			definition = &TypeDef{Name: name, Input: true, Fields: make(map[string]FieldDef)}
			g.types.defs[name] = definition
		}
		if definition.Input {
			if schema, ok := mergeObservedSchemas(g.inputSamples[name]); ok && schema.Type == "object" {
				g.fillTypeDefinition(definition, &schema, true)
			}
		}
	}
	return serializeGenerator(g), nil
}

func (g *Generator) prepareDeclaredInputs() {
	for name := range g.inputNames {
		if _, ok := g.types.defs[name]; !ok {
			g.types.defs[name] = &TypeDef{
				Name:   name,
				Input:  true,
				Fields: make(map[string]FieldDef),
				Shape:  "declared-input:" + name,
			}
		}
	}
}

func (g *Generator) fillTypeDefinition(definition *TypeDef, schema *parser.Schema, input bool) {
	if definition == nil || schema == nil || schema.Type != "object" {
		return
	}
	definition.Input = input
	definition.Shape = schemaShape(schema, input)
	definition.Fields = make(map[string]FieldDef)
	propertyNames := make([]string, 0, len(schema.Properties))
	for propertyName := range schema.Properties {
		propertyNames = append(propertyNames, propertyName)
	}
	sort.Strings(propertyNames)
	for _, propertyName := range propertyNames {
		propertySchema := schema.Properties[propertyName]
		fieldType, ok := g.schemaType(propertyName, definition.Name, &propertySchema, input, "")
		if ok {
			definition.Fields[propertyName] = FieldDef{Name: propertyName, Type: fieldType}
		}
	}
}

func expandSelections(fields []*Field, fragments map[string]*Fragment, visited map[string]bool, depth int) []*Field {
	if depth > maxParseDepth {
		return nil
	}
	var result []*Field
	for _, field := range fields {
		if field == nil {
			continue
		}
		for _, name := range field.FragmentSpreads {
			if visited[name] {
				continue
			}
			fragment := fragments[name]
			if fragment == nil {
				continue
			}
			visited[name] = true
			result = append(result, expandSelections(fragment.SelectionSet, fragments, visited, depth+1)...)
			delete(visited, name)
		}
		for _, inline := range field.InlineFragments {
			if inline != nil {
				result = append(result, expandSelections(inline.SelectionSet, fragments, visited, depth+1)...)
			}
		}
		if field.Name != "" {
			result = append(result, field)
		}
	}
	return result
}

func isBuiltinType(name string) bool {
	switch name {
	case "String", "Int", "Float", "Boolean", "ID":
		return true
	default:
		return false
	}
}
