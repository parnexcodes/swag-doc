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
	file         *os.File
	mutex        sync.Mutex
}

// NewFileStorage creates a new file storage
func NewFileStorage(baseDir string) (*FileStorage, error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	// Generate a timestamp-based session filename with PID to avoid collisions
	sessionFile := filepath.Join(baseDir, fmt.Sprintf("session-%s-%d.json",
		time.Now().Format("20060102-150405"), os.Getpid()))

	// Create the file and write the opening bracket
	f, err := os.Create(sessionFile)
	if err != nil {
		return nil, fmt.Errorf("creating session file: %w", err)
	}
	// Write the opening bracket for JSON array
	if _, err := f.WriteString("[\n"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing session file header: %w", err)
	}

	return &FileStorage{
		baseDir:      baseDir,
		sessionFile:  sessionFile,
		transactions: []APITransaction{},
		file:         f,
	}, nil
}

// Store saves an API transaction to storage
func (s *FileStorage) Store(transaction APITransaction) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Marshal first — fail fast before any side effects
	data, err := json.Marshal(transaction)
	if err != nil {
		return err
	}

	// Write comma separator if not the first entry
	if len(s.transactions) > 0 {
		if _, err := s.file.WriteString(",\n"); err != nil {
			return err
		}
	}

	if _, err := s.file.Write(data); err != nil {
		return err
	}

	if err := s.file.Sync(); err != nil {
		return err
	}

	// Only append to in-memory list after successful write
	s.transactions = append(s.transactions, transaction)
	return nil
}

// Close finalizes the session file
func (s *FileStorage) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.file != nil {
		// Write the closing bracket
		if _, err := s.file.WriteString("\n]\n"); err != nil {
			_ = s.file.Close()
			return err
		}
		return s.file.Close()
	}
	return nil
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

	// Close the session file before removing (required on Windows)
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}

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
