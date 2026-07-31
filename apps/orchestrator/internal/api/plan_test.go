package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

// makeServer builds a Server with a real decomposer that talks to the real
// mock provider (since we have no LLM key in tests). The mock returns one
// of 4 deterministic text blocks; the decomposer will try to parse each
// as JSON. For the plan tests, we only assert API-layer behavior (shape,
// status codes, error handling) — the inner JSON shape is tested in the
// decomposer package.
func makeServer(t *testing.T) *Server {
	t.Helper()
	reg := providers.NewRegistry(nil) // no keys → mock fallback
	decomp := decomposer.New(decomposer.Deps{
		Registry:  reg,
		Model:     "mock:echo-v1",
		Retries:   2,
		MaxTokens: 512,
	})
	return &Server{Registry: reg, Decomposer: decomp}
}

// TestHandlePlan_RejectsBadJSON: 400 on garbage body.
func TestHandlePlan_RejectsBadJSON(t *testing.T) {
	srv := makeServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader("{not json"))
	rw := httptest.NewRecorder()
	srv.HandlePlan(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
}

// TestHandlePlan_RejectsMissingPrompt: 400 on empty prompt.
func TestHandlePlan_RejectsMissingPrompt(t *testing.T) {
	srv := makeServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader(`{"prompt":""}`))
	rw := httptest.NewRecorder()
	srv.HandlePlan(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
}

// TestHandlePlan_RejectsWrongMethod: 405 on GET.
func TestHandlePlan_RejectsWrongMethod(t *testing.T) {
	srv := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/plan", nil)
	rw := httptest.NewRecorder()
	srv.HandlePlan(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rw.Code)
	}
}

// TestHandlePlan_NoDecomposerConfigured: returns 503.
func TestHandlePlan_NoDecomposerConfigured(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil), Decomposer: nil}
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader(`{"prompt":"hi"}`))
	rw := httptest.NewRecorder()
	srv.HandlePlan(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rw.Code)
	}
}

// TestHandlePlan_RunsWithMock: even with no API keys, the endpoint completes.
// With the mock provider the LLM "output" is gibberish, so the decomposer
// will fall back to passthrough. We just assert the response is well-formed.
func TestHandlePlan_RunsWithMock(t *testing.T) {
	srv := makeServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader(`{"prompt":"plan a 3-day trip to Tokyo"}`))
	rw := httptest.NewRecorder()
	srv.HandlePlan(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if ct := rw.Header().Get("content-type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var resp planResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rw.Body.String())
	}
	if resp.Plan == nil {
		t.Fatal("Plan is nil")
	}
	if resp.Plan.Version != "plan.v1" {
		t.Errorf("version = %q", resp.Plan.Version)
	}
	// With mock, expect passthrough OR a real plan (if we ever wire a smart
	// mock). At minimum the response shape must hold.
	if len(resp.Plan.Nodes) == 0 {
		t.Error("expected at least 1 node in plan")
	}
}

// TestPassthrough_Shape: isPassthrough() recognizes the synthetic 1-node
// synthesizer plan.
func TestIsPassthrough_True(t *testing.T) {
	p := decomposer.PassthroughPlan("hi")
	if !isPassthrough(p) {
		t.Error("expected isPassthrough(passthrough) = true")
	}
}

func TestIsPassthrough_False(t *testing.T) {
	p := &decomposer.Plan{
		Version: "plan.v1",
		Nodes: []decomposer.Node{
			{ID: "n1", Role: decomposer.RoleResearcher, Payload: "x", DependsOn: []string{},
				Requires: decomposer.Requirements{TaskClass: decomposer.ClassFactual, Modality: decomposer.ModalityText, LatencyBudgetMS: 1000, MaxCostUSD: 0.1}},
			{ID: "n2", Role: decomposer.RoleSynthesizer, Payload: "y", DependsOn: []string{"n1"},
				Requires: decomposer.Requirements{TaskClass: decomposer.ClassReasoning, Modality: decomposer.ModalityText, LatencyBudgetMS: 1000, MaxCostUSD: 0.1}},
		},
		Edges: []decomposer.Edge{{From: "n1", To: "n2"}},
	}
	if isPassthrough(p) {
		t.Error("expected isPassthrough(2-node plan) = false")
	}
}