// Package testcorpus builds deterministic synthetic API transaction corpora
// for benchmarks and performance-verification tests.
package testcorpus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// Options configures corpus generation.
type Options struct {
	// N is the total number of transactions.
	N int
	// Paths is the number of distinct request paths; 0 defaults to 10.
	Paths int
	// DistinctBodies is the number of structurally distinct body shapes;
	// 0 defaults to N.
	DistinctBodies int
	// RepeatedBodies cycles transactions through DistinctBodies byte-identical
	// body shapes. When false, every transaction receives a structurally
	// unique body.
	RepeatedBodies bool
}

// Build returns a deterministic corpus of N transactions.
func Build(opts Options) []proxy.APITransaction {
	if opts.N <= 0 {
		return nil
	}
	if opts.Paths <= 0 {
		opts.Paths = 10
	}
	if opts.DistinctBodies <= 0 {
		opts.DistinctBodies = opts.N
	}

	paths := make([]string, opts.Paths)
	for i := range paths {
		paths[i] = fmt.Sprintf("/resource%d/items", i)
	}

	methods := []string{"GET", "POST", "PUT"}

	txs := make([]proxy.APITransaction, opts.N)
	for i := range txs {
		path := paths[i%opts.Paths]
		method := methods[i%len(methods)]

		txs[i] = proxy.APITransaction{
			Request: proxy.RequestData{
				Method: method,
				Path:   path,
				Headers: map[string][]string{
					"Content-Type": {"application/json"},
				},
			},
			Response: proxy.ResponseData{
				StatusCode: 200,
				Headers: map[string][]string{
					"Content-Type": {"application/json"},
				},
				Body: responseBody(i, opts),
			},
		}

		if method == "POST" {
			txs[i].Request.Body = requestBody(i, opts)
		}
	}
	return txs
}

// responseBody returns a deterministic JSON body. In repeated mode the body
// is byte-identical for every occurrence of the same shape (values derive
// from the shape index), so the sanitize cache is exercised; otherwise every
// transaction gets a structurally unique body.
func responseBody(i int, opts Options) []byte {
	shape := i % opts.DistinctBodies
	seed := i
	if opts.RepeatedBodies {
		seed = shape
	}
	arrLen := 1 + shape%5
	hasExtra := (shape/5)%2 == 1
	hasNested := (shape/10)%2 == 1

	items := make([]interface{}, arrLen)
	for j := range items {
		items[j] = j + seed
	}

	m := map[string]interface{}{
		"id":     seed,
		"name":   fmt.Sprintf("resource-%d", seed),
		"active": seed%2 == 0,
		"score":  float64(seed) / 2,
		"items":  items,
	}
	if hasExtra {
		m["extra"] = fmt.Sprintf("extra-%d", seed)
	}
	if hasNested {
		m["nested"] = map[string]interface{}{
			"created": fmt.Sprintf("2026-01-%02d", seed%28+1),
			"tags":    []interface{}{"alpha", "beta", "gamma"},
		}
	}
	if !opts.RepeatedBodies {
		// A unique key per transaction makes every sanitized body distinct.
		m[fmt.Sprintf("unique_field_%d", i)] = i
	}

	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return data
}

// requestBody returns a deterministic request body. In repeated mode it is a
// single constant body across all transactions (exercising the cache);
// otherwise it is unique per transaction.
func requestBody(i int, opts Options) []byte {
	seed := i
	if opts.RepeatedBodies {
		seed = 0
	}
	data, err := json.Marshal(map[string]interface{}{
		"query": seed,
		"text":  fmt.Sprintf("search term %d", seed),
	})
	if err != nil {
		panic(err)
	}
	return data
}

// WriteSessionFile writes transactions to a session file in the same layout
// FileStorage produces: a JSON array, one transaction per line.
func WriteSessionFile(dir, name string, txs []proxy.APITransaction) error {
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i, tx := range txs {
		if i > 0 {
			buf.WriteString(",\n")
		}
		data, err := json.Marshal(tx)
		if err != nil {
			return err
		}
		buf.Write(data)
	}
	buf.WriteString("\n]\n")
	return os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644)
}
