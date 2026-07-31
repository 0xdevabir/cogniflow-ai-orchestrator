package meter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoopMeterer(t *testing.T) {
	m := NoopMeterer{}
	m.Record(Event{V: "usage.v1"})
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultReturnsNoop(t *testing.T) {
	if _, ok := Default().(NoopMeterer); !ok {
		t.Fatalf("Default should return NoopMeterer, got %T", Default())
	}
}

func TestJSONLMeterer_AppendsAndFlushes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "usage.jsonl")
	m, err := NewJSONLMeterer(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	now := time.Now().UTC().Truncate(time.Second)
	m.Record(Event{V: "usage.v1", RunID: "r1", NodeID: "n1", Model: "mock:echo-v1", TokensIn: 10, TokensOut: 5, CostUSD: 0.001, OccurredAt: now})
	m.Record(Event{V: "usage.v1", RunID: "r1", NodeID: "n2", Model: "openai:gpt-4o-mini", TokensIn: 100, TokensOut: 50, CostUSD: 0.04, OccurredAt: now})
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(raw))
	}
	var e1 Event
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatalf("bad line 0: %v", err)
	}
	if e1.RunID != "r1" || e1.TokensIn != 10 {
		t.Errorf("first event mismatch: %+v", e1)
	}
}

func TestStripeMeterer_BatchesAndFlushes(t *testing.T) {
	var captured stripeBatchPayload
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m, err := NewStripeMeterer(StripeConfig{
		APIKey:        "sk_test_abc",
		MeterID:       "mtr_xyz",
		BaseURL:       srv.URL,
		BufferSize:    100,
		FlushInterval: time.Hour, // disable the auto-flush for this test
	})
	if err != nil {
		t.Fatal(err)
	}

	m.Record(Event{V: "usage.v1", Workspace: "ws1", TokensIn: 5, TokensOut: 3, OccurredAt: time.Unix(100, 0)})
	m.Record(Event{V: "usage.v1", Workspace: "ws2", TokensIn: 50, TokensOut: 30, OccurredAt: time.Unix(101, 0)})
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", calls)
	}
	if captured.MeterID != "mtr_xyz" {
		t.Errorf("meter_id = %q", captured.MeterID)
	}
	if len(captured.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(captured.Events))
	}
	if captured.Events[0].Quantity != 8 || captured.Events[1].Quantity != 80 {
		t.Errorf("quantities wrong: %+v", captured.Events)
	}
	if captured.Events[0].Identifier.CustomerID != "ws1" {
		t.Errorf("first customer_id = %q", captured.Events[0].Identifier.CustomerID)
	}
}

func TestStripeMeterer_ReBuffersOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, err := NewStripeMeterer(StripeConfig{
		APIKey:        "sk_test_abc",
		MeterID:       "mtr_xyz",
		BaseURL:       srv.URL,
		BufferSize:    100,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Record(Event{V: "usage.v1", Workspace: "ws1", TokensIn: 5, TokensOut: 3})
	if err := m.Flush(); err == nil {
		t.Fatal("expected error on 500")
	}
	// After the failure, the buffer should still hold the event for retry.
	m.mu.Lock()
	if len(m.buffer) != 1 {
		t.Errorf("expected re-buffer, got %d events", len(m.buffer))
	}
	m.mu.Unlock()
}

func TestStripeMetererFromEnv_NoKeyReturnsNoop(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")
	t.Setenv("STRIPE_METER_ID", "")
	if _, ok := StripeMetererFromEnv().(NoopMeterer); !ok {
		t.Fatal("expected NoopMeterer when env empty")
	}
}

func TestStripeMetererFromEnv_BadConfigReturnsNoop(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_x")
	t.Setenv("STRIPE_METER_ID", "")
	m := StripeMetererFromEnv()
	if _, ok := m.(NoopMeterer); !ok {
		t.Fatalf("expected NoopMeterer when meter id missing, got %T", m)
	}
}

func TestNewStripeMeterer_MissingKeyFails(t *testing.T) {
	_, err := NewStripeMeterer(StripeConfig{MeterID: "m"})
	if err == nil {
		t.Fatal("expected error on missing API key")
	}
	_, err = NewStripeMeterer(StripeConfig{APIKey: "k"})
	if err == nil {
		t.Fatal("expected error on missing meter id")
	}
}

func TestJSONLMeterer_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	m, err := NewJSONLMeterer(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Path() != path {
		t.Errorf("Path() = %q, want %q", m.Path(), path)
	}
}

func TestJSONLMeterer_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	m1, err := NewJSONLMeterer(path)
	if err != nil {
		t.Fatal(err)
	}
	m1.Record(Event{V: "usage.v1", RunID: "r1"})
	if err := m1.Close(); err != nil {
		t.Fatal(err)
	}
	m2, err := NewJSONLMeterer(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	m2.Record(Event{V: "usage.v1", RunID: "r2"})
	if err := m2.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 2 {
		t.Errorf("expected 2 lines after reopen, got %q", raw)
	}
}