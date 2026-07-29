package parser

// Schema represents an OpenAPI schema
type Schema struct {
	Type         string            `json:"type,omitempty"`
	Format       string            `json:"format,omitempty"`
	Properties   map[string]Schema `json:"properties,omitempty"`
	Items        *Schema           `json:"items,omitempty"`
	Required     []string          `json:"required,omitempty"`
	Enum         []interface{}     `json:"enum,omitempty"`
	Example      interface{}       `json:"example,omitempty"`
	AllOf        []Schema          `json:"allOf,omitempty"`
	OneOf        []Schema          `json:"oneOf,omitempty"`
	AnyOf        []Schema          `json:"anyOf,omitempty"`
	Not          *Schema           `json:"not,omitempty"`
	Default      interface{}       `json:"default,omitempty"`
	Nullable     bool              `json:"nullable,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
	WriteOnly    bool              `json:"writeOnly,omitempty"`
	XML          map[string]string `json:"xml,omitempty"`
	ExternalDocs map[string]string `json:"externalDocs,omitempty"`
	Deprecated   bool              `json:"deprecated,omitempty"`
}
