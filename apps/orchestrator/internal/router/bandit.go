package router

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// FeedbackLogger appends events to a destination.
type FeedbackLogger interface {
	Append(ev FeedbackEvent) error
	Close() error
	Path() string
}

// JSONLFeedbackLogger appends one FeedbackEvent per line to a file.
type JSONLFeedbackLogger struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// NewJSONLFeedbackLogger opens (or creates) the file in append mode.
// Returns an error if the path cannot be opened.
func NewJSONLFeedbackLogger(path string) (*JSONLFeedbackLogger, error) {
	if path == "" {
		return nil, fmt.Errorf("router: empty bandit log path")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLFeedbackLogger{f: f, path: path}, nil
}

// Append marshals the event to JSON and writes a single line.
func (l *JSONLFeedbackLogger) Append(ev FeedbackEvent) error {
	if l == nil || l.f == nil {
		return nil
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.f.Write(b)
	return err
}

// Close closes the underlying file.
func (l *JSONLFeedbackLogger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// Path returns the file path.
func (l *JSONLFeedbackLogger) Path() string { return l.path }