// Package providers — vendor-agnostic streaming layer for AI models.
//
// This is the architectural keystone of CogniFlow: every model integration
// goes through the Streamer interface, so the orchestrator, fusion engine,
// and web client never need to know which provider they are talking to.
//
// Wire format MUST match packages/schemas/chunk.schema.json (json tags are
// snake_case). The Phase 5 fusion engine and the Phase 6 RAG layer extend
// SpanRef with the optional DocID / CharStart / CharEnd fields.
package providers

import (
	"context"
	"encoding/json"
)

// Request is what the orchestrator sends to a model.
type Request struct {
	Prompt      string  // the user / sub-task prompt
	Model       string  // fully-qualified: "openai:gpt-4o-mini", "anthropic:claude-3-5-sonnet-latest", "mock:echo"
	SystemMsg   string  // optional system prompt
	MaxTokens   int     // 0 = let the provider decide
	Temperature float64 // 0..1; 0 = default
	StreamID    string  // sub-task id; "default" in Phase 1
	NodeID      string  // sub-task node id; "default" in Phase 1
	// TopP is optional; providers may ignore.
	TopP float64
}

// Chunk is one streaming unit, vendor-agnostic.
//
// V MUST be "chunk.v1" so consumers can version-detect.
type Chunk struct {
	V        string    `json:"v"`                  // always "chunk.v1"
	StreamID string    `json:"stream_id"`          // sub-task id
	NodeID   string    `json:"node_id,omitempty"`  // sub-task node id
	Model    string    `json:"model,omitempty"`    // model that produced this chunk
	Text     string    `json:"text"`               // token(s) to append
	Conf     float64   `json:"conf,omitempty"`     // 0..1, default 1.0
	Finish   bool      `json:"finish,omitempty"`   // true on the last chunk of a stream
	Cite     []SpanRef `json:"cite,omitempty"`     // provenance; Phase 5 fills
}

// SpanRef is one citation pointer attached to a chunk (or, in Phase 5, to a claim).
type SpanRef struct {
	SubTaskID  string `json:"sub_task_id"`
	Model      string `json:"model"`
	PromptHash string `json:"prompt_hash,omitempty"` // sha256 of the prompt that produced this
	DocID      string `json:"doc_id,omitempty"`      // RAG doc id (Phase 6)
	CharStart  int    `json:"char_start,omitempty"`  // char range in the source doc
	CharEnd    int    `json:"char_end,omitempty"`
}

// ChunkSink receives Chunks. The returned error cancels the upstream call.
type ChunkSink interface {
	Send(ctx context.Context, c Chunk) error
}

// Streamer is the vendor-agnostic contract every model adapter must satisfy.
type Streamer interface {
	// Stream consumes the request and pushes tokens to the sink until completion,
	// error, or context cancellation. Implementations MUST emit exactly one
	// Chunk with Finish: true at the end (success OR error path).
	Stream(ctx context.Context, req Request, sink ChunkSink) error

	// Name returns the provider key ("openai", "anthropic", "mock", ...).
	Name() string
}

// Registry returns Streamers by fully-qualified model id.
type Registry interface {
	// Get returns the Streamer registered under the prefix of model
	// (e.g. "openai" for "openai:gpt-4o-mini").
	Get(model string) (Streamer, error)

	// List returns the names of all registered Streamers.
	List() []string
}

// RegistryConfig controls which adapters are available. Nil keys disable
// the corresponding real provider; the mock adapter is always available.
type RegistryConfig struct {
	OpenAIKey    string
	AnthropicKey string
	MistralKey   string
	HFKey        string
	OllamaURL    string
	GroqKey      string
	HTTPTimeout  int // seconds; default 60
}

// FinishChunk is a small helper adapters use to emit the canonical ending chunk.
func FinishChunk(req Request, model string) Chunk {
	return Chunk{
		V:        "chunk.v1",
		StreamID: req.StreamID,
		NodeID:   req.NodeID,
		Model:    model,
		Finish:   true,
	}
}

// MarshalSSE serializes any value as an SSE `data:` field.
func MarshalSSE(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
