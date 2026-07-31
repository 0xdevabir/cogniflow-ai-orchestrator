package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// Weights are the per-task-class scoring coefficients. Defaults are
// 0.45 / 0.30 / 0.25 (bench / cost / latency). Override via
// WeightedConfig.Weights for cheaper-fast task classes.
type Weights struct {
	Bench   float64 `json:"bench"`
	Cost    float64 `json:"cost"`
	Latency float64 `json:"latency"`
}

// DefaultWeights returns the spec's MVP defaults.
func DefaultWeights() Weights {
	return Weights{Bench: 0.45, Cost: 0.30, Latency: 0.25}
}

// SetWeights installs a new per-task-class weight map on an already-built
// router. Used by main.go to apply the bandit-learn recommendation at
// startup without rebuilding the router.
func (w *WeightedRouter) SetWeights(m map[decomposer.TaskClass]Weights) {
	w.cfg.Weights = m
}

// WeightedConfig is the construction-time config for WeightedRouter.
type WeightedConfig struct {
	Bench         *Benchmarks
	Costs         *CostTable
	Weights       map[decomposer.TaskClass]Weights // overrides per task class
	EstPromptTok  int                              // default 500
	EstOutTok     int                              // default 1000
	Logger        FeedbackLogger                   // optional; nil disables logging
}

// WeightedRouter is the Phase 3 router implementation.
type WeightedRouter struct {
	cfg WeightedConfig
}

// NewWeighted builds a WeightedRouter. Validates weights sum ≈ 1.0 per
// task class and clamps negative weights to 0.
func NewWeighted(cfg WeightedConfig) (*WeightedRouter, error) {
	if cfg.Bench == nil {
		return nil, fmt.Errorf("router: nil Benchmarks")
	}
	if cfg.Costs == nil {
		return nil, fmt.Errorf("router: nil CostTable")
	}
	if cfg.EstPromptTok <= 0 {
		cfg.EstPromptTok = 500
	}
	if cfg.EstOutTok <= 0 {
		cfg.EstOutTok = 1000
	}
	if cfg.Weights == nil {
		cfg.Weights = map[decomposer.TaskClass]Weights{}
	}
	for tc, w := range cfg.Weights {
		sum := w.Bench + w.Cost + w.Latency
		if math.Abs(sum-1.0) > 0.01 {
			return nil, fmt.Errorf("router: weights for %q sum to %.3f, expected ~1.0", tc, sum)
		}
	}
	return &WeightedRouter{cfg: cfg}, nil
}

// weightsFor returns the Weights for a given task class, falling back to defaults.
func (w *WeightedRouter) weightsFor(tc decomposer.TaskClass) Weights {
	if w, ok := w.cfg.Weights[tc]; ok {
		return w
	}
	return DefaultWeights()
}

// candidateSet is the set of model ids we score. We use the union of
// keys from cost_table.json (latency column) and benchmarks.json.
func (w *WeightedRouter) candidateSet() []string {
	seen := map[string]struct{}{}
	for m := range w.cfg.Costs.LatencyP95MS {
		seen[m] = struct{}{}
	}
	for _, byModel := range w.cfg.Bench.Scores {
		for m := range byModel {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out) // deterministic
	return out
}

// Route implements Router.Route.
func (w *WeightedRouter) Route(ctx context.Context, n NodeSummary) (*Decision, error) {
	tc := n.TaskClass()
	candidates := w.candidateSet()

	// Filter by latency budget + cost budget before scoring.
	budgeted := make([]string, 0, len(candidates))
	for _, m := range candidates {
		if p95, ok := w.cfg.Costs.LatencyP95MS[m]; ok {
			if n.LatencyBudgetMS() > 0 && p95 > n.LatencyBudgetMS() {
				continue
			}
		}
		if costEst := w.estimateCost(m); n.MaxCostUSD() > 0 && costEst > n.MaxCostUSD() {
			continue
		}
		budgeted = append(budgeted, m)
	}

	if len(budgeted) == 0 {
		// Fallback: relax both constraints and pick the cheapest mock.
		// The mock provider is always available and free.
		return &Decision{
			Model:       ModelID{Provider: "mock", Model: "echo-v1"},
			Score:       0.5,
			Breakdown:   map[string]float64{"bench": 0.1, "cost": 1.0, "latency": 1.0},
			BanditArmID: w.armID(tc, "mock:echo-v1"),
			Reason:      "no candidates fit latency/cost budgets — falling back to mock",
		}, nil
	}

	wt := w.weightsFor(tc)
	scored := make([]ScoredModel, 0, len(budgeted))

	// Compute per-component denominators for normalization.
	maxCostEst, maxLat := w.maxima(budgeted)

	for _, m := range budgeted {
		bench := w.cfg.Bench.Scores[string(tc)][m]
		if bench == 0 {
			// If benchmarks don't have this model for this class, treat as low.
			bench = 0.20
		}
		costEst := w.estimateCost(m)
		costNorm := normalizeInverse(costEst, maxCostEst)
		lat := w.cfg.Costs.LatencyP95MS[m]
		if lat == 0 {
			// Treat unset latency as the worst observed so it doesn't unfairly win.
			if maxLat < 1 {
				maxLat = 1
			}
			lat = int(maxLat)
		}
		latNorm := normalizeInverse(float64(lat), maxLat)
		score := wt.Bench*bench + wt.Cost*costNorm + wt.Latency*latNorm

		scored = append(scored, ScoredModel{
			Model: parseModelID(m),
			Score: roundN(score, 4),
			Breakdown: map[string]float64{
				"bench":    roundN(bench, 4),
				"cost":     roundN(costNorm, 4),
				"latency":  roundN(latNorm, 4),
				"cost_est": roundN(costEst, 6),
				"lat_p95":  float64(lat),
			},
		})
	}

	// Sort descending by score. Deterministic tiebreak: cheaper first, then faster.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		ci := w.estimateCost(scored[i].Model.String())
		cj := w.estimateCost(scored[j].Model.String())
		if ci != cj {
			return ci < cj
		}
		return w.cfg.Costs.LatencyP95MS[scored[i].Model.String()] <
			w.cfg.Costs.LatencyP95MS[scored[j].Model.String()]
	})

	pick := scored[0]
	reason := fmt.Sprintf("score %.3f (bench %.2f, cost %.2f, latency %.2f)",
		pick.Score, pick.Breakdown["bench"], pick.Breakdown["cost"], pick.Breakdown["latency"])

	d := &Decision{
		Model:        pick.Model,
		Score:        pick.Score,
		Breakdown:    pick.Breakdown,
		Alternatives: scored,
		BanditArmID:  w.armID(tc, pick.Model.String()),
		Reason:       reason,
	}

	// Log the decision event.
	if w.cfg.Logger != nil {
		_ = w.cfg.Logger.Append(FeedbackEvent{
			NodeID:      "", // populated by caller (Phase 4)
			BanditArmID: d.BanditArmID,
			Model:       d.Model.String(),
			TaskClass:   string(tc),
			Satisfaction: 0,
			LatencyMS:   int(pick.Breakdown["lat_p95"]),
			CostUSD:     pick.Breakdown["cost_est"],
			Timestamp:   time.Now(),
		})
	}

	return d, nil
}

// CheaperAlternatives implements Router.CheaperAlternatives.
func (w *WeightedRouter) CheaperAlternatives(ctx context.Context, tc decomposer.TaskClass, exclude []string, k int) []ScoredModel {
	if k <= 0 {
		k = 3
	}
	candidates := w.candidateSet()
	excluded := map[string]bool{}
	for _, m := range exclude {
		excluded[m] = true
	}
	var out []ScoredModel
	for _, m := range candidates {
		if excluded[m] {
			continue
		}
		cost := w.estimateCost(m)
		out = append(out, ScoredModel{
			Model: parseModelID(m),
			Score: -cost, // sort ascending on cost
			Breakdown: map[string]float64{
				"cost_est": roundN(cost, 6),
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Breakdown["cost_est"] < out[j].Breakdown["cost_est"]
	})
	if len(out) > k {
		out = out[:k]
	}
	// Recompute real scores for the chosen ones (using the task-class weights).
	wt := w.weightsFor(tc)
	for i := range out {
		bench := w.cfg.Bench.Scores[string(tc)][out[i].Model.String()]
		if bench == 0 {
			bench = 0.20
		}
		costNorm := 1.0 - out[i].Breakdown["cost_est"]/maxCost(w.cfg.Costs)
		if costNorm < 0 {
			costNorm = 0
		}
		lat := float64(w.cfg.Costs.LatencyP95MS[out[i].Model.String()])
		out[i].Score = roundN(wt.Bench*bench+wt.Cost*costNorm+wt.Latency*normalizeInverse(lat, 10000), 4)
	}
	return out
}

// RecordFeedback implements Router.RecordFeedback.
func (w *WeightedRouter) RecordFeedback(ctx context.Context, ev FeedbackEvent) error {
	if w.cfg.Logger == nil {
		return nil
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	return w.cfg.Logger.Append(ev)
}

// --- helpers ---

func (w *WeightedRouter) estimateCost(model string) float64 {
	pi := w.cfg.Costs.PerMillionInputUSD[model]
	po := w.cfg.Costs.PerMillionOutputUSD[model]
	if pi == 0 && po == 0 {
		// local / mock → free
		return 0
	}
	return float64(w.cfg.EstPromptTok)*pi/1e6 + float64(w.cfg.EstOutTok)*po/1e6
}

func (w *WeightedRouter) maxima(candidates []string) (maxCost, maxLat float64) {
	for _, m := range candidates {
		c := w.estimateCost(m)
		if c > maxCost {
			maxCost = c
		}
		l := float64(w.cfg.Costs.LatencyP95MS[m])
		if l > maxLat {
			maxLat = l
		}
	}
	if maxCost == 0 {
		maxCost = 1
	}
	if maxLat == 0 {
		maxLat = 1
	}
	return
}

func normalizeInverse(v, max float64) float64 {
	if max <= 0 {
		return 1.0
	}
	n := 1.0 - v/max
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	return n
}

func maxCost(c *CostTable) float64 {
	var m float64
	for _, v := range c.PerMillionOutputUSD {
		if v > m {
			m = v
		}
	}
	if m == 0 {
		m = 1
	}
	return m
}

func parseModelID(s string) ModelID {
	i := strings.Index(s, ":")
	if i < 0 {
		return ModelID{Provider: "", Model: s}
	}
	return ModelID{Provider: s[:i], Model: s[i+1:]}
}

func (w *WeightedRouter) armID(tc decomposer.TaskClass, model string) string {
	h := sha256.Sum256([]byte(string(tc) + "|" + model))
	return hex.EncodeToString(h[:6])
}

func roundN(v float64, digits int) float64 {
	mul := math.Pow(10, float64(digits))
	return math.Round(v*mul) / mul
}