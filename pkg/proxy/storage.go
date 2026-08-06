package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultFlushInterval is how often the session file is synced to disk while
// the proxy is running.
const DefaultFlushInterval = 5 * time.Second

// Storage interface for storing API transactions
type Storage interface {
	Store(transaction APITransaction) error
	GetAll() ([]APITransaction, error)
	Clear() error
}

// FileStorage stores API transactions as JSON files
type FileStorage struct {
	baseDir       string
	sessionFile   string
	transactions  []APITransaction
	file          *os.File
	mutex         sync.Mutex
	flushInterval time.Duration
	stopFlush     chan struct{}
	flushDone     chan struct{}
	flushStopOnce sync.Once
}

// NewFileStorage creates a new file storage
func NewFileStorage(baseDir string) (*FileStorage, error) {
	return newFileStorage(baseDir, DefaultFlushInterval)
}

// NewFileStorageWithFlushInterval creates a new file storage that syncs the
// session file to disk every flushInterval. An interval <= 0 disables
// periodic syncing; the file is still synced on Close.
func NewFileStorageWithFlushInterval(baseDir string, flushInterval time.Duration) (*FileStorage, error) {
	return newFileStorage(baseDir, flushInterval)
}

// NewFileStorageReader creates a read-only view of a data directory. Unlike
// NewFileStorage it does not create a session file, so generation can read
// captured data without mutating the directory. Store and Clear return an
// error; Close is a no-op.
func NewFileStorageReader(baseDir string) (*FileStorage, error) {
	return &FileStorage{
		baseDir:      baseDir,
		transactions: []APITransaction{},
	}, nil
}

func newFileStorage(baseDir string, flushInterval time.Duration) (*FileStorage, error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	// Generate a timestamp-based session filename with PID to avoid collisions
	sessionFile := filepath.Join(baseDir, fmt.Sprintf("session-%s-%d.json",
		time.Now().Format("20060102-150405"), os.Getpid()))

	// Create the file exclusively: a second storage in the same process and
	// second must not truncate an existing session file.
	f, err := os.OpenFile(sessionFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if !os.IsExist(err) {
			return nil, fmt.Errorf("creating session file: %w", err)
		}
		// Name collision — retry with an incrementing suffix
		for i := 1; ; i++ {
			sessionFile = filepath.Join(baseDir, fmt.Sprintf("session-%s-%d-%d.json",
				time.Now().Format("20060102-150405"), os.Getpid(), i))
			f, err = os.OpenFile(sessionFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err == nil {
				break
			}
			if !os.IsExist(err) {
				return nil, fmt.Errorf("creating session file: %w", err)
			}
		}
	}
	// Write the opening bracket for JSON array
	if _, err := f.WriteString("[\n"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing session file header: %w", err)
	}

	s := &FileStorage{
		baseDir:       baseDir,
		sessionFile:   sessionFile,
		transactions:  []APITransaction{},
		file:          f,
		flushInterval: flushInterval,
	}
	s.startFlusher()
	return s, nil
}

// startFlusher starts the background goroutine that periodically syncs the
// session file to disk. Transactions remain buffered in the OS page cache
// between syncs; Close always performs a final sync.
func (s *FileStorage) startFlusher() {
	if s.flushInterval <= 0 {
		return
	}
	s.stopFlush = make(chan struct{})
	s.flushDone = make(chan struct{})
	go func() {
		defer close(s.flushDone)
		ticker := time.NewTicker(s.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mutex.Lock()
				if s.file != nil {
					if err := s.file.Sync(); err != nil {
						log.Printf("Error syncing session file: %v", err)
					}
				}
				s.mutex.Unlock()
			case <-s.stopFlush:
				return
			}
		}
	}()
}

// stopFlusher stops the background sync goroutine and waits for it to exit.
// Safe to call multiple times.
func (s *FileStorage) stopFlusher() {
	s.flushStopOnce.Do(func() {
		if s.stopFlush != nil {
			close(s.stopFlush)
			<-s.flushDone
		}
	})
}

// Store saves an API transaction to storage
func (s *FileStorage) Store(transaction APITransaction) error {
	// Marshal first — fail fast before any side effects
	data, err := json.Marshal(transaction)
	if err != nil {
		return err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.file == nil {
		return fmt.Errorf("storage is closed")
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

	// Only append to in-memory list after successful write
	s.transactions = append(s.transactions, transaction)
	return nil
}

// Close finalizes the session file
func (s *FileStorage) Close() error {
	s.stopFlusher()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.file != nil {
		// Write the closing bracket
		if _, err := s.file.WriteString("\n]\n"); err != nil {
			_ = s.file.Close()
			s.file = nil
			return err
		}
		// Flush buffered data to disk before closing
		if err := s.file.Sync(); err != nil {
			_ = s.file.Close()
			s.file = nil
			return err
		}
		err := s.file.Close()
		s.file = nil
		return err
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
		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		// Peek the first byte to distinguish session files (JSON arrays)
		// from legacy single-transaction files (JSON objects)
		var first [1]byte
		if n, _ := f.Read(first[:]); n == 0 {
			_ = f.Close()
			continue
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			_ = f.Close()
			continue
		}

		// Stream-decode so only one file's contents are live at a time
		dec := json.NewDecoder(f)
		if first[0] == '[' {
			var transactions []APITransaction
			if err := dec.Decode(&transactions); err == nil {
				allTransactions = append(allTransactions, transactions...)
			}
		} else {
			// For backward compatibility - handle single transaction files
			var transaction APITransaction
			if err := dec.Decode(&transaction); err == nil {
				allTransactions = append(allTransactions, transaction)
			}
		}
		_ = f.Close()
	}

	return allTransactions, nil
}

// Clear removes all stored API transactions
func (s *FileStorage) Clear() error {
	s.stopFlusher()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.file == nil {
		return fmt.Errorf("storage is not open for writing")
	}

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
