package router

import (
	_ "embed"
	"encoding/json"
)

//go:embed data/benchmarks.json
var benchmarksJSON []byte

//go:embed data/cost_table.json
var costTableJSON []byte

// Benchmarks is the per-(task_class, model) quality matrix.
type Benchmarks struct {
	Scores map[string]map[string]float64 `json:"scores"` // task_class -> model_id -> 0..1
}

// CostTable is the per-model cost + latency + carbon matrix.
type CostTable struct {
	PerMillionInputUSD    map[string]float64 `json:"per_million_input_usd"`
	PerMillionOutputUSD   map[string]float64 `json:"per_million_output_usd"`
	LatencyP95MS          map[string]int     `json:"latency_p95_ms"`
	CarbonGPerMTokens     map[string]float64 `json:"carbon_g_per_million_tokens"`
}

// LoadBenchmarks parses the embedded benchmarks.json. Errors are fatal —
// the file is part of the binary.
func LoadBenchmarks() (*Benchmarks, error) {
	var b Benchmarks
	if err := json.Unmarshal(benchmarksJSON, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// LoadCostTable parses the embedded cost_table.json.
func LoadCostTable() (*CostTable, error) {
	var c CostTable
	if err := json.Unmarshal(costTableJSON, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Models returns the union of all model ids across both tables. The
// WeightedRouter uses this as the candidate set when no other source
// of candidates is available.
func (b *Benchmarks) Models() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, byModel := range b.Scores {
		for m := range byModel {
			seen[m] = struct{}{}
		}
	}
	for m := range seen {
		out = append(out, m)
	}
	return out
}