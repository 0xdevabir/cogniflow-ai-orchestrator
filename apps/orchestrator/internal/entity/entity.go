// Package entity — Phase 8 stub for the entity graph layer (Neo4j).
//
// Phase 6 declares the interface and ships a NoopStore so callers can wire it
// without taking a hard dependency on a graph database. Phase 8 swaps in a
// Neo4j-backed implementation; the interface and call-sites stay the same.
package entity

import "context"

// Entity is a named thing (person, org, contract clause, etc.) extracted
// from a document.
type Entity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"` // "person" | "org" | "place" | "clause" | ...
	DocID string `json:"doc_id,omitempty"`
}

// Store is the persistence interface for the entity graph.
type Store interface {
	Upsert(ctx context.Context, docID string, entities []Entity) error
	Query(ctx context.Context, name string) ([]Entity, error)
}

// NoopStore discards every entity. Safe for the Phase 6 default when Neo4j
// isn't configured.
type NoopStore struct{}

// Upsert drops the entities on the floor.
func (NoopStore) Upsert(_ context.Context, _ string, _ []Entity) error { return nil }

// Query returns nothing.
func (NoopStore) Query(_ context.Context, _ string) ([]Entity, error) { return nil, nil }
