package graphql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const maxParseDepth = 100

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenName
	tokenInt
	tokenFloat
	tokenString
	tokenPunct
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

// ValueKind identifies the kind of a GraphQL argument or default value.
type ValueKind string

const (
	ValueInt      ValueKind = "int"
	ValueFloat    ValueKind = "float"
	ValueString   ValueKind = "string"
	ValueBoolean  ValueKind = "boolean"
	ValueNull     ValueKind = "null"
	ValueEnum     ValueKind = "enum"
	ValueList     ValueKind = "list"
	ValueObject   ValueKind = "object"
	ValueVariable ValueKind = "variable"
)

// Value is a GraphQL literal or variable reference.
type Value struct {
	Kind    ValueKind
	Text    string
	Bool    bool
	List    []Value
	Object  []ObjectField
	VarName string
}

// ObjectField is a field in an object literal value.
type ObjectField struct {
	Name  string
	Value Value
}

// Argument is a named field argument.
type Argument struct {
	Name  string
	Value Value
}

// Field is a selected GraphQL field.
type Field struct {
	Alias           string
	Name            string
	Arguments       []Argument
	SelectionSet    []*Field
	FragmentSpreads []string
	InlineFragments []*InlineFragment
}

// InlineFragment is an inline fragment selection.
type InlineFragment struct {
	TypeCondition string
	SelectionSet  []*Field
}

// VariableDef declares a typed operation variable.
type VariableDef struct {
	Name       string
	Type       string
	HasDefault bool
}

// Operation is a query, mutation, or subscription operation.
type Operation struct {
	Kind         string
	Name         string
	Variables    []*VariableDef
	SelectionSet []*Field
}

// Fragment is a named fragment definition.
type Fragment struct {
	Name          string
	TypeCondition string
	SelectionSet  []*Field
}

// Document is a parsed GraphQL document.
type Document struct {
	Operations []*Operation
	Fragments  []*Fragment
}

// ParseDocument parses a GraphQL document without executing it.
func ParseDocument(source string) (*Document, error) {
	p := &documentParser{lexer: lexer{source: source}}
	return p.parseDocument()
}

type lexer struct {
	source string
	pos    int
}

func (l *lexer) next() (token, error) {
	if err := l.skipIgnored(); err != nil {
		return token{}, err
	}
	start := l.pos
	if l.pos >= len(l.source) {
		return token{kind: tokenEOF, pos: l.pos}, nil
	}

	if strings.HasPrefix(l.source[l.pos:], "...") {
		l.pos += 3
		return token{kind: tokenPunct, text: "...", pos: start}, nil
	}

	ch := l.source[l.pos]
	if isNameStart(ch) {
		l.pos++
		for l.pos < len(l.source) && isNameContinue(l.source[l.pos]) {
			l.pos++
		}
		return token{kind: tokenName, text: l.source[start:l.pos], pos: start}, nil
	}

	if ch == '"' {
		text, err := l.readString()
		if err != nil {
			return token{}, err
		}
		return token{kind: tokenString, text: text, pos: start}, nil
	}

	if ch == '-' || isDigit(ch) {
		text, kind, err := l.readNumber()
		if err != nil {
			return token{}, err
		}
		return token{kind: kind, text: text, pos: start}, nil
	}

	if strings.ContainsRune("!$():=@[]{|}&", rune(ch)) {
		l.pos++
		return token{kind: tokenPunct, text: string(ch), pos: start}, nil
	}

	return token{}, fmt.Errorf("graphql: unexpected character %q at offset %d", ch, l.pos)
}

func (l *lexer) skipIgnored() error {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		switch ch {
		case ' ', '\t', '\n', '\r', ',':
			l.pos++
		case '#':
			for l.pos < len(l.source) && l.source[l.pos] != '\n' && l.source[l.pos] != '\r' {
				l.pos++
			}
		default:
			if l.pos == 0 && strings.HasPrefix(l.source, "\xef\xbb\xbf") {
				l.pos += 3
				continue
			}
			return nil
		}
	}
	return nil
}

func (l *lexer) readString() (string, error) {
	if strings.HasPrefix(l.source[l.pos:], "\"\"\"") {
		return l.readBlockString()
	}

	l.pos++
	var b strings.Builder
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		l.pos++
		switch ch {
		case '"':
			return b.String(), nil
		case '\\':
			if l.pos >= len(l.source) {
				return "", fmt.Errorf("graphql: unterminated escape at offset %d", l.pos)
			}
			decoded, err := l.readEscape()
			if err != nil {
				return "", err
			}
			b.WriteRune(decoded)
		case '\n', '\r':
			return "", fmt.Errorf("graphql: unescaped newline in string at offset %d", l.pos-1)
		default:
			if ch < 0x20 {
				return "", fmt.Errorf("graphql: invalid control character in string at offset %d", l.pos-1)
			}
			b.WriteByte(ch)
		}
	}
	return "", fmt.Errorf("graphql: unterminated string at offset %d", l.pos)
}

func (l *lexer) readBlockString() (string, error) {
	l.pos += 3
	var b strings.Builder
	for l.pos < len(l.source) {
		if strings.HasPrefix(l.source[l.pos:], "\"\"\"") {
			l.pos += 3
			return normalizeBlockString(b.String()), nil
		}
		if strings.HasPrefix(l.source[l.pos:], "\\\"\"\"") {
			b.WriteString("\"\"\"")
			l.pos += 4
			continue
		}
		b.WriteByte(l.source[l.pos])
		l.pos++
	}
	return "", fmt.Errorf("graphql: unterminated block string at offset %d", l.pos)
}

func normalizeBlockString(value string) string {
	value = strings.TrimPrefix(value, "\r\n")
	value = strings.TrimPrefix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	value = strings.TrimSuffix(value, "\n")
	return value
}

func (l *lexer) readEscape() (rune, error) {
	ch := l.source[l.pos]
	l.pos++
	switch ch {
	case '"', '\\', '/':
		return rune(ch), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		if l.pos+4 > len(l.source) {
			return 0, fmt.Errorf("graphql: incomplete unicode escape at offset %d", l.pos)
		}
		digits := l.source[l.pos : l.pos+4]
		l.pos += 4
		value, err := strconv.ParseUint(digits, 16, 16)
		if err != nil {
			return 0, fmt.Errorf("graphql: invalid unicode escape %q", digits)
		}
		return rune(value), nil
	default:
		return 0, fmt.Errorf("graphql: invalid escape sequence \\%c at offset %d", ch, l.pos-1)
	}
}

func (l *lexer) readNumber() (string, tokenKind, error) {
	start := l.pos
	if l.source[l.pos] == '-' {
		l.pos++
		if l.pos >= len(l.source) || !isDigit(l.source[l.pos]) {
			return "", tokenEOF, fmt.Errorf("graphql: invalid number at offset %d", start)
		}
	}
	if l.source[l.pos] == '0' {
		l.pos++
		if l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			return "", tokenEOF, fmt.Errorf("graphql: leading zero in number at offset %d", start)
		}
	} else {
		for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			l.pos++
		}
	}

	kind := tokenInt
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		kind = tokenFloat
		l.pos++
		if l.pos >= len(l.source) || !isDigit(l.source[l.pos]) {
			return "", tokenEOF, fmt.Errorf("graphql: expected digits after decimal point at offset %d", start)
		}
		for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			l.pos++
		}
	}
	if l.pos < len(l.source) && (l.source[l.pos] == 'e' || l.source[l.pos] == 'E') {
		kind = tokenFloat
		l.pos++
		if l.pos < len(l.source) && (l.source[l.pos] == '+' || l.source[l.pos] == '-') {
			l.pos++
		}
		if l.pos >= len(l.source) || !isDigit(l.source[l.pos]) {
			return "", tokenEOF, fmt.Errorf("graphql: expected exponent digits at offset %d", start)
		}
		for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			l.pos++
		}
	}
	return l.source[start:l.pos], kind, nil
}

func isNameStart(ch byte) bool {
	return ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

func isNameContinue(ch byte) bool {
	return isNameStart(ch) || isDigit(ch)
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

type documentParser struct {
	lexer  lexer
	buffer *token
	depth  int
}

func (p *documentParser) parseDocument() (*Document, error) {
	doc := &Document{}
	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if tok.kind == tokenEOF {
			break
		}
		if tok.kind == tokenName && tok.text == "fragment" {
			fragment, err := p.parseFragment()
			if err != nil {
				return nil, err
			}
			doc.Fragments = append(doc.Fragments, fragment)
			continue
		}
		if tok.kind == tokenPunct && tok.text == "{" || tok.kind == tokenName && isOperationType(tok.text) {
			operation, err := p.parseOperation()
			if err != nil {
				return nil, err
			}
			doc.Operations = append(doc.Operations, operation)
			continue
		}
		return nil, p.errorf(tok, "expected operation, fragment, or selection set")
	}
	if len(doc.Operations) == 0 {
		return nil, fmt.Errorf("graphql: document contains no operation")
	}
	return doc, nil
}

func (p *documentParser) parseOperation() (*Operation, error) {
	op := &Operation{Kind: "query"}
	tok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if tok.kind == tokenPunct && tok.text == "{" {
		selection, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		op.SelectionSet = selection
		return op, nil
	}

	kind, err := p.expectName()
	if err != nil {
		return nil, err
	}
	op.Kind = kind
	if tok, err := p.peek(); err != nil {
		return nil, err
	} else if tok.kind == tokenName {
		op.Name, err = p.expectName()
		if err != nil {
			return nil, err
		}
	}
	if tok, err := p.peek(); err != nil {
		return nil, err
	} else if isPunct(tok, "(") {
		op.Variables, err = p.parseVariableDefinitions()
		if err != nil {
			return nil, err
		}
	}
	if err := p.skipDirectives(); err != nil {
		return nil, err
	}
	op.SelectionSet, err = p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	return op, nil
}

func (p *documentParser) parseVariableDefinitions() ([]*VariableDef, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	var defs []*VariableDef
	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if isPunct(tok, ")") {
			_, _ = p.consume()
			return defs, nil
		}
		if err := p.expectPunct("$"); err != nil {
			return nil, err
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		typeName, err := p.parseTypeReference()
		if err != nil {
			return nil, err
		}
		def := &VariableDef{Name: name, Type: typeName}
		if tok, err := p.peek(); err != nil {
			return nil, err
		} else if isPunct(tok, "=") {
			_, _ = p.consume()
			if _, err := p.parseValue(); err != nil {
				return nil, err
			}
			def.HasDefault = true
		}
		defs = append(defs, def)
	}
}

func (p *documentParser) parseTypeReference() (string, error) {
	if err := p.enter(); err != nil {
		return "", err
	}
	defer p.leave()

	var b strings.Builder
	if tok, err := p.peek(); err != nil {
		return "", err
	} else if isPunct(tok, "[") {
		_, _ = p.consume()
		b.WriteByte('[')
		inner, err := p.parseTypeReference()
		if err != nil {
			return "", err
		}
		b.WriteString(inner)
		if err := p.expectPunct("]"); err != nil {
			return "", err
		}
		b.WriteByte(']')
	} else {
		name, err := p.expectName()
		if err != nil {
			return "", err
		}
		b.WriteString(name)
	}
	if tok, err := p.peek(); err != nil {
		return "", err
	} else if isPunct(tok, "!") {
		_, _ = p.consume()
		b.WriteByte('!')
	}
	return b.String(), nil
}

func (p *documentParser) parseSelectionSet() ([]*Field, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	var fields []*Field
	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if isPunct(tok, "}") {
			_, _ = p.consume()
			if len(fields) == 0 {
				return nil, p.errorf(tok, "selection set must not be empty")
			}
			return fields, nil
		}
		if tok.kind == tokenEOF {
			return nil, p.errorf(tok, "unterminated selection set")
		}
		if isPunct(tok, "...") {
			field, err := p.parseFragmentSelection()
			if err != nil {
				return nil, err
			}
			fields = append(fields, field)
			continue
		}
		field, err := p.parseField()
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
}

func (p *documentParser) parseField() (*Field, error) {
	first, err := p.expectName()
	if err != nil {
		return nil, err
	}
	field := &Field{Name: first}
	if tok, err := p.peek(); err != nil {
		return nil, err
	} else if isPunct(tok, ":") {
		_, _ = p.consume()
		field.Alias = first
		field.Name, err = p.expectName()
		if err != nil {
			return nil, err
		}
	}
	if tok, err := p.peek(); err != nil {
		return nil, err
	} else if isPunct(tok, "(") {
		field.Arguments, err = p.parseArguments()
		if err != nil {
			return nil, err
		}
	}
	if err := p.skipDirectives(); err != nil {
		return nil, err
	}
	if tok, err := p.peek(); err != nil {
		return nil, err
	} else if isPunct(tok, "{") {
		field.SelectionSet, err = p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
	}
	return field, nil
}

func (p *documentParser) parseArguments() ([]Argument, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	var args []Argument
	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if isPunct(tok, ")") {
			_, _ = p.consume()
			return args, nil
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, Argument{Name: name, Value: value})
	}
}

func (p *documentParser) parseValue() (Value, error) {
	if err := p.enter(); err != nil {
		return Value{}, err
	}
	defer p.leave()

	tok, err := p.peek()
	if err != nil {
		return Value{}, err
	}
	switch {
	case isPunct(tok, "$"):
		_, _ = p.consume()
		name, err := p.expectName()
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueVariable, VarName: name}, nil
	case isPunct(tok, "["):
		_, _ = p.consume()
		var values []Value
		for {
			next, err := p.peek()
			if err != nil {
				return Value{}, err
			}
			if isPunct(next, "]") {
				_, _ = p.consume()
				return Value{Kind: ValueList, List: values}, nil
			}
			value, err := p.parseValue()
			if err != nil {
				return Value{}, err
			}
			values = append(values, value)
		}
	case isPunct(tok, "{"):
		_, _ = p.consume()
		var fields []ObjectField
		for {
			next, err := p.peek()
			if err != nil {
				return Value{}, err
			}
			if isPunct(next, "}") {
				_, _ = p.consume()
				return Value{Kind: ValueObject, Object: fields}, nil
			}
			name, err := p.expectName()
			if err != nil {
				return Value{}, err
			}
			if err := p.expectPunct(":"); err != nil {
				return Value{}, err
			}
			value, err := p.parseValue()
			if err != nil {
				return Value{}, err
			}
			fields = append(fields, ObjectField{Name: name, Value: value})
		}
	case tok.kind == tokenString:
		_, _ = p.consume()
		return Value{Kind: ValueString, Text: tok.text}, nil
	case tok.kind == tokenInt:
		_, _ = p.consume()
		return Value{Kind: ValueInt, Text: tok.text}, nil
	case tok.kind == tokenFloat:
		_, _ = p.consume()
		return Value{Kind: ValueFloat, Text: tok.text}, nil
	case tok.kind == tokenName:
		_, _ = p.consume()
		switch tok.text {
		case "true":
			return Value{Kind: ValueBoolean, Bool: true}, nil
		case "false":
			return Value{Kind: ValueBoolean, Bool: false}, nil
		case "null":
			return Value{Kind: ValueNull}, nil
		default:
			return Value{Kind: ValueEnum, Text: tok.text}, nil
		}
	default:
		return Value{}, p.errorf(tok, "expected value")
	}
}

func (p *documentParser) parseFragment() (*Fragment, error) {
	if _, err := p.expectName(); err != nil { // fragment
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if keyword, err := p.expectName(); err != nil || keyword != "on" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("graphql: expected on after fragment name")
	}
	typeCondition, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.skipDirectives(); err != nil {
		return nil, err
	}
	selection, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	return &Fragment{Name: name, TypeCondition: typeCondition, SelectionSet: selection}, nil
}

func (p *documentParser) parseFragmentSelection() (*Field, error) {
	if err := p.expectPunct("..."); err != nil {
		return nil, err
	}
	field := &Field{}
	tok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if tok.kind == tokenName && tok.text == "on" {
		_, _ = p.consume()
		field.InlineFragments = []*InlineFragment{{}}
		field.InlineFragments[0].TypeCondition, err = p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.skipDirectives(); err != nil {
			return nil, err
		}
		field.InlineFragments[0].SelectionSet, err = p.parseSelectionSet()
		return field, err
	}
	if isPunct(tok, "{") || isPunct(tok, "@") {
		field.InlineFragments = []*InlineFragment{{}}
		if err := p.skipDirectives(); err != nil {
			return nil, err
		}
		field.InlineFragments[0].SelectionSet, err = p.parseSelectionSet()
		return field, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	field.FragmentSpreads = []string{name}
	if err := p.skipDirectives(); err != nil {
		return nil, err
	}
	return field, nil
}

func (p *documentParser) skipDirectives() error {
	for {
		tok, err := p.peek()
		if err != nil {
			return err
		}
		if !isPunct(tok, "@") {
			return nil
		}
		_, _ = p.consume()
		if _, err := p.expectName(); err != nil {
			return err
		}
		if tok, err := p.peek(); err != nil {
			return err
		} else if isPunct(tok, "(") {
			if _, err := p.parseArguments(); err != nil {
				return err
			}
		}
	}
}

func (p *documentParser) peek() (token, error) {
	if p.buffer != nil {
		return *p.buffer, nil
	}
	tok, err := p.lexer.next()
	if err != nil {
		return token{}, err
	}
	p.buffer = &tok
	return tok, nil
}

func (p *documentParser) consume() (token, error) {
	tok, err := p.peek()
	if err != nil {
		return token{}, err
	}
	p.buffer = nil
	return tok, nil
}

func (p *documentParser) expectName() (string, error) {
	tok, err := p.consume()
	if err != nil {
		return "", err
	}
	if tok.kind != tokenName {
		return "", p.errorf(tok, "expected name")
	}
	return tok.text, nil
}

func (p *documentParser) expectPunct(value string) error {
	tok, err := p.consume()
	if err != nil {
		return err
	}
	if !isPunct(tok, value) {
		return p.errorf(tok, "expected %q", value)
	}
	return nil
}

func (p *documentParser) enter() error {
	p.depth++
	if p.depth > maxParseDepth {
		return fmt.Errorf("graphql: maximum nesting depth %d exceeded", maxParseDepth)
	}
	return nil
}

func (p *documentParser) leave() {
	p.depth--
}

func (p *documentParser) errorf(tok token, format string, args ...any) error {
	return fmt.Errorf("graphql: %s at offset %d", fmt.Sprintf(format, args...), tok.pos)
}

func isPunct(tok token, value string) bool {
	return tok.kind == tokenPunct && tok.text == value
}

func isOperationType(value string) bool {
	return value == "query" || value == "mutation" || value == "subscription"
}

// ParseTypeReference parses a GraphQL type reference such as [User!]!.
func ParseTypeReference(source string) (TypeRef, error) {
	p := &documentParser{lexer: lexer{source: source}}
	ref, err := p.parseTypeReferenceValue()
	if err != nil {
		return TypeRef{}, err
	}
	tok, err := p.peek()
	if err != nil {
		return TypeRef{}, err
	}
	if tok.kind != tokenEOF {
		return TypeRef{}, p.errorf(tok, "unexpected token after type reference")
	}
	return ref, nil
}

func (p *documentParser) parseTypeReferenceValue() (TypeRef, error) {
	if err := p.enter(); err != nil {
		return TypeRef{}, err
	}
	defer p.leave()

	tok, err := p.peek()
	if err != nil {
		return TypeRef{}, err
	}
	var ref TypeRef
	if isPunct(tok, "[") {
		_, _ = p.consume()
		elem, err := p.parseTypeReferenceValue()
		if err != nil {
			return TypeRef{}, err
		}
		if err := p.expectPunct("]"); err != nil {
			return TypeRef{}, err
		}
		ref.Elem = &elem
	} else {
		name, err := p.expectName()
		if err != nil {
			return TypeRef{}, err
		}
		ref.Named = name
	}
	if tok, err := p.peek(); err != nil {
		return TypeRef{}, err
	} else if isPunct(tok, "!") {
		_, _ = p.consume()
		ref.NonNull = true
	}
	return ref, nil
}

// ToGoValue converts a parsed GraphQL literal to a JSON-like Go value.
// Variable references return nil because their runtime value is not in the
// query document.
func (v Value) ToGoValue() any {
	switch v.Kind {
	case ValueInt:
		value, err := strconv.ParseFloat(v.Text, 64)
		if err != nil {
			return nil
		}
		return value
	case ValueFloat:
		value, err := strconv.ParseFloat(v.Text, 64)
		if err != nil {
			return nil
		}
		return value
	case ValueString, ValueEnum:
		return v.Text
	case ValueBoolean:
		return v.Bool
	case ValueNull, ValueVariable:
		return nil
	case ValueList:
		result := make([]any, 0, len(v.List))
		for _, item := range v.List {
			result = append(result, item.ToGoValue())
		}
		return result
	case ValueObject:
		result := make(map[string]any, len(v.Object))
		for _, field := range v.Object {
			result[field.Name] = field.Value.ToGoValue()
		}
		return result
	default:
		return nil
	}
}

// NameFromIdentifier makes a safe GraphQL type name from a field identifier.
func NameFromIdentifier(identifier string) string {
	var b strings.Builder
	upper := true
	for _, r := range identifier {
		if r == '_' || r == '-' || unicode.IsSpace(r) {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "Anonymous"
	}
	return b.String()
}
