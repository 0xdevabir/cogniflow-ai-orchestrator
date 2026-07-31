package meter

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONLMeterer appends every event to a local JSON-Lines file. Useful for
// dev + a future batched-Stripe replacer (Phase 8+). Lines are flushed
// after a short interval so a process crash loses at most ~flushInterval
// seconds of usage data.
type JSONLMeterer struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	w        *bufio.Writer
	flushDur time.Duration
	last     time.Time
}

// NewJSONLMeterer opens (or appends to) the given file. The parent dir is
// created if missing. Lines are flushed to disk at most every 250ms.
func NewJSONLMeterer(path string) (*JSONLMeterer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLMeterer{
		path:     path,
		f:        f,
		w:        bufio.NewWriter(f),
		flushDur: 250 * time.Millisecond,
		last:     time.Now(),
	}, nil
}

// Path returns the on-disk file the meter writes to.
func (j *JSONLMeterer) Path() string { return j.path }

// Record appends one event as a JSON line and flushes if the flush
// interval has elapsed.
func (j *JSONLMeterer) Record(ev Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = json.NewEncoder(j.w).Encode(ev)
	if time.Since(j.last) >= j.flushDur {
		_ = j.w.Flush()
		j.last = time.Now()
	}
}

// Flush drains the buffered writer to the underlying file.
func (j *JSONLMeterer) Flush() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.w.Flush(); err != nil {
		return err
	}
	j.last = time.Now()
	return nil
}

// Close drains + closes the underlying file. The meter is unusable after.
func (j *JSONLMeterer) Close() error {
	if err := j.Flush(); err != nil {
		return err
	}
	return j.f.Close()
}
