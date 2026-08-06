package openapi

import (
	"testing"

	"github.com/parnexcodes/swag-doc/internal/testcorpus"
)

func benchConfig() OpenAPIConfig {
	return OpenAPIConfig{
		Title:           "Bench API",
		Description:     "Benchmark corpus",
		Version:         "1.0.0",
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
	}
}

func BenchmarkGenerateSpec(b *testing.B) {
	cases := []struct {
		name string
		opts testcorpus.Options
	}{
		{
			name: "RepeatedBodies/500",
			opts: testcorpus.Options{N: 500, Paths: 25, DistinctBodies: 20, RepeatedBodies: true},
		},
		{
			name: "RepeatedBodies/2000",
			opts: testcorpus.Options{N: 2000, Paths: 50, DistinctBodies: 20, RepeatedBodies: true},
		},
		{
			name: "DistinctBodies/500",
			opts: testcorpus.Options{N: 500, Paths: 25, DistinctBodies: 500, RepeatedBodies: false},
		},
		{
			name: "DistinctBodies/2000",
			opts: testcorpus.Options{N: 2000, Paths: 50, DistinctBodies: 2000, RepeatedBodies: false},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			corpus := testcorpus.Build(tc.opts)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g := NewOpenAPIGenerator(benchConfig())
				for _, tx := range corpus {
					g.AddTransaction(tx)
				}
				if _, err := g.GenerateSpec(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
