package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/rag"
	"github.com/cogniflow/orchestrator/internal/router"
)

// --- Phase 6: helpers for the RAG-injected /v1/run test ---

type chunkForTest = rag.Chunk

func newRAGStoreForTest(t *testing.T) *rag.MemStore {
	t.Helper()
	return rag.NewMemStore()
}

func docForTest(id, ws, title string) rag.Document {
	return rag.Document{ID: id, WorkspaceID: ws, Title: title}
}

func ragServiceFromStore(s *rag.MemStore) *rag.Service {
	return rag.NewService(s, nil)
}

// ensure unused-import lint doesn't trip on the phase-2 helpers.
var _ = context.Background

func makeServerForRun(t *testing.T) *Server {
	t.Helper()
	reg := providers.NewRegistry(nil)
	return &Server{Registry: reg}
}

func TestHandleRun_RejectsBadJSON(t *testing.T) {
	srv := makeServerForRun(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader("{not json"))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
}

func TestHandleRun_RejectsWrongMethod(t *testing.T) {
	srv := makeServerForRun(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/run", nil)
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rw.Code)
	}
}

func TestHandleRun_RejectsMissingInput(t *testing.T) {
	srv := makeServerForRun(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/run",
		strings.NewReader(`{}`))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

func TestHandleRun_RejectsInvalidPlan(t *testing.T) {
	srv := makeServerForRun(t)
	body := `{"plan":{"version":"plan.v1","nodes":[{"id":"A","role":"r","payload":"a","depends_on":["Z"]}],"edges":[]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(body))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "unknown dependency") {
		t.Errorf("expected 'unknown dependency' in body, got %s", rw.Body.String())
	}
}

func TestHandleRun_TemporalUnavailable(t *testing.T) {
	srv := makeServerForRun(t)
	plan := minimalPlan()
	body, _ := json.Marshal(map[string]any{
		"plan":          plan,
		"executor_mode": "temporal",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	if !strings.Contains(rw.Body.String(), "temporal_unavailable") {
		t.Errorf("expected temporal_unavailable code, got: %s", rw.Body.String())
	}
}

func TestHandleRun_RunsPlanAndStreamsSSE(t *testing.T) {
	srv := makeServerForRun(t)
	plan := minimalPlan()
	body, _ := json.Marshal(map[string]any{"plan": plan})

	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if ct := rw.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}

	// Parse SSE events.
	events := parseSSE(t, rw.Body.String())
	if len(events) == 0 {
		t.Fatal("no SSE events emitted")
	}

	// We expect at minimum: a "plan" event, two "node_status: running" events,
	// some "chunk" events, two "node_status: ok" events, and a "done".
	var sawPlan, sawDone bool
	statuses := map[string]string{}
	chunks := 0
	for _, ev := range events {
		switch ev.Event {
		case "plan":
			sawPlan = true
		case "node_status":
			var m map[string]any
			if err := json.Unmarshal([]byte(ev.Data), &m); err != nil {
				t.Errorf("bad node_status json: %v", err)
				continue
			}
			nodeID, _ := m["node_id"].(string)
			status, _ := m["status"].(string)
			if nodeID != "" && status != "" {
				statuses[nodeID+":"+status] = ev.Data
			}
		case "chunk":
			chunks++
		case "done":
			sawDone = true
		}
	}
	if !sawPlan {
		t.Error("no plan event emitted")
	}
	if !sawDone {
		t.Error("no done event emitted")
	}
	if chunks == 0 {
		t.Error("no chunks emitted")
	}
	if _, ok := statuses["n1:running"]; !ok {
		t.Errorf("missing n1:running; got %v", statuses)
	}
	if _, ok := statuses["n1:ok"]; !ok {
		t.Errorf("missing n1:ok; got %v", statuses)
	}
	if _, ok := statuses["n2:running"]; !ok {
		t.Errorf("missing n2:running; got %v", statuses)
	}
	if _, ok := statuses["n2:ok"]; !ok {
		t.Errorf("missing n2:ok; got %v", statuses)
	}
}

func TestHandleRun_NoFlusherReturns500(t *testing.T) {
	srv := makeServerForRun(t)
	plan := minimalPlan()
	body, _ := json.Marshal(map[string]any{"plan": plan})
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	// httptest.NewRecorder() does implement Flusher via ResponseRecorder, but
	// we can simulate non-flusher with a custom writer.
	rw := &nonFlusher{header: http.Header{}}
	srv.HandleRun(rw, req)
	if rw.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rw.status)
	}
}

// --- helpers ---

type sseEvent struct {
	Event string
	Data  string
}

// parseSSE reads an SSE-formatted body and returns one event per chunk.
// Format: "event: foo\ndata: bar\n\n"
func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var cur sseEvent
	flush := func() {
		if cur.Event != "" || cur.Data != "" {
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event: "):
			cur.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = strings.TrimPrefix(line, "data: ")
		}
	}
	flush()
	return out
}

func minimalPlan() *decomposer.Plan {
	return &decomposer.Plan{
		Version: "plan.v1",
		Nodes: []decomposer.Node{
			{
				ID:      "n1",
				Role:    decomposer.RoleResearcher,
				Payload: "find info",
				Requires: decomposer.Requirements{
					TaskClass: decomposer.ClassFactual, Modality: decomposer.ModalityText,
					LatencyBudgetMS: 5000, MaxCostUSD: 0.05,
				},
			},
			{
				ID:        "n2",
				Role:      decomposer.RoleSynthesizer,
				Payload:   "summarize",
				DependsOn: []string{"n1"},
				Requires: decomposer.Requirements{
					TaskClass: decomposer.ClassReasoning, Modality: decomposer.ModalityText,
					LatencyBudgetMS: 5000, MaxCostUSD: 0.05,
				},
			},
		},
		Edges: []decomposer.Edge{{From: "n1", To: "n2"}},
	}
}

// nonFlusher is a ResponseWriter that does NOT implement http.Flusher.
// Used to assert the no_flusher error path.
type nonFlusher struct {
	header http.Header
	status int
	body   strings.Builder
}

func (n *nonFlusher) Header() http.Header        { return n.header }
func (n *nonFlusher) Write(b []byte) (int, error) { return n.body.Write(b) }
func (n *nonFlusher) WriteHeader(s int)          { n.status = s }

// Ensure unused import isn't flagged.
var _ = time.Second

// --- Phase 7: budget + eval end-to-end ---

// expensiveRouter always picks an expensive model first so the cascade has
// something to downgrade. CheaperAlternatives returns a stair-step ladder.
type expensiveRouter struct {
	initial string
	ladders map[string][]string
}

func splitModelID(s string) router.ModelID {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return router.ModelID{Provider: s[:i], Model: s[i+1:]}
		}
	}
	return router.ModelID{Provider: "", Model: s}
}

func (e *expensiveRouter) Route(ctx context.Context, n router.NodeSummary) (*router.Decision, error) {
	return &router.Decision{
		Model:  splitModelID(e.initial),
		Score:  0.5,
		Reason: "stub: expensive initial pick",
	}, nil
}

func (e *expensiveRouter) CheaperAlternatives(ctx context.Context, tc decomposer.TaskClass, exclude []string, k int) []router.ScoredModel {
	alts := e.ladders[e.initial]
	out := make([]router.ScoredModel, 0, len(alts))
	for _, a := range alts {
		skip := false
		for _, x := range exclude {
			if a == x {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out = append(out, router.ScoredModel{Model: splitModelID(a), Score: 0.5})
		if len(out) >= k {
			break
		}
	}
	return out
}

func (e *expensiveRouter) RecordFeedback(ctx context.Context, ev router.FeedbackEvent) error {
	return nil
}

func TestHandleRun_BudgetTriggersDowngrade(t *testing.T) {
	srv := makeServerForRun(t)
	// Seed cost table so the estimator has prices.
	costs, err := router.LoadCostTable()
	if err != nil {
		t.Fatal(err)
	}
	srv.CostTable = costs
	// Force the initial pick to be expensive; cascade should downgrade to mock.
	srv.Router = &expensiveRouter{
		initial: "anthropic:claude-3-opus-latest",
		ladders: map[string][]string{
			"anthropic:claude-3-opus-latest": {
				"anthropic:claude-3-5-sonnet-latest",
				"openai:gpt-4o-mini",
				"mock:echo-v1",
			},
		},
	}

	body, _ := json.Marshal(map[string]any{
		"plan": map[string]any{
			"version": "plan.v1",
			"nodes": []map[string]any{
				{"id": "n1", "role": "researcher", "payload": "summarize x",
					"requires": map[string]any{"task_class": "factual"}},
			},
			"edges": []any{},
		},
		"budget": map[string]any{"max_cost_usd": 0.0001},
		"eval":  false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	// Parse the SSE stream and look for a downgrade event whose data
	// reports the cascade swapped n1 off opus.
	scanner := bufio.NewScanner(strings.NewReader(rw.Body.String()))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	var sawDowngrade bool
	var downgradeData map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: downgrade"):
			sawDowngrade = true
		case sawDowngrade && strings.HasPrefix(line, "data: ") && downgradeData == nil:
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &downgradeData); err != nil {
				t.Fatalf("bad downgrade json: %v", err)
			}
		}
	}
	if !sawDowngrade {
		t.Fatalf("expected a downgrade event in the SSE stream; body=%s", rw.Body.String())
	}
	if downgradeData == nil {
		t.Fatal("downgrade event had no data payload")
	}
	if downgradeData["downgraded"].(float64) < 1 {
		t.Errorf("expected downgraded >= 1, got %v", downgradeData["downgraded"])
	}
	original, _ := downgradeData["original"].(map[string]any)
	newMap, _ := downgradeData["new"].(map[string]any)
	if original["n1"] != "anthropic:claude-3-opus-latest" {
		t.Errorf("expected original n1 to be opus, got %v", original["n1"])
	}
	if got := newMap["n1"]; got == "anthropic:claude-3-opus-latest" {
		t.Errorf("expected cascade to swap n1 off opus, but stayed at %v", got)
	}
}

func TestHandleRun_EvalDisabled(t *testing.T) {
	srv := makeServerForRun(t)
	costs, _ := router.LoadCostTable()
	srv.CostTable = costs
	f := false
	body, _ := json.Marshal(map[string]any{
		"plan": map[string]any{
			"version": "plan.v1",
			"nodes": []map[string]any{
				{"id": "n1", "role": "researcher", "payload": "x"},
			},
			"edges": []any{},
		},
		"eval": &f,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), "event: eval") {
		t.Error("expected no eval event when eval=false")
	}
}

// --- Phase 6: RAG integration into /v1/run ---

func TestHandleRun_NeedsRAGInjectsDocs(t *testing.T) {
	srv := makeServerForRun(t)
	// Seed RAG.
	store := newRAGStoreForTest(t)
	_ = store.UpsertDoc(context.Background(), docForTest("doc_rag", "ws1", "NDA"))
	_ = store.UpsertChunks(context.Background(), []chunkForTest{
		{ID: "c1", DocID: "doc_rag", WorkspaceID: "ws1",
			Text: "the terminating party shall provide thirty days written notice"},
	})
	srv.RAG = ragServiceFromStore(store)

	body, _ := json.Marshal(map[string]any{
		"plan": map[string]any{
			"version": "plan.v1",
			"nodes": []map[string]any{
				{"id": "n1", "role": "factchecker", "payload": "what is the termination clause?",
					"needs_rag": true, "requires": map[string]any{"task_class": "factual"}},
			},
			"edges": []any{},
		},
		"workspace": "ws1",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader(string(body)))
	rw := httptest.NewRecorder()
	srv.HandleRun(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	// Parse SSE events and look for a manifest event that mentions doc_rag.
	scanner := bufio.NewScanner(strings.NewReader(rw.Body.String()))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	sawManifest := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			continue
		}
		if v, ok := payload["spans"]; ok {
			sawManifest = true
			raw, _ := json.Marshal(v)
			if !strings.Contains(string(raw), "doc_rag") {
				t.Fatalf("expected manifest to reference doc_rag, got %s", raw)
			}
		}
	}
	if !sawManifest {
		t.Fatal("no manifest event in run stream")
	}
}
