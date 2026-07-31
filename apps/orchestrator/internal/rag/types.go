// Package rag — Retrieval-Augmented Generation for CogniFlow.
//
// Phase 6 introduces:
//   - Chunker: sentence-aware fixed-window text splitter (800/200 by default).
//   - Store: append-only chunk storage with vector similarity search.
//   - Embedder: vendor-agnostic embedding model client (OpenAI default).
//   - Service: high-level upload + retrieve + inject-into-prompt helpers.
//
// The DAG executor (internal/dag) calls into Service.BuildInjectedSystemPrompt
// for any node with `NeedsRAG: true`. The resulting spans carry DocID +
// CharStart + CharEnd so Phase 5's CitationManifest can render per-citation
// hover cards pointing back at the source document and character range.
package rag

import (
	"context"
	"time"
)

// Chunk is a window of source text plus its embedding vector.
type Chunk struct {
	ID         string    // unique chunk id (UUIDv4 in production)
	DocID      string    // owning document id
	WorkspaceID string   // workspace the doc belongs to
	DocTitle   string    // convenience copy of doc title
	Text       string    // raw text of the chunk
	Embedding  []float32 // 1536 dims for text-embedding-3-small
	CharStart  int       // char offset in the source document
	CharEnd    int       // exclusive end offset
	CreatedAt  time.Time
}

// ScoredChunk pairs a Chunk with its retrieval score.
type ScoredChunk struct {
	Chunk
	Score float64 // cosine similarity in [0, 1]
}

// Document is the metadata record for an uploaded source.
type Document struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Title       string    `json:"title"`
	Source      string    `json:"source"`   // "upload" | "url" | "paste"
	MimeType    string    `json:"mime_type"` // "application/pdf" | "text/plain" | ...
	Size        int       `json:"size"`      // bytes of original file
	ChunkCount  int       `json:"chunk_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store is the persistence interface for documents + embeddings.
//
// PgStore is the production impl; MemStore is the in-proc test double that
// the orchestrator also falls back to when ORCH_DATABASE_URL is empty so the
// Phase 6 demo works without Docker.
type Store interface {
	UpsertChunks(ctx context.Context, chunks []Chunk) error
	Retrieve(ctx context.Context, workspaceID, query string, k int) ([]ScoredChunk, error)
	DeleteDoc(ctx context.Context, docID string) error
	ListDocs(ctx context.Context, workspaceID string) ([]Document, error)
	GetDoc(ctx context.Context, docID string) (Document, error)
}

// Embedder turns text into vectors. Implementations must return one []float32
// per input text, in the same order.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
	Model() string
}
