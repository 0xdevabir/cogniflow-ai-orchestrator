package banditlearn

import (
	"strings"
	"testing"
	"time"
)

const sampleLog = `
{"run_id":"r1","node_id":"n1","bandit_arm_id":"a","model":"openai:gpt-4o-mini","task_class":"reasoning","satisfaction":0.7,"latency_ms":900,"cost_usd":0.001,"timestamp":"2026-01-01T00:00:00Z"}
{"run_id":"r1","node_id":"n1","bandit_arm_id":"a","model":"openai:gpt-4o-mini","task_class":"reasoning","satisfaction":0.6,"latency_ms":850,"cost_usd":0.001,"timestamp":"2026-01-01T00:00:01Z"}
{"run_id":"r1","node_id":"n1","bandit_arm_id":"a","model":"openai:gpt-4o-mini","task_class":"reasoning","satisfaction":0.8,"latency_ms":950,"cost_usd":0.001,"timestamp":"2026-01-01T00:00:02Z"}
{"run_id":"r1","node_id":"n1","bandit_arm_id":"b","model":"anthropic:claude-3-5-sonnet-latest","task_class":"reasoning","satisfaction":0.9,"latency_ms":1800,"cost_usd":0.05,"timestamp":"2026-01-01T00:00:03Z"}
{"run_id":"r1","node_id":"n1","bandit_arm_id":"b","model":"anthropic:claude-3-5-sonnet-latest","task_class":"reasoning","satisfaction":0.85,"latency_ms":2000,"cost_usd":0.06,"timestamp":"2026-01-01T00:00:04Z"}
{"run_id":"r2","node_id":"n2","bandit_arm_id":"c","model":"openai:gpt-4o-mini","task_class":"factual","satisfaction":0.5,"latency_ms":600,"cost_usd":0.0007,"timestamp":"2026-01-01T00:00:05Z"}
{"run_id":"r2","node_id":"n2","bandit_arm_id":"c","model":"openai:gpt-4o-mini","task_class":"factual","satisfaction":0.55,"latency_ms":600,"cost_usd":0.0007,"timestamp":"2026-01-01T00:00:06Z"}
{"run_id":"r2","node_id":"n2","bandit_arm_id":"d","model":"mock:echo-v1","task_class":"factual","satisfaction":0.0,"latency_ms":100,"cost_usd":0.0,"timestamp":"2026-01-01T00:00:07Z"}
`

func TestLearn(t *testing.T) {
	rec, err := Learn(strings.NewReader(sampleLog), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rec.TotalEvents != 8 {
		t.Errorf("total events = %d, want 8", rec.TotalEvents)
	}
	if len(rec.Classes) != 2 {
		t.Fatalf("expected 2 classes, got %d", len(rec.Classes))
	}

	// Find "reasoning" + "factual" classes.
	var reasoning, factual *ClassSummary
	for i := range rec.Classes {
		switch rec.Classes[i].TaskClass {
		case "reasoning":
			reasoning = &rec.Classes[i]
		case "factual":
			factual = &rec.Classes[i]
		}
	}
	if reasoning == nil || factual == nil {
		t.Fatalf("missing class: %+v", rec.Classes)
	}

	// Reasoning: claude should win (mean 0.875 vs gpt-4o-mini 0.7).
	if reasoning.Winner != "anthropic:claude-3-5-sonnet-latest" {
		t.Errorf("reasoning winner = %q, want claude", reasoning.Winner)
	}
	if reasoning.RecommendedBenchBoost <= 0 || reasoning.RecommendedBenchBoost > 1 {
		t.Errorf("boost out of range: %v", reasoning.RecommendedBenchBoost)
	}

	// Factual: gpt-4o-mini should win (mean 0.525 vs mock 0.0).
	if factual.Winner != "openai:gpt-4o-mini" {
		t.Errorf("factual winner = %q, want gpt-4o-mini", factual.Winner)
	}

	// String summary should mention winner.
	s := rec.String()
	if !strings.Contains(s, "anthropic:claude-3-5-sonnet-latest") {
		t.Errorf("summary missing winner: %q", s)
	}
}

func TestLearnMinCountFiltersLowSamples(t *testing.T) {
	// Only 1 event per model; minCount=5 should yield no winner.
	rec, err := Learn(strings.NewReader(sampleLog), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Classes {
		if c.Winner != "" {
			t.Errorf("class %s should have no winner, got %q", c.TaskClass, c.Winner)
		}
	}
}

func TestLearnEmptyLog(t *testing.T) {
	rec, err := Learn(strings.NewReader(""), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.TotalEvents != 0 || len(rec.Classes) != 0 {
		t.Errorf("expected empty recommendation, got %+v", rec)
	}
}

func TestLearnSkipsMalformedLines(t *testing.T) {
	bad := sampleLog + "\n{not json\n"
	rec, err := Learn(strings.NewReader(bad), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.TotalEvents != 8 {
		t.Errorf("total events = %d, want 8", rec.TotalEvents)
	}
}

func TestLearnTimestampPreserved(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec, err := Learn(strings.NewReader(sampleLog), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.GeneratedAt.Location() != time.UTC {
		t.Errorf("expected UTC generated_at, got %v", rec.GeneratedAt.Location())
	}
	if ts.After(rec.GeneratedAt) {
		t.Errorf("generated_at %v before sample times", rec.GeneratedAt)
	}
}

func TestMeanAndClamp(t *testing.T) {
	if got := mean(ModelStats{}); got != 0 {
		t.Errorf("mean of empty = %v", got)
	}
	if got := mean(ModelStats{Count: 2, SatSum: 1.0}); got != 0.5 {
		t.Errorf("mean = %v", got)
	}
	if clamp01(-1) != 0 || clamp01(2) != 1 || clamp01(0.5) != 0.5 {
		t.Error("clamp01 wrong")
	}
}

func TestJSON(t *testing.T) {
	rec, err := Learn(strings.NewReader(sampleLog), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rec.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "reasoning") {
		t.Errorf("json missing reasoning: %s", b)
	}
}
