package budget

import (
	"context"
	"testing"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/router"
)

func mustRouter(t *testing.T) (router.Router, *router.CostTable) {
	t.Helper()
	costs, err := router.LoadCostTable()
	if err != nil {
		t.Fatal(err)
	}
	bench, err := router.LoadBenchmarks()
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewWeighted(router.WeightedConfig{
		Bench:        bench,
		Costs:        costs,
		EstPromptTok: 500,
		EstOutTok:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, costs
}

func TestEstimator_PerNode_KnownModel(t *testing.T) {
	_, costs := mustRouter(t)
	e := New(costs, 1_000_000, 0) // 1M input tokens, 0 output
	p := e.PerNode("openai:gpt-4o")
	// per_million_input_usd["openai:gpt-4o"] == 2.50
	if p.CostUSD < 2.49 || p.CostUSD > 2.51 {
		t.Fatalf("expected ~2.50 cost for 1M input gpt-4o, got %f", p.CostUSD)
	}
}

func TestEstimator_PerNode_UnknownModelIsZero(t *testing.T) {
	_, costs := mustRouter(t)
	e := New(costs, 500, 1000)
	if p := e.PerNode("totally-fake:model"); p.CostUSD != 0 || p.CarbonG != 0 {
		t.Fatalf("unknown model should be free, got %+v", p)
	}
}

func TestEstimator_Total(t *testing.T) {
	_, costs := mustRouter(t)
	e := New(costs, 500, 1000)
	projs := []Projection{
		{CostUSD: 0.10, CarbonG: 0.5},
		{CostUSD: 0.20, CarbonG: 1.5},
	}
	if got := e.Total(projs); got.CostUSD != 0.30 || got.CarbonG != 2.0 {
		t.Fatalf("got %+v", got)
	}
}

func TestProjection_Fits(t *testing.T) {
	p := Projection{CostUSD: 0.05, CarbonG: 0.2}
	if !p.Fits(Budget{MaxCostUSD: 0.10, MaxCarbonG: 0.5}) {
		t.Fatal("should fit")
	}
	if p.Fits(Budget{MaxCostUSD: 0.01}) {
		t.Fatal("cost over")
	}
	if p.Fits(Budget{MaxCarbonG: 0.1}) {
		t.Fatal("carbon over")
	}
	if !p.Fits(Budget{}) {
		t.Fatal("zero budget = unlimited")
	}
}

func TestPlanDowngrade_Fits(t *testing.T) {
	r, costs := mustRouter(t)
	e := New(costs, 500, 1000)
	current := map[string]string{
		"n1": "anthropic:claude-3-opus-latest", // ~$90/M out
		"n2": "anthropic:claude-3-opus-latest",
	}
	res := e.PlanDowngrade(current, Budget{MaxCostUSD: 0.05}, r)
	if res.Unachievable {
		t.Fatal("expected to find a fitting cascade")
	}
	if res.FinalCost > 0.05 {
		t.Fatalf("expected final cost <=0.05, got %f", res.FinalCost)
	}
	if res.New["n1"] == "anthropic:claude-3-opus-latest" {
		t.Fatal("expected opus to be downgraded")
	}
}

func TestPlanDowngrade_NoChangeWhenFits(t *testing.T) {
	r, costs := mustRouter(t)
	e := New(costs, 500, 1000)
	current := map[string]string{"n1": "mock:echo-v1"}
	res := e.PlanDowngrade(current, Budget{MaxCostUSD: 0.10}, r)
	if res.Downgraded != 0 {
		t.Fatalf("expected no changes, got %d", res.Downgraded)
	}
	if res.Unachievable {
		t.Fatal("free plan should fit")
	}
}

func TestPlanDowngrade_UnachievableWhenFreeNodeExcluded(t *testing.T) {
	r, costs := mustRouter(t)
	_ = r
	// Build a router-like that returns no alternatives to force the unachievable path.
	r2 := &stubR{alternatives: map[string][]router.ScoredModel{}}
	res := (&Estimator{costs: costs, promptTokens: 500, outputTokens: 1000}).PlanDowngrade(
		map[string]string{"n1": "anthropic:claude-3-opus-latest"},
		Budget{MaxCostUSD: 0.0001},
		r2,
	)
	if !res.Unachievable {
		t.Fatal("expected unachievable for empty alternatives")
	}
}

func TestPlanDowngrade_NilRouter(t *testing.T) {
	_, costs := mustRouter(t)
	e := New(costs, 500, 1000)
	res := e.PlanDowngrade(map[string]string{"n1": "openai:gpt-4o"}, Budget{MaxCostUSD: 0.01}, nil)
	if !res.Unachievable {
		t.Fatal("nil router → unachievable")
	}
}

func TestRound4(t *testing.T) {
	if got := round4(1.234567); got != 1.2346 {
		t.Fatalf("got %f", got)
	}
}

func TestCloneMap(t *testing.T) {
	m := map[string]string{"a": "1"}
	c := cloneMap(m)
	c["a"] = "2"
	if m["a"] != "1" {
		t.Fatal("clone mutated original")
	}
}

func TestCheaperAlternativesExists(t *testing.T) {
	// Ensure the cascade can pick a strictly cheaper model than opus.
	r, _ := mustRouter(t)
	alts := r.CheaperAlternatives(context.Background(), decomposer.ClassReasoning, []string{"anthropic:claude-3-opus-latest"}, 3)
	if len(alts) == 0 {
		t.Fatal("expected alternatives to opus")
	}
	if alts[0].Model.String() == "anthropic:claude-3-opus-latest" {
		t.Fatal("excluded model leaked")
	}
}

// stubR is a router.Router that returns no alternatives, forcing the cascade
// to fail.
type stubR struct {
	alternatives map[string][]router.ScoredModel
}

func (s *stubR) Route(_ context.Context, _ router.NodeSummary) (*router.Decision, error) {
	return &router.Decision{Model: router.ModelID{Provider: "mock", Model: "x"}}, nil
}
func (s *stubR) CheaperAlternatives(_ context.Context, _ decomposer.TaskClass, _ []string, _ int) []router.ScoredModel {
	return nil
}
func (s *stubR) RecordFeedback(_ context.Context, _ router.FeedbackEvent) error { return nil }