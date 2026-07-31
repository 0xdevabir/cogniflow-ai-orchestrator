package router

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cogniflow/orchestrator/internal/banditlearn"
	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// RecommendationLoader applies a banditlearn recommendation to a router's
// per-task-class weights. Each class's bench weight is boosted by the
// recommendation's RecommendedBenchBoost (clamped to keep the sum ~1.0).
//
// The boost is intentionally small: the bandit log captures *what the
// router chose*, not ground-truth quality, so an aggressive override would
// just lock the system in to past biases. A small boost nudges the router
// toward recent winners without erasing the cost / latency signals.
func ApplyRecommendation(rec *banditlearn.Recommendation, base map[decomposer.TaskClass]Weights) (map[decomposer.TaskClass]Weights, error) {
	if rec == nil {
		return base, nil
	}
	out := map[decomposer.TaskClass]Weights{}
	for k, v := range base {
		out[k] = v
	}
	for _, c := range rec.Classes {
		if c.Winner == "" {
			continue
		}
		tc := decomposer.TaskClass(c.TaskClass)
		current := out[tc]
		if (current == Weights{}) {
			current = DefaultWeights()
		}
		// Boost bench by up to 0.20 (so the existing 0.45 becomes ≤0.65),
		// funded by reducing cost + latency proportionally.
		boost := c.RecommendedBenchBoost
		if boost > 0.20 {
			boost = 0.20
		}
		newBench := current.Bench + boost
		// Reduce cost + latency by the same total amount, scaled by current.
		removal := boost
		denom := current.Cost + current.Latency
		var newCost, newLat float64
		if denom > 0 {
			newCost = current.Cost - removal*current.Cost/denom
			newLat = current.Latency - removal*current.Latency/denom
		} else {
			// Edge: cost + latency both zero. Skip the override.
			continue
		}
		if newCost < 0 {
			newCost = 0
		}
		if newLat < 0 {
			newLat = 0
		}
		out[tc] = Weights{
			Bench:   roundN(newBench, 4),
			Cost:    roundN(newCost, 4),
			Latency: roundN(newLat, 4),
		}
	}
	return out, nil
}

// LoadRecommendation is a convenience: read a recommendation JSON file
// from disk and apply it to base weights.
func LoadRecommendation(path string, base map[decomposer.TaskClass]Weights) (map[decomposer.TaskClass]Weights, error) {
	if path == "" {
		return base, nil
	}
	rec, err := loadRecFromJSON(path)
	if err != nil {
		return base, fmt.Errorf("router: load recommendation: %w", err)
	}
	return ApplyRecommendation(rec, base)
}

// loadRecFromJSON decodes a recommendation file. A missing file is treated
// as "no recommendation" (returns nil, nil) so the router can be deployed
// without a recommendation.json present. A malformed file IS an error.
func loadRecFromJSON(path string) (*banditlearn.Recommendation, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec banditlearn.Recommendation
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}
