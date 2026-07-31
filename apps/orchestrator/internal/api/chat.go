// Package api hosts HTTP + SSE handlers for the orchestrator.
//
// Endpoints:
//   GET  /healthz                              (Phase 0)
//   POST /v1/chat  → SSE event stream          (Phase 1)
//   POST /v1/plan  → JSON                      (Phase 2)
//   POST /v1/run   → SSE event stream          (Phase 4)
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/entity"
	"github.com/cogniflow/orchestrator/internal/meter"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/rag"
	"github.com/cogniflow/orchestrator/internal/router"
)

// Server holds the dependency-injected collaborators.
type Server struct {
	Registry    providers.Registry
	Decomposer  *decomposer.Decomposer
	Router      router.Router // optional; nil disables per-node routing
	RAG         *rag.Service  // optional; nil disables docs + retrieval
	EntityStore entity.Store  // optional; NoopStore in Phase 6
	CostTable   *router.CostTable // optional; used by the budget cascade (Phase 7)
	Meter       meter.Meterer     // optional; NoopMeterer when unset (Phase 8)
}

// chatRequest is the JSON body the web app POSTs.
type chatRequest struct {
	Prompt         string `json:"prompt"`
	Model          string `json:"model,omitempty"`          // e.g. "openai:gpt-4o-mini"
	ConversationID string `json:"conversation_id,omitempty"`
	// Workspace scopes RAG retrieval for the single-node chat path. Optional.
	Workspace string `json:"workspace,omitempty"`
}

// chatError is the payload of an `event: error` SSE event.
type chatError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// HandleChat is the SSE endpoint: POST /v1/chat.
//
// Wire format (Server-Sent Events, text/event-stream):
//
//	event: node_status
//	data: {"node_id":"default","status":"running","model":"openai:gpt-4o-mini"}
//
//	event: chunk
//	data: {"v":"chunk.v1","stream_id":"default","text":"Here is..."}
//
//	event: done
//	data: {"ok":true}
//
// On error:
//
//	event: error
//	data: {"message":"...","code":"..."}
func (s *Server) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "bad_json")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required", "missing_prompt")
		return
	}

	// Default model if none specified.
	if req.Model == "" {
		req.Model = "openai:gpt-4o-mini"
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported", "no_flusher")
		return
	}

	// SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	streamID := req.ConversationID
	if streamID == "" {
		streamID = "default"
	}
	nodeID := streamID // single-node Phase 1 chat

	// Phase 6: if a RAG service is wired and a workspace was provided, inject
	// the top-k retrieved docs into the system prompt. The single-node chat
	// is the simplest place to demonstrate RAG end-to-end (no DAG needed).
	var injectedSections []rag.InjectedSection
	systemMsg := ""
	if s.RAG != nil {
		injection, sections, err := s.RAG.BuildInjectedSystemPrompt(r.Context(), defaultWorkspaceFromRequest(r, req.Workspace), req.Prompt)
		if err == nil {
			systemMsg = injection
			injectedSections = sections
		}
	}

	convReq := providers.Request{
		Prompt:    req.Prompt,
		Model:     req.Model,
		SystemMsg: systemMsg,
		StreamID:  streamID,
		NodeID:    nodeID,
	}

	streamer, err := s.Registry.Get(req.Model)
	if err != nil {
		writeSSEError(w, flusher, chatError{Message: err.Error(), Code: "no_provider"})
		return
	}

	// Emit "node_status: running" before the first chunk.
	writeSSE(w, flusher, "node_status", map[string]any{
		"node_id": nodeID,
		"status":  "running",
		"model":   req.Model,
	})

	sink := &sseSink{w: w, flusher: flusher, finished: false}

	// Stream. The streamer MUST emit a final Chunk with Finish:true.
	if err := streamer.Stream(r.Context(), convReq, sink); err != nil {
		// Context cancellation is the normal disconnect path — not an error.
		if !errors.Is(err, context.Canceled) {
			writeSSE(w, flusher, "error", chatError{Message: err.Error(), Code: "stream_error"})
		}
	}

	// Phase 6: emit a manifest of retrieved docs so the web client can show
	// "answered from N docs" badges + per-citation provenance in the hover
	// card. Phase 5 fusion emits the same shape for the multi-node case.
	if len(injectedSections) > 0 {
		spans := make([]map[string]any, 0, len(injectedSections))
		for i, sec := range injectedSections {
			spans = append(spans, map[string]any{
				"id":         fmt.Sprintf("sp_%d_%s", i+1, streamID),
				"sub_task_id": nodeID,
				"model":      req.Model,
				"text":       sec.Text,
				"doc_id":     sec.DocID,
				"char_start": sec.CharStart,
				"char_end":   sec.CharEnd,
			})
		}
		writeSSE(w, flusher, "manifest", map[string]any{"v": "citation.v1", "spans": spans})
	}

	// Emit "done" so the client knows the connection is closing cleanly.
	writeSSE(w, flusher, "done", map[string]any{"ok": true, "node_id": nodeID})
}

// sseSink adapts an http.ResponseWriter to providers.ChunkSink.
type sseSink struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	finished bool
}

func (s *sseSink) Send(ctx context.Context, c providers.Chunk) error {
	if s.finished {
		// Don't emit past Finish:true (Phase 4 might still call us via cancel).
		return nil
	}
	if c.Finish {
		s.finished = true
	}
	return writeSSE(s.w, s.flusher, "chunk", c)
}

// writeSSE emits a properly-formatted SSE event and flushes.
func writeSSE(w http.ResponseWriter, f http.Flusher, event string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(body)); err != nil {
		return err
	}
	f.Flush()
	return nil
}

func writeSSEError(w http.ResponseWriter, f http.Flusher, e chatError) {
	_ = writeSSE(w, f, "error", e)
}
