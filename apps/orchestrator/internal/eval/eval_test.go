package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cogniflow/orchestrator/internal/citation"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/router"
)

func TestScore_CalculatesCost(t *testing.T) {
	costs, err := loadCosts()
	if err != nil {
		t.Fatal(err)
	}
	j := New(nil, costs, "")
	res, err := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Usage: []UsageEvent{
			{NodeID: "n1", Model: "openai:gpt-4o-mini", TokensIn: 1000, TokensOut: 1000, DurationMS: 500},
		},
		Streams: map[string]string{"n1": "ok"},
		Plan: &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{
			{ID: "n1", Role: decomposer.RoleSynthesizer},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// gpt-4o-mini: 0.15 in / 0.60 out → 1k in = 0.00015, 1k out = 0.0006
	if res.CostUSD < 0.0005 || res.CostUSD > 0.0010 {
		t.Fatalf("cost %f out of expected range", res.CostUSD)
	}
	if res.LatencyTotalMS < 500 {
		t.Fatalf("latency_total %d too small", res.LatencyTotalMS)
	}
	if res.LatencyP95MS != 500 {
		t.Fatalf("p95 got %d, want 500", res.LatencyP95MS)
	}
	if len(res.ModelMix) != 1 || res.ModelMix[0] != "openai:gpt-4o-mini" {
		t.Fatalf("model mix: %v", res.ModelMix)
	}
}

func TestScore_PerNodeBreakdown(t *testing.T) {
	costs, _ := loadCosts()
	j := New(nil, costs, "")
	res, _ := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-2 * time.Second),
		EndedAt:   time.Now(),
		Usage: []UsageEvent{
			{NodeID: "n1", Model: "openai:gpt-4o", TokensIn: 100, TokensOut: 100, DurationMS: 200},
			{NodeID: "n2", Model: "openai:gpt-4o-mini", TokensIn: 100, TokensOut: 100, DurationMS: 100},
			{NodeID: "n3", Model: "mock:echo-v1", TokensIn: 0, TokensOut: 0, DurationMS: 50},
		},
		Streams: map[string]string{"n3": "ok"},
		Plan: &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{
			{ID: "n1"}, {ID: "n2", DependsOn: []string{"n1"}}, {ID: "n3", DependsOn: []string{"n2"}},
		}},
	})
	if len(res.PerNode) != 3 {
		t.Fatalf("expected 3 per-node evals, got %d", len(res.PerNode))
	}
	if ne := res.PerNode["n3"]; ne.Model != "mock:echo-v1" || ne.CostUSD != 0 {
		t.Fatalf("n3: %+v", ne)
	}
}

func TestScore_FaithfulnessHeuristic_FullySupported(t *testing.T) {
	m := citation.New()
	m.Add(citation.Span{ID: "s1", Text: "the terminating party shall provide 30 days notice"})
	res, _ := heuristicScore(t, "the terminating party shall provide 30 days notice.", m)
	if res.FaithfulnessPct < 0.99 {
		t.Fatalf("expected ~1.0, got %f", res.FaithfulnessPct)
	}
}

func TestScore_FaithfulnessHeuristic_UnsupportedClaims(t *testing.T) {
	m := citation.New()
	m.Add(citation.Span{ID: "s1", Text: "the terminating party shall provide 30 days notice"})
	res, _ := heuristicScore(t, "the moon is made of cheese.", m)
	if res.FaithfulnessPct > 0.1 {
		t.Fatalf("expected low faithfulness, got %f", res.FaithfulnessPct)
	}
}

func TestScore_FaithfulnessHeuristic_MixedSupportedAndUnsupported(t *testing.T) {
	m := citation.New()
	m.Add(citation.Span{ID: "s1", Text: "the terminating party shall provide 30 days notice"})
	res, _ := heuristicScore(t, "the moon is made of cheese. the terminating party shall provide 30 days notice.", m)
	if res.FaithfulnessPct < 0.3 || res.FaithfulnessPct > 0.7 {
		t.Fatalf("expected ~0.5, got %f", res.FaithfulnessPct)
	}
}

func TestScore_RecordsDowngradeFrom(t *testing.T) {
	costs, _ := loadCosts()
	j := New(nil, costs, "")
	res, _ := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Usage: []UsageEvent{
			{NodeID: "n1", Model: "openai:gpt-4o-mini", DurationMS: 200},
		},
		Streams: map[string]string{"n1": "ok"},
		Plan: &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{{ID: "n1"}}},
		Downgrade: &DowngradeRecord{
			Original: map[string]string{"n1": "anthropic:claude-3-opus-latest"},
			New:      map[string]string{"n1": "openai:gpt-4o-mini"},
			SavedUSD: 0.1,
		},
	})
	if got := res.PerNode["n1"].DowngradedFrom; got != "anthropic:claude-3-opus-latest" {
		t.Fatalf("downgraded_from: %q", got)
	}
}

func TestScore_ParsesJudgeResult(t *testing.T) {
	// Simulate a judge LLM that returns valid JSON.
	body, _ := json.Marshal(judgeOutput{
		FaithfulnessPct: 0.87,
		UncitedClaims:   []string{"the moon is made of cheese"},
		Reasoning:       "one claim is unsourced",
	})
	costs, _ := loadCosts()
	j := New(providers.NewRegistryForTest([]string{string(body)}), costs, "mock:judge")
	res, err := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Usage:     []UsageEvent{{NodeID: "n1", Model: "mock:echo-v1", DurationMS: 100}},
		Streams:   map[string]string{"n1": "the moon is made of cheese."},
		Plan:      &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{{ID: "n1", Role: decomposer.RoleSynthesizer}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FaithfulnessPct != 0.87 {
		t.Fatalf("expected 0.87, got %f", res.FaithfulnessPct)
	}
	if res.HallucinationFlags != 1 {
		t.Fatalf("expected 1 hallucination flag, got %d", res.HallucinationFlags)
	}
	if res.JudgedBy == "" {
		t.Fatal("judged_by should be set when LLM judge ran")
	}
}

func TestScore_FallbackHeuristicWhenJudgeFails(t *testing.T) {
	costs, _ := loadCosts()
	reg := providers.NewRegistry(nil) // empty — judge will fail
	j := New(reg, costs, "openai:gpt-4o-mini")
	res, _ := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Usage:     []UsageEvent{{NodeID: "n1", Model: "mock:echo-v1", DurationMS: 100}},
		Streams:   map[string]string{"n1": "the terminating party shall provide 30 days notice."},
		Plan:      &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{{ID: "n1", Role: decomposer.RoleSynthesizer}}},
	})
	if res.JudgedBy != "heuristic" {
		t.Fatalf("expected fallback to heuristic, got %q", res.JudgedBy)
	}
}

func TestScore_LatencyP95_Sorts(t *testing.T) {
	costs, _ := loadCosts()
	j := New(nil, costs, "")
	res, _ := j.Score(context.Background(), RunRecord{
		StartedAt: time.Now().Add(-2 * time.Second),
		EndedAt:   time.Now(),
		Usage: []UsageEvent{
			{NodeID: "n1", Model: "mock:echo-v1", DurationMS: 100},
			{NodeID: "n2", Model: "mock:echo-v1", DurationMS: 200},
			{NodeID: "n3", Model: "mock:echo-v1", DurationMS: 300},
			{NodeID: "n4", Model: "mock:echo-v1", DurationMS: 4000},
		},
		Streams: map[string]string{"n4": "ok"},
		Plan: &decomposer.Plan{Version: "plan.v1", Nodes: []decomposer.Node{
			{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4", DependsOn: []string{"n1", "n2", "n3"}},
		}},
	})
	if res.LatencyP95MS != 4000 {
		t.Fatalf("p95 should be max (4000), got %d", res.LatencyP95MS)
	}
}

func TestSplitSentences(t *testing.T) {
	got := splitSentences("hi. there! you? okay")
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
}

func TestTruncate(t *testing.T) {
	if s := truncate("hello world", 5); s != "hello…" {
		t.Fatalf("got %q", s)
	}
	if s := truncate("hi", 5); s != "hi" {
		t.Fatalf("got %q", s)
	}
}

func TestPickSynthText(t *testing.T) {
	rec := RunRecord{
		Streams: map[string]string{"a": "alpha", "b": "beta", "c": "gamma"},
		Plan: &decomposer.Plan{Nodes: []decomposer.Node{
			{ID: "a"}, {ID: "b", DependsOn: []string{"a"}}, {ID: "c", DependsOn: []string{"b"}},
		}},
	}
	if got := pickSynthText(rec); got != "alpha" {
		t.Fatalf("synth should be the root (no dependents), got %q", got)
	}
}

func TestContainsCI(t *testing.T) {
	if !containsCI("Hello World", "world") {
		t.Fatal("should match case-insensitively")
	}
	if containsCI("", "x") {
		t.Fatal("empty haystack")
	}
	if containsCI("abc", "") {
		t.Fatal("empty needle")
	}
}

func TestBuildJudgePrompt_IncludesAllSpans(t *testing.T) {
	m := citation.New()
	m.Add(citation.Span{ID: "s1", Text: "first chunk", Model: "openai:gpt-4o", DocID: "doc1"})
	m.Add(citation.Span{ID: "s2", Text: "second chunk", Model: "mock:echo", DocID: "doc1"})
	prompt := buildJudgePrompt("the answer", RunRecord{Manifest: m})
	if !strings.Contains(prompt, "[1]") || !strings.Contains(prompt, "[2]") {
		t.Fatalf("prompt missing span markers: %s", prompt)
	}
	if !strings.Contains(prompt, "doc1") {
		t.Fatal("prompt should include doc id")
	}
}

// --- helpers ---

func loadCosts() (*router.CostTable, error) {
	return router.LoadCostTable()
}

// heuristicScore wraps a one-off heuristic invocation for tests.
func heuristicScore(t *testing.T, finalText string, m *citation.Manifest) (*Result, error) {
	t.Helper()
	pct := heuristicFaithfulness(finalText, m)
	return &Result{FaithfulnessPct: pct, HallucinationFlags: 0, JudgedBy: "heuristic"}, nil
}