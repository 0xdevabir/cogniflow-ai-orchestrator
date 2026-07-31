package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cogniflow/orchestrator/internal/dag"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

// runRequest is the body the web app POSTs to /v1/run.
//
// Either pass an explicit Plan (e.g. from a prior /v1/plan call) OR pass a
// prompt + model; the orchestrator will decompose + route + execute in one
// shot. The Plan path is the common case for the playground UX.
type runRequest struct {
	// Plan is the explicit DAG to run. Optional.
	Plan *decomposer.Plan `json:"plan,omitempty"`
	// Prompt + Model trigger the decompose-then-run path. Optional.
	Prompt string `json:"prompt,omitempty"`
	Model  string `json:"model,omitempty"`
	// ExecutorMode overrides the global default; "local" or "temporal".
	ExecutorMode string `json:"executor_mode,omitempty"`
	// Parallelism overrides the executor's default (4).
	Parallelism int `json:"parallelism,omitempty"`
}

// HandleRun is the SSE endpoint that executes a Plan (or a prompt) in
// parallel goroutines and streams node_status + chunk + done events.
//
// Wire format (Server-Sent Events, text/event-stream):
//
//	event: plan
//	data: {"version":"plan.v1","total_nodes":4,"levels":2}
//
//	event: node_status
//	data: {"node_id":"n1","status":"running","model":"...","score":0.86,"breakdown":{...},"reason":"..."}
//
//	event: chunk
//	data: {"v":"chunk.v1","stream_id":"n1","node_id":"n1","model":"...","text":"..."}
//
//	event: node_status
//	data: {"node_id":"n1","status":"ok"}
//
//	event: done
//	data: {"ok":true,"total_nodes":4,"cancelled":false}
//
// On error:
//
//	event: error
//	data: {"message":"...","code":"..."}
func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported", "no_flusher")
		return
	}

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "bad_json")
		return
	}

	// Resolve the Plan: either it was passed in directly, or we decompose.
	plan := req.Plan
	if plan == nil {
		if req.Prompt == "" {
			writeJSONError(w, http.StatusBadRequest, "either plan or prompt is required", "missing_input")
			return
		}
		if s.Decomposer == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "decomposer not configured", "no_decomposer")
			return
		}
		decomp := s.Decomposer
		if req.Model != "" {
			d := decomp.Deps()
			d.Model = req.Model
			decomp = decomposer.New(d)
		}
		var err error
		plan, err = decomp.Decompose(r.Context(), req.Prompt)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error(), "decompose_failed")
			return
		}
	}

	if err := dag.Validate(plan.Nodes, plan.Edges); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_plan")
		return
	}

	// Executor mode.
	if req.ExecutorMode == "temporal" {
		_ = writeSSE(w, flusher, "error", chatError{
			Message: "Temporal executor not implemented yet (Phase 8)",
			Code:    "temporal_unavailable",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Build a registry; if no real keys, fall back to the default mock.
	reg := s.Registry
	if reg == nil {
		reg = providers.NewRegistry(nil)
	}

	executor := dag.New(plan, s.Router, reg, &runSink{w: w, flusher: flusher})
	if req.Parallelism > 0 {
		executor.Parallelism = req.Parallelism
	}

	// Use a child context we can cancel cleanly if the client disconnects.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := executor.Run(ctx); err != nil {
		_ = writeSSE(w, flusher, "error", chatError{
			Message: err.Error(),
			Code:    "run_failed",
		})
		return
	}
}

// runSink adapts an http.ResponseWriter to dag.Sink.
type runSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func (s *runSink) Emit(event string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeSSE(s.w, s.flusher, event, data)
}
