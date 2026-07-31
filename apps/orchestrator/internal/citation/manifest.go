// Package citation models the provenance graph for every claim.
//
// Every Chunk carries SpanRef slices. The CitationManifest is versioned and
// threaded through the DAG. At the end, an SSE manifest event ships the
// full graph to the web app for inline rendering.
package citation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ManifestVersion is the JSON-schema version of the manifest wire format.
const ManifestVersion = "citation.v1"

// Manifest is an immutable, append-only collection of Spans. Spans are
// keyed by ID ("sp_NNN"); the Build helpers mint IDs deterministically.
type Manifest struct {
	V     string `json:"v"`
	Spans []Span `json:"spans"`
}

// Span is one citation pointer. It captures:
//   - WHO produced the claim (model + sub_task_id)
//   - WHAT the verbatim claim text is
//   - WHERE it lives in the world (optional RAG doc + char range)
type Span struct {
	ID         string `json:"id"`
	SubTaskID  string `json:"sub_task_id"`
	Model      string `json:"model"`
	Text       string `json:"text"`
	DocID      string `json:"doc_id,omitempty"`
	DocSnippet string `json:"doc_snippet,omitempty"`
	PromptHash string `json:"prompt_hash,omitempty"`
	CharStart  int    `json:"char_start,omitempty"`
	CharEnd    int    `json:"char_end,omitempty"`
}

// New returns an empty manifest stamped with the current version.
func New() *Manifest {
	return &Manifest{V: ManifestVersion}
}

// Add appends a span, returning the assigned ID. The caller may override
// the ID by setting s.ID; otherwise one is generated.
func (m *Manifest) Add(s Span) string {
	if s.ID == "" {
		s.ID = NewSpanID()
	}
	m.Spans = append(m.Spans, s)
	return s.ID
}

// Lookup returns the span with the given ID, or zero-value + false.
func (m *Manifest) Lookup(id string) (Span, bool) {
	for _, s := range m.Spans {
		if s.ID == id {
			return s, true
		}
	}
	return Span{}, false
}

// BySubTask returns all spans produced by a given sub-task, in the order
// they were added.
func (m *Manifest) BySubTask(id string) []Span {
	var out []Span
	for _, s := range m.Spans {
		if s.SubTaskID == id {
			out = append(out, s)
		}
	}
	return out
}

// HashPrompt sha256s the prompt for provenance. Returned as 12-char hex.
func HashPrompt(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(h[:6])
}

// HashText sha256s a string for stable IDs. Returned as 12-char hex.
func HashText(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

// NewSpanID returns a fresh span ID of the form "sp_<hash>". The hash is
// derived from "sp|<unix-nano>|<counter>" so consecutive IDs are unique
// even within the same nanosecond.
func NewSpanID() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("sp|%d|%d", nowNano(), nextSpanCounter())))
	return "sp_" + hex.EncodeToString(h[:4])
}
