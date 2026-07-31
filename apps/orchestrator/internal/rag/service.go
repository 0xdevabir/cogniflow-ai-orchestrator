package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoResults is returned when retrieval produced no chunks for the query.
var ErrNoResults = errors.New("rag: no results")

// Service is the high-level RAG layer used by the API and the DAG executor.
//
//   - Upload: extract text → chunk → embed → upsert into the Store.
//   - BuildInjectedSystemPrompt: retrieve top-k for a query, format as
//     ===DOC n | doc_id | "title"=== blocks for the synthesizer.
type Service struct {
	Store    Store
	Embedder Embedder
	Chunker  ChunkOpts
	TopK     int
}

// NewService builds a Service with default options.
func NewService(store Store, embedder Embedder) *Service {
	return &Service{
		Store:    store,
		Embedder: embedder,
		Chunker:  ChunkOpts{},
		TopK:     6,
	}
}

// UploadResult is what the upload API returns to the caller.
type UploadResult struct {
	DocID      string `json:"doc_id"`
	Title      string `json:"title"`
	ChunkCount int    `json:"chunk_count"`
}

// Upload chunks text, embeds each chunk, and persists it under the given
// workspace. The caller supplies the Document record (id, mime, source, etc).
func (s *Service) Upload(ctx context.Context, doc Document, text string) (UploadResult, error) {
	if s.Store == nil {
		return UploadResult{}, errors.New("rag: no store configured")
	}
	if strings.TrimSpace(text) == "" {
		return UploadResult{}, errors.New("rag: empty document text")
	}

	chunks := SplitText(text, s.Chunker)
	if len(chunks) == 0 {
		return UploadResult{}, errors.New("rag: document too small to chunk")
	}

	// Stamp chunk metadata from the parent document.
	for i := range chunks {
		chunks[i].DocID = doc.ID
		chunks[i].WorkspaceID = doc.WorkspaceID
		chunks[i].DocTitle = doc.Title
	}

	if s.Embedder != nil {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}
		vecs, err := s.Embedder.Embed(ctx, texts)
		if err != nil {
			return UploadResult{}, fmt.Errorf("rag: embed: %w", err)
		}
		if len(vecs) != len(chunks) {
			return UploadResult{}, fmt.Errorf("rag: embedder returned %d vectors for %d chunks", len(vecs), len(chunks))
		}
		for i := range chunks {
			chunks[i].Embedding = vecs[i]
		}
	}

	if err := s.Store.UpsertChunks(ctx, chunks); err != nil {
		return UploadResult{}, fmt.Errorf("rag: upsert: %w", err)
	}
	doc.ChunkCount = len(chunks)
	// Persist the doc metadata record. We piggyback on UpsertChunks by adding
	// a single empty chunk if the store doesn't track docs separately — but
	// MemStore/PgStore both have explicit doc APIs. Use a small wrapper to
	// avoid coupling Service to a concrete store.
	if meta, ok := s.Store.(docMetaWriter); ok {
		_ = meta.UpsertDoc(ctx, doc)
	}
	return UploadResult{DocID: doc.ID, Title: doc.Title, ChunkCount: len(chunks)}, nil
}

// docMetaWriter is an optional interface on Store for explicit doc upserts.
type docMetaWriter interface {
	UpsertDoc(ctx context.Context, doc Document) error
}

// InjectedSection describes one retrieved chunk that will appear in the
// downstream prompt, returned alongside the rendered prompt so the caller can
// stamp citation metadata onto the resulting spans.
type InjectedSection struct {
	DocID     string `json:"doc_id"`
	DocTitle  string `json:"doc_title"`
	Text      string `json:"text"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
	Score     float64 `json:"score"`
}

// BuildInjectedSystemPrompt retrieves top-k chunks for the query and renders
// them as ===DOC n | doc_id | "title"=== blocks inside a system message.
//
// The returned []InjectedSection is in the same order as the rendered DOC
// markers — callers use it to attach DocID + char range to outgoing Spans.
func (s *Service) BuildInjectedSystemPrompt(ctx context.Context, workspaceID, query string) (string, []InjectedSection, error) {
	if s.Store == nil {
		return "", nil, errors.New("rag: no store configured")
	}
	k := s.TopK
	if k <= 0 {
		k = 6
	}
	scored, err := s.Store.Retrieve(ctx, workspaceID, query, k)
	if err != nil {
		return "", nil, fmt.Errorf("rag: retrieve: %w", err)
	}
	if len(scored) == 0 {
		return "", nil, ErrNoResults
	}

	var b strings.Builder
	b.WriteString("The following documents were retrieved to help you answer. Use them and cite them with the bracketed DOC numbers in your response.\n\n")
	sections := make([]InjectedSection, 0, len(scored))
	for i, c := range scored {
		idx := i + 1
		title := c.DocTitle
		if title == "" {
			title = c.DocID
		}
		fmt.Fprintf(&b, "===DOC %d | %s | %q===\n%s\n\n", idx, c.DocID, title, c.Text)
		sections = append(sections, InjectedSection{
			DocID:     c.DocID,
			DocTitle:  title,
			Text:      c.Text,
			CharStart: c.CharStart,
			CharEnd:   c.CharEnd,
			Score:     c.Score,
		})
	}
	b.WriteString("===TASK===\n")
	b.WriteString(query)
	b.WriteString("\n")
	return b.String(), sections, nil
}