package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cogniflow/orchestrator/internal/budget"
	"github.com/cogniflow/orchestrator/internal/dag"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/eval"
	"github.com/cogniflow/orchestrator/internal/fusion"
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
	// FusionMode selects the fusion strategy: "auto" | "heuristic" | "model".
	// Default is "auto" (heuristic for ≤1 stream, model otherwise).
	FusionMode string `json:"fusion_mode,omitempty"`
	// WorkspaceID scopes the run to a RAG workspace. Optional; defaults to
	// "default" on the server. Phase 6 wires this into the executor so
	// nodes with NeedsRAG=true retrieve from the right workspace.
	WorkspaceID string `json:"workspace,omitempty"`
	// Budget is the per-run cost + carbon cap. If supplied and the projected
	// total exceeds the cap, the executor cascade-downgrades models before
	// running. Phase 7.
	Budget *budget.Budget `json:"budget,omitempty"`
	// Eval controls whether a faithfulness judge runs after the run. Default
	// is true (the orchestrator is built for it). Phase 7.
	Eval *bool `json:"eval,omitempty"`
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
	// Phase 5: wire the fuser. The fuser merges upstream streams after the
	// DAG completes, emits a "fusion" stream, and a "manifest" event.
	mode := fusion.ModeAuto
	if req.FusionMode != "" {
		mode = fusion.Mode(req.FusionMode)
	}
	executor.Fuser = fusion.New(fusion.Config{Mode: mode}, reg)
	// Phase 6: give the executor access to RAG retrieval. Nodes whose plan
	// annotation has needs_rag=true will call this service before streaming.
	executor.RAG = s.RAG
	executor.WorkspaceID = req.WorkspaceID
	if executor.WorkspaceID == "" {
		executor.WorkspaceID = "default"
	}
	// Phase 7: budget cascade. Estimator projects cost + carbon per node
	// using the same CostTable the router uses.
	if s.CostTable != nil {
		executor.Estimator = budget.New(s.CostTable, 500, 1000)
	}
	if req.Budget != nil {
		executor.Budget = *req.Budget
	}
	// Phase 7: faithfulness judge. Eval is on by default; disabled by the
	// client via {"eval": false}.
	runEval := true
	if req.Eval != nil {
		runEval = *req.Eval
	}
	if runEval {
		executor.Judge = eval.New(s.Registry, s.CostTable, "openai:gpt-4o-mini")
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
