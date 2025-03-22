package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Storage interface for storing API transactions
type Storage interface {
	Store(transaction APITransaction) error
	GetAll() ([]APITransaction, error)
	Clear() error
}

// FileStorage stores API transactions as JSON files
type FileStorage struct {
	baseDir      string
	sessionFile  string
	transactions []APITransaction
	mutex        sync.Mutex
}

// NewFileStorage creates a new file storage
func NewFileStorage(baseDir string) (*FileStorage, error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	// Generate a timestamp-based session filename
	sessionFile := filepath.Join(baseDir, fmt.Sprintf("session-%s.json", time.Now().Format("20060102-150405")))

	return &FileStorage{
		baseDir:      baseDir,
		sessionFile:  sessionFile,
		transactions: []APITransaction{},
	}, nil
}

// Store saves an API transaction to storage
func (s *FileStorage) Store(transaction APITransaction) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Add transaction to in-memory list
	s.transactions = append(s.transactions, transaction)

	// Write all transactions to the session file
	data, err := json.MarshalIndent(s.transactions, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(s.sessionFile, data, 0644)
}

// GetAll returns all stored API transactions
func (s *FileStorage) GetAll() ([]APITransaction, error) {
	// First, load all transactions from session files
	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, err
	}

	var allTransactions []APITransaction

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(s.baseDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// Check if it's a session file (containing an array)
		if len(data) > 0 && data[0] == '[' {
			var transactions []APITransaction
			if err := json.Unmarshal(data, &transactions); err == nil {
				allTransactions = append(allTransactions, transactions...)
			}
		} else {
			// For backward compatibility - handle single transaction files
			var transaction APITransaction
			if err := json.Unmarshal(data, &transaction); err == nil {
				allTransactions = append(allTransactions, transaction)
			}
		}
	}

	return allTransactions, nil
}

// Clear removes all stored API transactions
func (s *FileStorage) Clear() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Reset in-memory transactions
	s.transactions = []APITransaction{}

	// Remove all files from the directory
	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(s.baseDir, file.Name())
		if err := os.Remove(filePath); err != nil {
			return err
		}
	}

	return nil
}

// TransactionInterceptor creates a function to intercept and store API transactions
func TransactionInterceptor(storage Storage) func(APITransaction) {
	return func(transaction APITransaction) {
		if err := storage.Store(transaction); err != nil {
			log.Printf("Error storing API transaction: %v", err)
		}
	}
}
