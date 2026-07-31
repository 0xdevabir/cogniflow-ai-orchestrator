package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// These three adapters are STUBS in Phase 1. They conform to the Streamer
// interface so the registry surface is identical and Phase 6+ can drop in
// real implementations behind the same constructors without changing any
// caller code. Each stub emits a single chunk explaining that it's a stub.
//
// Real implementation notes (Phase 6+):
//
//   - Mistral: use the official mistral-go SDK or raw HTTP — endpoint
//     https://api.mistral.ai/v1/chat/completions, OpenAI-compatible wire
//     format. SSE chunks identical to OpenAI.
//
//   - HuggingFace Inference API: HTTPS POST to
//     https://api-inference.huggingface.co/models/<id> with bearer key.
//     Response is *not* SSE — wrap as one chunk for now or upgrade to SSE
//     when the streaming endpoint is configured.
//
//   - Ollama: local HTTP at OLLAMA_BASE_URL (default http://localhost:11434).
//     POST /api/chat with {model, messages, stream:true}. Response is NDJSON
//     lines, each {message: {content: "..."}, done: bool}.

func newMistralStub(key string) Streamer { return &mistralStub{key: key} }
func newHFStub(key string) Streamer      { return &hfStub{key: key} }
func newOllamaStub(url string) Streamer  { return &ollamaStub{url: url} }

type mistralStub struct{ key string }

func (m *mistralStub) Name() string { return "mistral" }
func (m *mistralStub) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	_, modelID := ParseModel(req.Model)
	if modelID == "" {
		modelID = "mistral-small-latest"
	}
	_ = sink.Send(ctx, Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: "mistral:" + modelID,
		Text:  fmt.Sprintf("[mistral stub] would call Mistral for: %q", truncate(req.Prompt, 60)),
	})
	return sink.Send(ctx, FinishChunk(req, "mistral:"+modelID))
}

type hfStub struct{ key string }

func (h *hfStub) Name() string { return "hf" }
func (h *hfStub) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	_, modelID := ParseModel(req.Model)
	if modelID == "" {
		modelID = "meta-llama/Meta-Llama-3-8B-Instruct"
	}
	_ = sink.Send(ctx, Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: "hf:" + modelID,
		Text:  fmt.Sprintf("[hf stub] would call HF Inference for: %q", truncate(req.Prompt, 60)),
	})
	return sink.Send(ctx, FinishChunk(req, "hf:"+modelID))
}

type ollamaStub struct{ url string }

func (o *ollamaStub) Name() string { return "ollama" }
func (o *ollamaStub) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	_, modelID := ParseModel(req.Model)
	if modelID == "" {
		modelID = "llama3.1"
	}
	_ = sink.Send(ctx, Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: "ollama:" + modelID,
		Text:  fmt.Sprintf("[ollama stub @ %s] would stream from local model for: %q", o.url, truncate(req.Prompt, 60)),
	})
	return sink.Send(ctx, FinishChunk(req, "ollama:"+modelID))
}

// errStubOnly is exported so tests can verify the stub-only path.
var errStubOnly = errors.New("adapter is a Phase 1 stub; real implementation lands in Phase 6+")

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
