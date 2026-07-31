package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

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
