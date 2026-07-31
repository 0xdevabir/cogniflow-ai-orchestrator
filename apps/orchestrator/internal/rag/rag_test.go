package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// EmbedderFunc adapts a function to the Embedder interface for tests.
type EmbedderFunc struct {
	DimSize  int
	ModelStr string
	Fn       func(ctx context.Context, texts []string) ([][]float32, error)
}

func (e EmbedderFunc) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.Fn(ctx, texts)
}

func (e EmbedderFunc) Dim() int    { return e.DimSize }
func (e EmbedderFunc) Model() string { return e.ModelStr }

// chunkText — small helper to make a chunk with stable id.
func TestSplitText_Defaults(t *testing.T) {
	in := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50)
	chunks := SplitText(in, ChunkOpts{})
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Text == "" {
			t.Fatalf("chunk %d empty", i)
		}
		if c.CharStart < 0 || c.CharEnd > len(in) || c.CharStart > c.CharEnd {
			t.Fatalf("chunk %d has invalid range %d..%d (len=%d)", i, c.CharStart, c.CharEnd, len(in))
		}
		if c.ID == "" {
			t.Fatalf("chunk %d missing id", i)
		}
	}
}

func TestSplitText_PreservesLineBreaks(t *testing.T) {
	in := "A sentence. Another sentence. A third.\n\nA paragraph break, and another sentence. And one more."
	chunks := SplitText(in, ChunkOpts{MaxChars: 30, OverlapChars: 5, MinChunkChars: 5})
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Each chunk's text should be a substring of the original.
	for i, c := range chunks {
		if in[c.CharStart:c.CharEnd] != c.Text {
			t.Fatalf("chunk %d text != source slice %q vs %q", i, c.Text, in[c.CharStart:c.CharEnd])
		}
	}
}

func TestSplitText_TinyInputDropped(t *testing.T) {
	if SplitText("too short", ChunkOpts{MinChunkChars: 100}) != nil {
		t.Fatal("expected tiny input to be dropped")
	}
}

func TestSplitText_CustomOpts(t *testing.T) {
	in := strings.Repeat("a b c d. ", 200)
	chunks := SplitText(in, ChunkOpts{MaxChars: 200, OverlapChars: 50, MinChunkChars: 50})
	if len(chunks) < 3 {
		t.Fatalf("expected >=3 chunks, got %d", len(chunks))
	}
}

func TestMemStore_UpsertAndRetrieve(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	doc := Document{ID: "doc_1", WorkspaceID: "w1", Title: "T1"}
	_ = s.UpsertDoc(ctx, doc)
	chunks := []Chunk{
		{ID: "c1", DocID: "doc_1", WorkspaceID: "w1", Text: "the terminating party shall provide 30 days notice"},
		{ID: "c2", DocID: "doc_1", WorkspaceID: "w1", Text: "the termination clause is in section 7 of the contract"},
		{ID: "c3", DocID: "doc_1", WorkspaceID: "w1", Text: "confidentiality obligations survive for five years"},
	}
	if err := s.UpsertChunks(ctx, chunks); err != nil {
		t.Fatal(err)
	}
	got, err := s.Retrieve(ctx, "w1", "termination notice", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].ID != "c1" {
		t.Fatalf("expected c1 first (highest overlap), got %s", got[0].ID)
	}
}

func TestMemStore_WorkspaceIsolation(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	_ = s.UpsertDoc(ctx, Document{ID: "d1", WorkspaceID: "ws:A"})
	_ = s.UpsertDoc(ctx, Document{ID: "d2", WorkspaceID: "ws:B"})
	_ = s.UpsertChunks(ctx, []Chunk{
		{ID: "x1", DocID: "d1", WorkspaceID: "ws:A", Text: "alpha bravo charlie"},
		{ID: "x2", DocID: "d2", WorkspaceID: "ws:B", Text: "alpha bravo charlie"},
	})
	got, _ := s.Retrieve(ctx, "ws:A", "alpha", 10)
	for _, c := range got {
		if c.WorkspaceID != "ws:A" {
			t.Fatalf("leak: %s in ws:A results", c.WorkspaceID)
		}
	}
}

func TestMemStore_DeleteDocCascadesChunks(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	_ = s.UpsertDoc(ctx, Document{ID: "d1", WorkspaceID: "w1"})
	_ = s.UpsertChunks(ctx, []Chunk{
		{ID: "c1", DocID: "d1", WorkspaceID: "w1", Text: "hello"},
		{ID: "c2", DocID: "d1", WorkspaceID: "w1", Text: "world"},
	})
	if err := s.DeleteDoc(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Retrieve(ctx, "w1", "hello", 5)
	if len(got) != 0 {
		t.Fatalf("expected 0 chunks after delete, got %d", len(got))
	}
}

func TestService_UploadThenRetrieve(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	emb := EmbedderFunc{
		DimSize:  4,
		ModelStr: "stub",
		Fn: func(_ context.Context, texts []string) ([][]float32, error) {
			out := make([][]float32, len(texts))
			for i, tx := range texts {
				v := make([]float32, 4)
				for j := 0; j < 4; j++ {
					v[j] = float32(len(tx) + j + i)
				}
				out[i] = v
			}
			return out, nil
		},
	}
	svc := NewService(s, emb)

	doc := Document{ID: "doc_x", WorkspaceID: "ws1", Title: "NDA"}
	res, err := svc.Upload(ctx, doc, strings.Repeat("The terminating party shall provide 30 days written notice. ", 20))
	if err != nil {
		t.Fatal(err)
	}
	if res.ChunkCount == 0 {
		t.Fatal("expected chunks")
	}
	injected, sections, err := svc.BuildInjectedSystemPrompt(ctx, "ws1", "termination notice")
	if err != nil {
		t.Fatal(err)
	}
	if injected == "" {
		t.Fatal("empty injected prompt")
	}
	if !strings.Contains(injected, "===DOC 1") {
		t.Fatalf("missing DOC marker: %s", injected)
	}
	if len(sections) == 0 {
		t.Fatal("expected sections")
	}
	if sections[0].DocID != "doc_x" {
		t.Fatalf("expected doc id in section, got %s", sections[0].DocID)
	}
}

func TestService_NoResultsReturnsErr(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	svc := NewService(s, nil)
	if _, _, err := svc.BuildInjectedSystemPrompt(ctx, "ws", "anything"); !errors.Is(err, ErrNoResults) {
		t.Fatalf("expected ErrNoResults, got %v", err)
	}
}

func TestService_EmbedderErrorBubbles(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	emb := EmbedderFunc{
		DimSize: 2, ModelStr: "boom",
		Fn: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, errors.New("rate limited")
		},
	}
	svc := NewService(s, emb)
	doc := Document{ID: "d1", WorkspaceID: "ws", Title: "T"}
	_, err := svc.Upload(ctx, doc, strings.Repeat("The terminating party shall provide thirty days written notice of termination. ", 20))
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected embed error, got %v", err)
	}
}

func TestLexicalScore_NonNegative(t *testing.T) {
	if s := lexicalScore("xyz", "abc"); s != 0 {
		t.Fatalf("expected 0 for disjoint, got %f", s)
	}
	if s := lexicalScore("alpha", "alpha beta gamma"); s <= 0 {
		t.Fatalf("expected positive score, got %f", s)
	}
}

func TestCosineSim_Identical(t *testing.T) {
	v := []float32{1, 0, 0}
	if s := cosineSim(v, v); s <= 0.99 {
		t.Fatalf("expected ~1, got %f", s)
	}
}

func TestCosineSim_Orthogonal(t *testing.T) {
	if s := cosineSim([]float32{1, 0, 0}, []float32{0, 1, 0}); s != 0 {
		t.Fatalf("expected 0, got %f", s)
	}
}

func TestCosineSim_LenMismatch(t *testing.T) {
	if s := cosineSim([]float32{1, 0}, []float32{1, 0, 0}); s != 0 {
		t.Fatalf("expected 0 for mismatch, got %f", s)
	}
}

func TestSplitText_PreservesCharRangesAreValid(t *testing.T) {
	in := strings.Repeat("a b c d e f g h i j k l m n o p. ", 60)
	chunks := SplitText(in, ChunkOpts{MaxChars: 150, OverlapChars: 50, MinChunkChars: 30})
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		// Every chunk text must equal the source slice for its range.
		if in[c.CharStart:c.CharEnd] != c.Text {
			t.Fatalf("chunk %d inconsistent: text=%q source[%d:%d]=%q", i, c.Text, c.CharStart, c.CharEnd, in[c.CharStart:c.CharEnd])
		}
	}
}
