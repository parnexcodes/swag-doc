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

// DefaultFlushInterval is how often the session files are synced to disk
// while the proxy is running.
const DefaultFlushInterval = 5 * time.Second

// wsSessionPrefix marks files holding WebSocket sessions. OpenAPI generation
// and GetAll skip files with this prefix; GetAllWebSocketSessions reads only
// files with it.
const wsSessionPrefix = "ws-session-"

// Storage interface for storing API transactions and WebSocket sessions
type Storage interface {
	Store(transaction APITransaction) error
	StoreWebSocketSession(session WebSocketSession) error
	GetAll() ([]APITransaction, error)
	GetAllWebSocketSessions() ([]WebSocketSession, error)
	Clear() error
}

// FileStorage stores API transactions and WebSocket sessions as JSON files
type FileStorage struct {
	baseDir       string
	sessionFile   string
	wsSessionFile string
	httpFile      *jsonArrayFile
	wsFile        *jsonArrayFile
	transactions  []APITransaction
	wsSessions    []WebSocketSession
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
// session files to disk every flushInterval. An interval <= 0 disables
// periodic syncing; the files are still synced on Close.
func NewFileStorageWithFlushInterval(baseDir string, flushInterval time.Duration) (*FileStorage, error) {
	return newFileStorage(baseDir, flushInterval)
}

// NewFileStorageReader creates a read-only view of a data directory. Unlike
// NewFileStorage it does not create session files, so generation can read
// captured data without mutating the directory. Store and Clear return an
// error; Close is a no-op.
func NewFileStorageReader(baseDir string) (*FileStorage, error) {
	return &FileStorage{
		baseDir:      baseDir,
		transactions: []APITransaction{},
		wsSessions:   []WebSocketSession{},
	}, nil
}

func newFileStorage(baseDir string, flushInterval time.Duration) (*FileStorage, error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	s := &FileStorage{
		baseDir:       baseDir,
		transactions:  []APITransaction{},
		wsSessions:    []WebSocketSession{},
		flushInterval: flushInterval,
	}

	// Create both session files exclusively: a second storage in the same
	// process must not truncate existing session files. On a name collision
	// both files are retried with an incrementing suffix.
	for attempt := 0; ; attempt++ {
		suffix := fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), os.Getpid())
		if attempt > 0 {
			suffix = fmt.Sprintf("%s-%d", suffix, attempt)
		}

		httpPath := filepath.Join(baseDir, "session-"+suffix+".json")
		httpFile, err := newJSONArrayFile(httpPath)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return nil, fmt.Errorf("creating session file: %w", err)
		}

		wsPath := filepath.Join(baseDir, wsSessionPrefix+suffix+".json")
		wsFile, err := newJSONArrayFile(wsPath)
		if err != nil {
			_ = httpFile.close()
			if os.IsExist(err) {
				continue
			}
			return nil, fmt.Errorf("creating websocket session file: %w", err)
		}

		s.sessionFile = httpPath
		s.wsSessionFile = wsPath
		s.httpFile = httpFile
		s.wsFile = wsFile
		break
	}

	s.startFlusher()
	return s, nil
}

// startFlusher starts the background goroutine that periodically syncs the
// session files to disk. Transactions remain buffered in the OS page cache
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
				if err := s.syncFiles(); err != nil {
					log.Printf("Error syncing session files: %v", err)
				}
				s.mutex.Unlock()
			case <-s.stopFlush:
				return
			}
		}
	}()
}

// syncFiles syncs all open session files to disk. Callers must hold mutex.
func (s *FileStorage) syncFiles() error {
	if s.httpFile != nil {
		if err := s.httpFile.sync(); err != nil {
			return err
		}
	}
	if s.wsFile != nil {
		if err := s.wsFile.sync(); err != nil {
			return err
		}
	}
	return nil
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
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.httpFile == nil {
		return fmt.Errorf("storage is closed")
	}

	if err := s.httpFile.append(transaction); err != nil {
		return err
	}

	// Only append to in-memory list after successful write
	s.transactions = append(s.transactions, transaction)
	return nil
}

// StoreWebSocketSession saves a WebSocket session to storage
func (s *FileStorage) StoreWebSocketSession(session WebSocketSession) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.wsFile == nil {
		return fmt.Errorf("storage is closed")
	}

	if err := s.wsFile.append(session); err != nil {
		return err
	}

	// Only append to in-memory list after successful write
	s.wsSessions = append(s.wsSessions, session)
	return nil
}

// Close finalizes the session files
func (s *FileStorage) Close() error {
	s.stopFlusher()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	var firstErr error
	if s.httpFile != nil {
		if err := s.httpFile.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.wsFile != nil {
		if err := s.wsFile.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GetAll returns all stored API transactions. WebSocket session files are
// ignored.
func (s *FileStorage) GetAll() ([]APITransaction, error) {
	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, err
	}

	var allTransactions []APITransaction
	for _, file := range files {
		if !isSessionJSONFile(file, false) {
			continue
		}

		items, err := readJSONArrayFile[APITransaction](filepath.Join(s.baseDir, file.Name()))
		if err == nil {
			allTransactions = append(allTransactions, items...)
		}
	}

	return allTransactions, nil
}

// GetAllWebSocketSessions returns all stored WebSocket sessions, reading
// only ws-session-*.json files.
func (s *FileStorage) GetAllWebSocketSessions() ([]WebSocketSession, error) {
	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, err
	}

	var allSessions []WebSocketSession
	for _, file := range files {
		if !isSessionJSONFile(file, true) {
			continue
		}

		items, err := readJSONArrayFile[WebSocketSession](filepath.Join(s.baseDir, file.Name()))
		if err == nil {
			allSessions = append(allSessions, items...)
		}
	}

	return allSessions, nil
}

// isSessionJSONFile reports whether a directory entry is a JSON session
// file: not a directory, .json suffix, and (when ws is true) the ws-session-
// prefix or (when ws is false) not the ws-session- prefix.
func isSessionJSONFile(entry os.DirEntry, ws bool) bool {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
		return false
	}
	isWS := strings.HasPrefix(entry.Name(), wsSessionPrefix)
	return isWS == ws
}

// readJSONArrayFile decodes a JSON file that is either a JSON array
// (streamed) or a single JSON object (legacy single-transaction files).
func readJSONArrayFile[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Peek the first byte to distinguish array files from legacy
	// single-transaction files (JSON objects)
	var first [1]byte
	if n, _ := f.Read(first[:]); n == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// Stream-decode so only one file's contents are live at a time
	dec := json.NewDecoder(f)
	if first[0] == '[' {
		var items []T
		if err := dec.Decode(&items); err != nil {
			return nil, err
		}
		return items, nil
	}
	var single T
	if err := dec.Decode(&single); err != nil {
		return nil, err
	}
	return []T{single}, nil
}

// ReadWebSocketSessionsFromFile reads WebSocket sessions from one file,
// accepting either an array of sessions or a single session object.
func ReadWebSocketSessionsFromFile(filename string) ([]WebSocketSession, error) {
	return readJSONArrayFile[WebSocketSession](filename)
}

// Clear removes all stored API transactions and WebSocket sessions
func (s *FileStorage) Clear() error {
	s.stopFlusher()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.httpFile == nil && s.wsFile == nil {
		return fmt.Errorf("storage is not open for writing")
	}

	// Reset in-memory transactions
	s.transactions = []APITransaction{}
	s.wsSessions = []WebSocketSession{}

	// Close the session files before removing (required on Windows)
	var firstErr error
	if s.httpFile != nil {
		if err := s.httpFile.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.wsFile != nil {
		if err := s.wsFile.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
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

		if err := os.Remove(filepath.Join(s.baseDir, file.Name())); err != nil {
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

// WebSocketSessionInterceptor creates a function to intercept and store
// WebSocket sessions.
func WebSocketSessionInterceptor(storage Storage) func(WebSocketSession) {
	return func(session WebSocketSession) {
		if err := storage.StoreWebSocketSession(session); err != nil {
			log.Printf("Error storing WebSocket session: %v", err)
		}
	}
}

// jsonArrayFile is an append-only JSON array file: objects are appended as
// comma-separated entries between "[\n" and "\n]\n". Writes are buffered in
// the OS page cache until sync or close.
type jsonArrayFile struct {
	file  *os.File
	count int
}

// newJSONArrayFile creates the file exclusively, writing the opening bracket.
func newJSONArrayFile(path string) (*jsonArrayFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString("[\n"); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &jsonArrayFile{file: f}, nil
}

// append marshals obj and appends it as the next array entry. On marshal or
// write failure the file is left in its previous state.
func (j *jsonArrayFile) append(obj any) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if j.count > 0 {
		if _, err := j.file.WriteString(",\n"); err != nil {
			return err
		}
	}
	if _, err := j.file.Write(data); err != nil {
		return err
	}
	j.count++
	return nil
}

func (j *jsonArrayFile) sync() error {
	if j.file == nil {
		return nil
	}
	return j.file.Sync()
}

// close writes the closing bracket, syncs, and closes the file.
func (j *jsonArrayFile) close() error {
	if j.file == nil {
		return nil
	}
	if _, err := j.file.WriteString("\n]\n"); err != nil {
		_ = j.file.Close()
		j.file = nil
		return err
	}
	if err := j.file.Sync(); err != nil {
		_ = j.file.Close()
		j.file = nil
		return err
	}
	err := j.file.Close()
	j.file = nil
	return err
}
