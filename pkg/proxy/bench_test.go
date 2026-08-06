package proxy

import (
	"testing"
)

// benchTransaction returns a small realistic transaction reused by the store
// benchmarks.
func benchTransaction() APITransaction {
	return APITransaction{
		Request: RequestData{
			Method: "POST",
			Path:   "/resource0/items",
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"query":1,"text":"search term"}`),
		},
		Response: ResponseData{
			StatusCode: 200,
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"id":1,"name":"resource-1","active":true,"score":0.5,"items":[0,1,2]}`),
		},
	}
}

func BenchmarkFileStorageStore(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	storage, err := NewFileStorage(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = storage.Close() }()

	tx := benchTransaction()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := storage.Store(tx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFileStorageStoreConcurrent(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	storage, err := NewFileStorage(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = storage.Close() }()

	tx := benchTransaction()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := storage.Store(tx); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
