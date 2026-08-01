package eval

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LoggedResult is the on-disk shape for one run's eval summary. We tag the
// Result with run_id / prompt / workspace so the dashboard endpoint can
// correlate usage events without a separate join key.
type LoggedResult struct {
	V                  string             `json:"v"`                 // "eval.v1"
	RunID              string             `json:"run_id"`
	Prompt             string             `json:"prompt,omitempty"`
	Workspace          string             `json:"workspace,omitempty"`
	StartedAt          time.Time          `json:"started_at"`
	FinishedAt         time.Time          `json:"finished_at"`
	FaithfulnessPct    float64            `json:"faithfulness_pct"`
	HallucinationFlags int                `json:"hallucination_flags"`
	CostUSD            float64            `json:"cost_usd"`
	CarbonG            float64            `json:"carbon_g"`
	LatencyTotalMS     int                `json:"latency_total_ms"`
	LatencyP95MS       int                `json:"latency_p95_ms"`
	ModelMix           []string           `json:"model_mix"`
	PerNode            map[string]NodeEval `json:"per_node,omitempty"`
	JudgedBy           string             `json:"judged_by,omitempty"`
}

// JSONLEvalLogger appends one eval summary per line to a local JSON-Lines
// file. Same wire-up pattern as meter.JSONLMeterer: opens in append mode,
// flushes at most every 250ms, no-op on failure so a logging error can't
// break the run hot path.
type JSONLEvalLogger struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	w        *bufio.Writer
	flushDur time.Duration
	last     time.Time
}

// NewJSONLEvalLogger opens (or appends to) the given file. The parent dir
// is created if missing.
func NewJSONLEvalLogger(path string) (*JSONLEvalLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLEvalLogger{
		path:     path,
		f:        f,
		w:        bufio.NewWriter(f),
		flushDur: 250 * time.Millisecond,
		last:     time.Now(),
	}, nil
}

// Path returns the on-disk file the logger writes to.
func (j *JSONLEvalLogger) Path() string { return j.path }

// Record appends one eval summary as a JSON line and flushes if the flush
// interval has elapsed. Errors are swallowed — logging must never break
// the request path.
func (j *JSONLEvalLogger) Record(res *Result, runID string, prompt string, workspace string, startedAt, finishedAt time.Time) {
	if res == nil {
		return
	}
	row := LoggedResult{
		V:                  "eval.v1",
		RunID:              runID,
		Prompt:             prompt,
		Workspace:          workspace,
		StartedAt:          startedAt.UTC(),
		FinishedAt:         finishedAt.UTC(),
		FaithfulnessPct:    res.FaithfulnessPct,
		HallucinationFlags: res.HallucinationFlags,
		CostUSD:            res.CostUSD,
		CarbonG:            res.CarbonG,
		LatencyTotalMS:     res.LatencyTotalMS,
		LatencyP95MS:       res.LatencyP95MS,
		ModelMix:           res.ModelMix,
		PerNode:            res.PerNode,
		JudgedBy:           res.JudgedBy,
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = json.NewEncoder(j.w).Encode(row)
	if time.Since(j.last) >= j.flushDur {
		_ = j.w.Flush()
		j.last = time.Now()
	}
}

// Flush drains the buffered writer to the underlying file.
func (j *JSONLEvalLogger) Flush() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.w.Flush(); err != nil {
		return err
	}
	j.last = time.Now()
	return nil
}

// Close drains + closes the underlying file.
func (j *JSONLEvalLogger) Close() error {
	if err := j.Flush(); err != nil {
		return err
	}
	return j.f.Close()
}