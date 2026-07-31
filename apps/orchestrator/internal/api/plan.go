package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// RoutedNode is a per-node routing decision the UI renders as a badge.
type RoutedNode struct {
	NodeID    string             `json:"node_id"`
	Model     string             `json:"model"`
	Score     float64            `json:"score"`
	Breakdown map[string]float64 `json:"breakdown"`
	Reason    string             `json:"reason"`
}

// planRequest is the JSON body the web app POSTs to /v1/plan.
type planRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"` // optional override of decomposer model
}

// planResponse is what /v1/plan returns. We include a passthrough flag so
// the UI can badge "fallback: single-node passthrough" when retries failed.
// `Routed` is populated when a Router is wired up; each node gets its
// per-task-class model pick.
type planResponse struct {
	Plan        *decomposer.Plan `json:"plan"`
	Passthrough bool             `json:"passthrough"`
	Model       string           `json:"model"`
	PromptEcho  string           `json:"prompt_echo"`
	Routed      []RoutedNode     `json:"routed,omitempty"`
}

// HandlePlan is the Plan debug endpoint.
//
//   POST /v1/plan
//   body: {"prompt":"...", "model":""}
//   → 200 application/json
//   {
//     "plan": { ... Plan ... },
//     "passthrough": false,
//     "model": "openai:gpt-4o",
//     "prompt_echo": "...",
//     "routed": [{"node_id":"n1","model":"...","score":0.86,"breakdown":{...},"reason":"..."}]
//   }
//
// On error: 400 with `{"error":"..."}`.
func (s *Server) HandlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Decomposer == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "decomposer not configured", "no_decomposer")
		return
	}

	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "bad_json")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required", "missing_prompt")
		return
	}

	// If the request specifies a model, swap it on the decomposer for
	// this call ONLY (we don't mutate server state).
	decomp := s.Decomposer
	model := ""
	if req.Model != "" {
		// Build a shallow copy with the new model.
		d := decomp.Deps()
		d.Model = req.Model
		newD := decomposer.New(d)
		decomp = newD
		model = req.Model
	} else {
		model = s.Decomposer.Model()
	}

	plan, err := decomp.Decompose(r.Context(), req.Prompt)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "decompose_failed")
		return
	}

	resp := planResponse{
		Plan:        plan,
		Passthrough: isPassthrough(plan),
		Model:       model,
		PromptEcho:  req.Prompt,
	}

	// If a router is wired, score every node and attach a Routed entry.
	if s.Router != nil && plan != nil {
		for _, n := range plan.Nodes {
			summary := &nodeSummary{node: n}
			d, rerr := s.Router.Route(r.Context(), summary)
			if rerr != nil || d == nil {
				continue
			}
			resp.Routed = append(resp.Routed, RoutedNode{
				NodeID:    n.ID,
				Model:     d.Model.String(),
				Score:     d.Score,
				Breakdown: d.Breakdown,
				Reason:    d.Reason,
			})
		}
	}

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// isPassthrough returns true if the plan is a single-node synthesizer.
// Used to flag the UI when the LLM failed to produce a real plan.
func isPassthrough(p *decomposer.Plan) bool {
	if p == nil {
		return true
	}
	if len(p.Nodes) != 1 {
		return false
	}
	return p.Nodes[0].Role == decomposer.RoleSynthesizer && len(p.Edges) == 0
}

// nodeSummary wraps a decomposer.Node to satisfy router.NodeSummary.
// We read the latency_budget_ms / max_cost_usd out of Requirements; if
// absent we leave them at zero (= "no budget constraint").
type nodeSummary struct {
	node decomposer.Node
}

func (n *nodeSummary) TaskClass() decomposer.TaskClass { return n.node.Requires.TaskClass }
func (n *nodeSummary) LatencyBudgetMS() int            { return n.node.Requires.LatencyBudgetMS }
func (n *nodeSummary) MaxCostUSD() float64             { return n.node.Requires.MaxCostUSD }