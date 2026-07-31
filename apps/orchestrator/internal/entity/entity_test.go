package entity

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	upserted map[string][]Entity
}

func newFake() *fakeStore { return &fakeStore{upserted: map[string][]Entity{}} }

func (f *fakeStore) Upsert(_ context.Context, docID string, es []Entity) error {
	if docID == "" {
		return errors.New("missing doc id")
	}
	f.upserted[docID] = append(f.upserted[docID], es...)
	return nil
}

func (f *fakeStore) Query(_ context.Context, name string) ([]Entity, error) {
	var out []Entity
	for _, list := range f.upserted {
		for _, e := range list {
			if e.Name == name {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func TestNoopStore_NoPanic(t *testing.T) {
	var s Store = NoopStore{}
	if err := s.Upsert(context.Background(), "d1", []Entity{{Name: "X"}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Query(context.Background(), "X"); err != nil || len(got) != 0 {
		t.Fatalf("expected nil/0, got %v / %d", err, len(got))
	}
}

func TestFakeStore_RoundTrip(t *testing.T) {
	s := newFake()
	ctx := context.Background()
	if err := s.Upsert(ctx, "doc1", []Entity{{Name: "Apple", Type: "org"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Query(ctx, "Apple")
	if len(got) != 1 || got[0].Type != "org" {
		t.Fatalf("expected one org entity, got %+v", got)
	}
}
