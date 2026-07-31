// Package budget enforces cost + carbon caps per conversation.
//
// The Estimator projects the (cost_usd, carbon_g) of a Plan using the same
// cost table the router uses. If the projection exceeds a user-supplied
// Budget, PlanDowngrade cascades each expensive node through progressively
// cheaper alternatives (Opus → Sonnet → Mixtral → mock) until the plan fits
// or we run out of candidates.
//
// Carbon is approximated as (input_tokens + output_tokens) * carbonPerMTok
// in the CostTable. Phase 8 replaces this with model-specific EcoLogits /
// LLM-CarbonFootprint lookups.
package budget

import (
	"fmt"
	"math"
	"sort"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/router"
)

// Budget is the per-request spending cap.
type Budget struct {
	MaxCostUSD float64 `json:"max_cost_usd"`
	MaxCarbonG float64 `json:"max_carbon_g"`
}

// Projection is a (cost, carbon) estimate for one node or a whole plan.
type Projection struct {
	CostUSD float64 `json:"cost_usd"`
	CarbonG float64 `json:"carbon_g"`
}

// Add returns the sum of two projections.
func (p Projection) Add(o Projection) Projection {
	return Projection{CostUSD: p.CostUSD + o.CostUSD, CarbonG: p.CarbonG + o.CarbonG}
}

// Fits returns true if the projection is within the budget (or the budget
// for that axis is 0 = unlimited).
func (p Projection) Fits(b Budget) bool {
	if b.MaxCostUSD > 0 && p.CostUSD > b.MaxCostUSD {
		return false
	}
	if b.MaxCarbonG > 0 && p.CarbonG > b.MaxCarbonG {
		return false
	}
	return true
}

// Estimator projects (cost, carbon) for nodes given a CostTable.
type Estimator struct {
	costs        *router.CostTable
	promptTokens int
	outputTokens int
}

// New builds an Estimator. promptTokens and outputTokens are the per-node
// estimates used to project a node's cost/carbon. Defaults: 500 / 1000.
func New(c *router.CostTable, promptTokens, outputTokens int) *Estimator {
	if promptTokens <= 0 {
		promptTokens = 500
	}
	if outputTokens <= 0 {
		outputTokens = 1000
	}
	return &Estimator{costs: c, promptTokens: promptTokens, outputTokens: outputTokens}
}

// PerNode returns the projected (cost, carbon) for a single model.
func (e *Estimator) PerNode(model string) Projection {
	if e == nil || e.costs == nil {
		return Projection{}
	}
	pi := e.costs.PerMillionInputUSD[model]
	po := e.costs.PerMillionOutputUSD[model]
	cg := e.costs.CarbonGPerMTokens[model]
	cost := float64(e.promptTokens)*pi/1e6 + float64(e.outputTokens)*po/1e6
	carbon := float64(e.promptTokens+e.outputTokens) * cg / 1e6
	return Projection{CostUSD: round4(cost), CarbonG: round4(carbon)}
}

// Total sums per-node projections.
func (e *Estimator) Total(items []Projection) Projection {
	out := Projection{}
	for _, p := range items {
		out.CostUSD += p.CostUSD
		out.CarbonG += p.CarbonG
	}
	return Projection{CostUSD: round4(out.CostUSD), CarbonG: round4(out.CarbonG)}
}

// ProjectionOf is a convenience: given the current model assigned to each
// node, total the projection for the whole plan.
func (e *Estimator) ProjectionOf(assign map[string]string) Projection {
	ids := make(map[string]struct{}, len(assign))
	for _, m := range assign {
		ids[m] = struct{}{}
	}
	// Per-node cost (assume each assignment is one invocation).
	var projs []Projection
	for _, m := range assign {
		projs = append(projs, e.PerNode(m))
	}
	return e.Total(projs)
}

// DowngradeResult describes the cascade outcome.
type DowngradeResult struct {
	Original     map[string]string `json:"original"`     // node_id → old model
	New          map[string]string `json:"new"`          // node_id → new model
	SavedUSD     float64           `json:"saved_usd"`
	SavedG       float64           `json:"saved_g"`
	FinalCost    float64           `json:"final_cost_usd"`
	FinalCarbon  float64           `json:"final_carbon_g"`
	Downgraded   int               `json:"downgraded"`    // # of nodes changed
	Unachievable bool              `json:"unachievable"`  // true if no cascade could fit the budget
}

// PlanDowngrade cascades each node from its current model to progressively
// cheaper alternatives until the total projection fits the budget. The
// cascade is per-node but coordinated globally: at every step we re-project
// the whole plan and break out as soon as it fits.
//
// r is the router; we use its CheaperAlternatives to pick the next
// candidate per node. Excluded models are tracked per node.
func (e *Estimator) PlanDowngrade(
	current map[string]string,
	b Budget,
	r router.Router,
) DowngradeResult {
	res := DowngradeResult{
		Original: cloneMap(current),
		New:      cloneMap(current),
	}
	startProj := e.ProjectionOf(current)
	res.FinalCost = startProj.CostUSD
	res.FinalCarbon = startProj.CarbonG

	if startProj.Fits(b) {
		// Already fits — nothing to do.
		return res
	}
	if r == nil {
		res.Unachievable = true
		return res
	}

	// For each node, track the set of already-tried model ids.
	tried := map[string]map[string]bool{}
	for nid := range current {
		tried[nid] = map[string]bool{current[nid]: true}
	}

	// Iterate at most len(candidateSet) times so we terminate even with cycles.
	for iter := 0; iter < 16; iter++ {
		changed := false
		for nid := range res.New {
			tc := decomposer.ClassReasoning // default for budget purposes
			alts := r.CheaperAlternatives(nil, tc, sortedKeys(tried[nid]), 1)
			if len(alts) == 0 {
				continue
			}
			candidate := alts[0].Model.String()
			if candidate == res.New[nid] {
				continue
			}
			res.New[nid] = candidate
			tried[nid][candidate] = true
			changed = true
			res.Downgraded++
		}
		proj := e.ProjectionOf(res.New)
		res.FinalCost = proj.CostUSD
		res.FinalCarbon = proj.CarbonG
		if proj.Fits(b) {
			break
		}
		if !changed {
			// Nothing more to try.
			break
		}
	}

	finalProj := Projection{CostUSD: res.FinalCost, CarbonG: res.FinalCarbon}
	if !finalProj.Fits(b) {
		res.Unachievable = true
	}
	res.SavedUSD = round4(math.Max(0, startProj.CostUSD-res.FinalCost))
	res.SavedG = round4(math.Max(0, startProj.CarbonG-res.FinalCarbon))
	return res
}

// --- helpers ---

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func round4(v float64) float64 {
	return math.Round(v*1e4) / 1e4
}

// FormatProjection renders a Projection for an SSE event payload.
func FormatProjection(p Projection) string {
	return fmt.Sprintf("cost=%.4f carbon=%.4f", p.CostUSD, p.CarbonG)
}
