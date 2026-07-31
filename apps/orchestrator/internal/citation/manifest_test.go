package citation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestManifest_VersionAndEmpty(t *testing.T) {
	m := New()
	if m.V != ManifestVersion {
		t.Errorf("v = %q, want %q", m.V, ManifestVersion)
	}
	if len(m.Spans) != 0 {
		t.Errorf("expected empty spans, got %d", len(m.Spans))
	}
}

func TestManifest_AddReturnsIDs(t *testing.T) {
	m := New()
	id1 := m.Add(Span{SubTaskID: "n1", Model: "mock:echo-v1", Text: "first"})
	id2 := m.Add(Span{SubTaskID: "n2", Model: "mock:echo-v1", Text: "second"})
	id3 := m.Add(Span{SubTaskID: "n3", Model: "mock:echo-v1", Text: "third"})
	if id1 == "" || id2 == "" || id3 == "" {
		t.Fatalf("expected non-empty IDs, got %q %q %q", id1, id2, id3)
	}
	if id1 == id2 || id2 == id3 || id1 == id3 {
		t.Errorf("IDs not unique: %q %q %q", id1, id2, id3)
	}
	if !strings.HasPrefix(id1, "sp_") {
		t.Errorf("id %q missing sp_ prefix", id1)
	}
	if len(m.Spans) != 3 {
		t.Errorf("spans = %d, want 3", len(m.Spans))
	}
}

func TestManifest_AddHonorsCallerID(t *testing.T) {
	m := New()
	id := m.Add(Span{ID: "sp_custom", SubTaskID: "n1", Model: "x", Text: "y"})
	if id != "sp_custom" {
		t.Errorf("id = %q, want sp_custom", id)
	}
}

func TestManifest_Lookup(t *testing.T) {
	m := New()
	id := m.Add(Span{SubTaskID: "n1", Model: "m", Text: "alpha"})
	got, ok := m.Lookup(id)
	if !ok {
		t.Fatalf("Lookup(%q) not found", id)
	}
	if got.Text != "alpha" {
		t.Errorf("text = %q, want alpha", got.Text)
	}
	if _, ok := m.Lookup("sp_doesnotexist"); ok {
		t.Errorf("expected miss for nonexistent id")
	}
}

func TestManifest_BySubTask(t *testing.T) {
	m := New()
	m.Add(Span{SubTaskID: "n1", Model: "m", Text: "a"})
	m.Add(Span{SubTaskID: "n2", Model: "m", Text: "b"})
	m.Add(Span{SubTaskID: "n1", Model: "m", Text: "c"})
	got := m.BySubTask("n1")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Text != "a" || got[1].Text != "c" {
		t.Errorf("order = %q %q", got[0].Text, got[1].Text)
	}
}

func TestManifest_AddSerializes(t *testing.T) {
	m := New()
	m.Add(Span{SubTaskID: "n1", Model: "openai:gpt-4o-mini", Text: "claim 1"})
	m.Add(Span{SubTaskID: "n2", Model: "anthropic:claude-3-5-sonnet-latest", Text: "claim 2"})
	m.Add(Span{SubTaskID: "n3", Model: "openai:gpt-4o", Text: "claim 3"})

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"v":"citation.v1"`) {
		t.Errorf("missing v field: %s", s)
	}
	if !strings.Contains(s, `"sub_task_id":"n1"`) {
		t.Errorf("missing n1: %s", s)
	}
	if !strings.Contains(s, `"claim 3"`) {
		t.Errorf("missing claim 3: %s", s)
	}

	// Round-trip
	var m2 Manifest
	if err := json.Unmarshal(b, &m2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(m.Spans, m2.Spans) {
		t.Errorf("round-trip mismatch")
	}
}

func TestHashPrompt_Stable(t *testing.T) {
	a := HashPrompt("hello world")
	b := HashPrompt("hello world")
	if a != b {
		t.Errorf("same prompt produced different hashes: %q vs %q", a, b)
	}
	if len(a) != 12 {
		t.Errorf("hash length = %d, want 12", len(a))
	}
	if HashPrompt("a") == HashPrompt("b") {
		t.Errorf("different prompts produced same hash")
	}
}

func TestHashText_Stable(t *testing.T) {
	if HashText("hi") == HashText("bye") {
		t.Errorf("different texts produced same hash")
	}
}

func TestNewSpanID_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewSpanID()
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
