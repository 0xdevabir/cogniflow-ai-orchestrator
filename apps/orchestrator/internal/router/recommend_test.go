package router

import (
	"testing"
	"time"

	"github.com/cogniflow/orchestrator/internal/banditlearn"
	"github.com/cogniflow/orchestrator/internal/decomposer"
)

func TestApplyRecommendation_BoostsBench(t *testing.T) {
	base := map[decomposer.TaskClass]Weights{
		decomposer.ClassReasoning: {Bench: 0.45, Cost: 0.30, Latency: 0.25},
		decomposer.ClassFactual:   {Bench: 0.45, Cost: 0.30, Latency: 0.25},
	}
	rec := &banditlearn.Recommendation{
		GeneratedAt: time.Now(),
		TotalEvents: 100,
		Classes: []banditlearn.ClassSummary{
			{
				TaskClass:             "reasoning",
				Winner:                "anthropic:claude-3-5-sonnet-latest",
				RecommendedBenchBoost: 0.18,
			},
		},
	}
	got, err := ApplyRecommendation(rec, base)
	if err != nil {
		t.Fatal(err)
	}

	r := got[decomposer.ClassReasoning]
	if r.Bench < 0.45 || r.Bench > 0.45+0.20+0.001 {
		t.Errorf("bench not boosted: %v", r.Bench)
	}
	if r.Cost+r.Latency <= 0 {
		t.Errorf("cost+lat must remain positive: %v", r)
	}
	// factual untouched
	if got[decomposer.ClassFactual] != base[decomposer.ClassFactual] {
		t.Errorf("factual untouched: got %v", got[decomposer.ClassFactual])
	}
}

func TestApplyRecommendation_ClampsLargeBoost(t *testing.T) {
	base := map[decomposer.TaskClass]Weights{
		decomposer.ClassReasoning: {Bench: 0.45, Cost: 0.30, Latency: 0.25},
	}
	rec := &banditlearn.Recommendation{
		Classes: []banditlearn.ClassSummary{
			{
				TaskClass:             "reasoning",
				Winner:                "x",
				RecommendedBenchBoost: 1.0, // huge
			},
		},
	}
	got, err := ApplyRecommendation(rec, base)
	if err != nil {
		t.Fatal(err)
	}
	r := got[decomposer.ClassReasoning]
	if r.Bench > 0.45+0.20+0.001 {
		t.Errorf("bench not clamped: %v", r.Bench)
	}
}

func TestApplyRecommendation_NilLeavesBase(t *testing.T) {
	base := map[decomposer.TaskClass]Weights{
		decomposer.ClassReasoning: {Bench: 0.45, Cost: 0.30, Latency: 0.25},
	}
	got, err := ApplyRecommendation(nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if got[decomposer.ClassReasoning] != base[decomposer.ClassReasoning] {
		t.Error("nil rec should leave base unchanged")
	}
}

func TestApplyRecommendation_NoWinnerSkips(t *testing.T) {
	base := map[decomposer.TaskClass]Weights{
		decomposer.ClassFactual: {Bench: 0.45, Cost: 0.30, Latency: 0.25},
	}
	rec := &banditlearn.Recommendation{
		Classes: []banditlearn.ClassSummary{
			{TaskClass: "factual", Winner: ""}, // no winner
		},
	}
	got, err := ApplyRecommendation(rec, base)
	if err != nil {
		t.Fatal(err)
	}
	if got[decomposer.ClassFactual] != base[decomposer.ClassFactual] {
		t.Errorf("no-winner rec should leave base unchanged: %v", got[decomposer.ClassFactual])
	}
}

func TestLoadRecommendation_NoPath(t *testing.T) {
	got, err := LoadRecommendation("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestLoadRecommendation_MissingFile(t *testing.T) {
	got, err := LoadRecommendation("/tmp/cogniflow-does-not-exist.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map on missing file, got %v", got)
	}
}
