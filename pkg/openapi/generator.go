package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// Generator creates OpenAPI documentation from recorded API calls
type Generator struct {
	storage     proxy.Storage
	title       string
	description string
	version     string
	basePath    string
}

// NewGenerator creates a new generator
func NewGenerator(storage proxy.Storage, title, description, version, basePath string) *Generator {
	return &Generator{
		storage:     storage,
		title:       title,
		description: description,
		version:     version,
		basePath:    basePath,
	}
}

// Generate creates an OpenAPI specification from the recorded API calls
func (g *Generator) Generate(outputPath string) error {
	// Get all transactions
	transactions, err := g.storage.GetAll()
	if err != nil {
		return err
	}

	// Create config
	config := OpenAPIConfig{
		Title:       g.title,
		Description: g.description,
		Version:     g.version,
		Servers: []OpenAPIServer{
			{
				URL:         g.basePath,
				Description: "API Server",
			},
		},
	}

	// Create generator
	generator := NewOpenAPIGenerator(config)

	// Add transactions
	for _, tx := range transactions {
		generator.AddTransaction(tx)
	}

	// Generate spec
	spec, err := generator.GenerateSpec()
	if err != nil {
		return err
	}

	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return err
	}

	return nil
}
