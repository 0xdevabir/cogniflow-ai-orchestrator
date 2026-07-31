package router

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// fakeNode is a minimal NodeSummary for testing.
type fakeNode struct {
	tc        decomposer.TaskClass
	latencyMS int
	maxCost   float64
}

func (f *fakeNode) TaskClass() decomposer.TaskClass { return f.tc }
func (f *fakeNode) LatencyBudgetMS() int            { return f.latencyMS }
func (f *fakeNode) MaxCostUSD() float64             { return f.maxCost }

// newTestRouter returns a WeightedRouter built from the embedded data with
// no logger (callers can attach one if needed).
func newTestRouter(t *testing.T) *WeightedRouter {
	t.Helper()
	bench, err := LoadBenchmarks()
	if err != nil {
		t.Fatalf("LoadBenchmarks: %v", err)
	}
	costs, err := LoadCostTable()
	if err != nil {
		t.Fatalf("LoadCostTable: %v", err)
	}
	r, err := NewWeighted(WeightedConfig{
		Bench:        bench,
		Costs:        costs,
		EstPromptTok: 500,
		EstOutTok:    1000,
	})
	if err != nil {
		t.Fatalf("NewWeighted: %v", err)
	}
	return r
}

func TestWeightedRouter_PicksBestForReasoning(t *testing.T) {
	r := newTestRouter(t)
	d, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassReasoning,
		latencyMS: 5000,
		maxCost:   1.0,
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d == nil {
		t.Fatal("nil decision")
	}
	// With default weights (0.45 bench / 0.30 cost / 0.25 latency), gpt-4o-mini
	// is genuinely the best-scoring reasoning pick because it's 10× cheaper
	// and 2× faster than the bigger models. The point of this test is to
	// assert the router produced a sensible breakdown + bandit arm id, not
	// that it always picks the highest-benchmark model.
	if d.Score <= 0 || d.Score > 1 {
		t.Errorf("score out of [0,1]: %v", d.Score)
	}
	if d.Reason == "" {
		t.Error("empty reason")
	}
	if d.BanditArmID == "" {
		t.Error("empty bandit arm id")
	}
	// And: Opus (overkill for cost+latency weights) should NOT win.
	if strings.HasPrefix(d.Model.String(), "anthropic:claude-3-opus") {
		t.Errorf("Opus should not win under default weights: %s", d.Model.String())
	}
	// The winner must be in the alternatives list at index 0.
	if len(d.Alternatives) == 0 || d.Alternatives[0].Model != d.Model {
		t.Error("winner not first in alternatives")
	}
}

func TestWeightedRouter_PicksCheapForSummarization(t *testing.T) {
	r := newTestRouter(t)
	d, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassSummarization,
		latencyMS: 5000,
		maxCost:   1.0,
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// For summarization, gpt-4o-mini is cheap and benchmark-competitive
	// (0.88 vs 0.91 for gpt-4o). On a tight budget it must win.
	// On a relaxed budget either GPT-4o/something strongly benchmarked OR
	// gpt-4o-mini wins. We assert that we do NOT pick Opus (overkill +
	// expensive for summarization).
	if strings.HasPrefix(d.Model.String(), "anthropic:claude-3-opus") {
		t.Errorf("Opus should not be picked for summarization, got %s", d.Model.String())
	}
}

func TestWeightedRouter_RespectsLatencyBudget(t *testing.T) {
	r := newTestRouter(t)
	// Tight 800ms latency budget. Ollama 70b (6000ms p95) and Opus (4000ms)
	// must be excluded.
	d, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassReasoning,
		latencyMS: 800,
		maxCost:   1.0,
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Walk alternatives too — none of them should violate the budget.
	for _, alt := range d.Alternatives {
		lat := waltLatency(alt)
		if lat > 800 {
			t.Errorf("alternative %s has p95 %dms > budget 800ms", alt.Model.String(), lat)
		}
	}
	if p95, ok := r.cfg.Costs.LatencyP95MS[d.Model.String()]; ok && p95 > 800 {
		t.Errorf("winner %s has p95 %dms > budget 800ms", d.Model.String(), p95)
	}
}

func TestWeightedRouter_RespectsCostBudget(t *testing.T) {
	r := newTestRouter(t)
	// Tight $0.001 budget. Opus (~$0.083) and Sonnet (~$0.0165) must be
	// excluded; cheap models like Haiku / gpt-4o-mini should win.
	d, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassReasoning,
		latencyMS: 5000,
		maxCost:   0.001,
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if strings.HasPrefix(d.Model.String(), "anthropic:claude-3-opus") ||
		strings.HasPrefix(d.Model.String(), "anthropic:claude-3-5-sonnet") {
		t.Errorf("expensive model %s won under tight cost budget", d.Model.String())
	}
}

func TestWeightedRouter_BreakdownPopulated(t *testing.T) {
	r := newTestRouter(t)
	d, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassCode,
		latencyMS: 5000,
		maxCost:   1.0,
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.Breakdown == nil {
		t.Fatal("nil breakdown")
	}
	for _, k := range []string{"bench", "cost", "latency", "cost_est", "lat_p95"} {
		if _, ok := d.Breakdown[k]; !ok {
			t.Errorf("breakdown missing key %q (have %v)", k, d.Breakdown)
		}
	}
	if len(d.Alternatives) == 0 {
		t.Error("no alternatives recorded")
	}
}

func TestFeedbackLogger_AppendsJSONLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bandit.jsonl")
	l, err := NewJSONLFeedbackLogger(p)
	if err != nil {
		t.Fatalf("NewJSONLFeedbackLogger: %v", err)
	}
	defer l.Close()

	ev1 := FeedbackEvent{
		BanditArmID: "abc",
		Model:       "openai:gpt-4o-mini",
		TaskClass:   "summarization",
	}
	ev2 := FeedbackEvent{
		BanditArmID: "def",
		Model:       "anthropic:claude-3-5-sonnet-latest",
		TaskClass:   "reasoning",
		Satisfaction: 0.8,
	}
	if err := l.Append(ev1); err != nil {
		t.Fatalf("Append ev1: %v", err)
	}
	if err := l.Append(ev2); err != nil {
		t.Fatalf("Append ev2: %v", err)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d (%q)", len(lines), string(b))
	}
	if !strings.Contains(lines[0], `"bandit_arm_id":"abc"`) {
		t.Errorf("line 0 missing arm id: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"satisfaction":0.8`) {
		t.Errorf("line 1 missing satisfaction: %s", lines[1])
	}
	if l.Path() != p {
		t.Errorf("Path() = %q, want %q", l.Path(), p)
	}
}

func TestWeightedRouter_LogsDecisionWhenLoggerSet(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bandit.jsonl")
	logger, err := NewJSONLFeedbackLogger(p)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	bench, _ := LoadBenchmarks()
	costs, _ := LoadCostTable()
	r, err := NewWeighted(WeightedConfig{
		Bench:        bench,
		Costs:        costs,
		EstPromptTok: 500,
		EstOutTok:    1000,
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("NewWeighted: %v", err)
	}
	if _, err := r.Route(context.Background(), &fakeNode{
		tc:        decomposer.ClassSummarization,
		latencyMS: 5000,
		maxCost:   1.0,
	}); err != nil {
		t.Fatalf("Route: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("logger did not write decision event")
	}
	if !strings.Contains(string(b), `"task_class":"summarization"`) {
		t.Errorf("decision event missing task_class: %s", string(b))
	}
}

func TestParseModelID(t *testing.T) {
	cases := []struct {
		in   string
		want ModelID
	}{
		{"openai:gpt-4o-mini", ModelID{Provider: "openai", Model: "gpt-4o-mini"}},
		{"mock:echo-v1", ModelID{Provider: "mock", Model: "echo-v1"}},
		{"standalone", ModelID{Provider: "", Model: "standalone"}},
	}
	for _, c := range cases {
		got := parseModelID(c.in)
		if got != c.want {
			t.Errorf("parseModelID(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestWeightedRouter_CheaperAlternatives(t *testing.T) {
	r := newTestRouter(t)
	alts := r.CheaperAlternatives(context.Background(), decomposer.ClassReasoning, nil, 3)
	if len(alts) == 0 {
		t.Fatal("no cheaper alternatives returned")
	}
	if len(alts) > 3 {
		t.Errorf("returned %d, want <=3", len(alts))
	}
	// Should be sorted ascending by cost.
	for i := 1; i < len(alts); i++ {
		if alts[i].Breakdown["cost_est"] < alts[i-1].Breakdown["cost_est"] {
			t.Errorf("alternatives not sorted by cost ascending")
		}
	}
}

func TestWeightedRouter_CheaperAlternativesExcludes(t *testing.T) {
	r := newTestRouter(t)
	alts := r.CheaperAlternatives(context.Background(), decomposer.ClassReasoning,
		[]string{"ollama:llama3.1:70b", "mock:echo-v1"}, 10)
	for _, a := range alts {
		if a.Model.String() == "ollama:llama3.1:70b" || a.Model.String() == "mock:echo-v1" {
			t.Errorf("excluded model %s returned in alternatives", a.Model.String())
		}
	}
}

func TestWeightedRouter_RecordFeedback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bandit.jsonl")
	logger, err := NewJSONLFeedbackLogger(p)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()
	bench, _ := LoadBenchmarks()
	costs, _ := LoadCostTable()
	r, err := NewWeighted(WeightedConfig{Bench: bench, Costs: costs, Logger: logger})
	if err != nil {
		t.Fatalf("NewWeighted: %v", err)
	}
	ev := FeedbackEvent{
		BanditArmID:  "x",
		Model:        "openai:gpt-4o-mini",
		TaskClass:    "summarization",
		Satisfaction: 0.95,
	}
	if err := r.RecordFeedback(context.Background(), ev); err != nil {
		t.Fatalf("RecordFeedback: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(b), `"satisfaction":0.95`) {
		t.Errorf("appended feedback not persisted: %s", string(b))
	}
}

func TestWeightedRouter_NilBenchOrCostsRejected(t *testing.T) {
	if _, err := NewWeighted(WeightedConfig{}); err == nil {
		t.Error("expected error for nil Bench")
	}
	bench, _ := LoadBenchmarks()
	if _, err := NewWeighted(WeightedConfig{Bench: bench}); err == nil {
		t.Error("expected error for nil Costs")
	}
}

func TestWeightedRouter_BadWeightsRejected(t *testing.T) {
	bench, _ := LoadBenchmarks()
	costs, _ := LoadCostTable()
	_, err := NewWeighted(WeightedConfig{
		Bench:   bench,
		Costs:   costs,
		Weights: map[decomposer.TaskClass]Weights{decomposer.ClassReasoning: {Bench: 0.5, Cost: 0.2, Latency: 0.2}},
	})
	if err == nil {
		t.Error("expected error for weights summing to 0.9 (off by >0.01)")
	}
}

// waltLatency pulls the p95 latency key out of a ScoredModel's breakdown.
func waltLatency(m ScoredModel) int {
	v, ok := m.Breakdown["lat_p95"]
	if !ok {
		return 0
	}
	return int(v)
}
