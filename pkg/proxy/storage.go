package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Storage interface for storing API transactions
type Storage interface {
	Store(transaction APITransaction) error
	GetAll() ([]APITransaction, error)
}

// FileStorage implements Storage using the file system
type FileStorage struct {
	dataDir string
	mutex   sync.Mutex
}

// NewFileStorage creates a new FileStorage instance
func NewFileStorage(dataDir string) (*FileStorage, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &FileStorage{
		dataDir: dataDir,
	}, nil
}

// Store saves an API transaction to a file
func (s *FileStorage) Store(transaction APITransaction) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create a filename based on the timestamp and path
	timestamp := transaction.Request.Timestamp.Format("20060102-150405.000")
	method := transaction.Request.Method
	path := transaction.Request.Path

	// Replace any invalid characters in the path
	path = filepath.Base(path)
	if path == "" || path == "." || path == "/" {
		path = "root"
	}

	filename := fmt.Sprintf("%s-%s-%s.json", timestamp, method, path)
	filepath := filepath.Join(s.dataDir, filename)

	// Marshal the transaction to JSON
	data, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal transaction: %w", err)
	}

	// Write the JSON to a file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write transaction to file: %w", err)
	}

	return nil
}

// GetAll returns all stored API transactions
func (s *FileStorage) GetAll() ([]APITransaction, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	files, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var transactions []APITransaction
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filepath := filepath.Join(s.dataDir, file.Name())
		data, err := os.ReadFile(filepath)
		if err != nil {
			return nil, fmt.Errorf("failed to read transaction file %s: %w", file.Name(), err)
		}

		var transaction APITransaction
		if err := json.Unmarshal(data, &transaction); err != nil {
			return nil, fmt.Errorf("failed to unmarshal transaction file %s: %w", file.Name(), err)
		}

		transactions = append(transactions, transaction)
	}

	return transactions, nil
}

// TransactionInterceptor creates an APIInterceptor function that stores transactions
func TransactionInterceptor(storage Storage) APIInterceptor {
	return func(transaction APITransaction) {
		if err := storage.Store(transaction); err != nil {
			fmt.Printf("Error storing transaction: %v\n", err)
		}
	}
}
