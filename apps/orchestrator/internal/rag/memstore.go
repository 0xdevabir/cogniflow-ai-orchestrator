package rag

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// MemStore is an in-process Store. It is the dev/test fallback when
// ORCH_DATABASE_URL is empty so the playground works without Docker.
type MemStore struct {
	mu       sync.RWMutex
	chunks   map[string]Chunk // by chunk id
	docs     map[string]Document
	byDoc    map[string][]string // doc id → chunk ids
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{
		chunks: map[string]Chunk{},
		docs:   map[string]Document{},
		byDoc:  map[string][]string{},
	}
}

// UpsertDoc inserts or replaces a document metadata record.
func (m *MemStore) UpsertDoc(ctx context.Context, doc Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}
	m.docs[doc.ID] = doc
	return nil
}

func (m *MemStore) UpsertChunks(ctx context.Context, chunks []Chunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range chunks {
		c2 := c
		if c2.CreatedAt.IsZero() {
			c2.CreatedAt = time.Now().UTC()
		}
		m.chunks[c2.ID] = c2
		m.byDoc[c2.DocID] = append(m.byDoc[c2.DocID], c2.ID)
		doc := m.docs[c2.DocID]
		doc.ChunkCount = len(m.byDoc[c2.DocID])
		m.docs[c2.DocID] = doc
	}
	return nil
}

func (m *MemStore) Retrieve(ctx context.Context, workspaceID, query string, k int) ([]ScoredChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if k <= 0 {
		k = 6
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScoredChunk, 0, k)
	for _, c := range m.chunks {
		if c.WorkspaceID != workspaceID {
			continue
		}
		score := lexicalScore(query, c.Text)
		if score == 0 {
			continue
		}
		out = append(out, ScoredChunk{Chunk: c, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (m *MemStore) DeleteDoc(ctx context.Context, docID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cid := range m.byDoc[docID] {
		delete(m.chunks, cid)
	}
	delete(m.byDoc, docID)
	delete(m.docs, docID)
	return nil
}

func (m *MemStore) ListDocs(ctx context.Context, workspaceID string) ([]Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Document, 0, len(m.docs))
	for _, d := range m.docs {
		if d.WorkspaceID != workspaceID {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemStore) GetDoc(ctx context.Context, docID string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.docs[docID]
	if !ok {
		return Document{}, fmt.Errorf("rag: doc %q not found", docID)
	}
	return d, nil
}

// lexicalScore is a tiny token-overlap score used by the MemStore fallback
// when an embedder isn't available (so the playground demo still works).
// Phase 8 will swap MemStore for PgStore + real embeddings behind the same
// interface.
func lexicalScore(query, text string) float64 {
	q := tokenize(query)
	t := tokenize(text)
	if len(q) == 0 || len(t) == 0 {
		return 0
	}
	tokens := make(map[string]int, len(t))
	for _, w := range t {
		tokens[w]++
	}
	hits := 0
	for _, w := range q {
		if tokens[w] > 0 {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	// Cosine-ish: token overlap normalised by sqrt(|q|*|t|).
	return float64(hits) / math.Sqrt(float64(len(q)*len(t)))
}

func tokenize(s string) []string {
	var out []string
	cur := make([]byte, 0, 16)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			cur = append(cur, c+32)
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			cur = append(cur, c)
		default:
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = cur[:0]
			}
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// cosineSim returns cosine similarity between two float32 vectors. Returns 0
// if lengths differ (caller should treat as no match).
func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// hashVector returns a deterministic hash of a vector for stable IDs in tests.
func hashVector(v []float32) uint64 {
	h := sha256.New()
	for _, x := range v {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(x))
		h.Write(b[:])
	}
	bs := h.Sum(nil)
	return binary.LittleEndian.Uint64(bs[:8])
}
