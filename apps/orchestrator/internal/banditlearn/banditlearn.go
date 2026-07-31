// Package banditlearn re-weights the orchestrator's router from logged
// feedback events.
//
// The router weights in internal/router are hard-coded defaults
// (0.45·bench + 0.30·cost + 0.25·latency). As real feedback accumulates in
// the JSONL log, operators wants to bias the router toward the models that
// actually perform well on each task class. This package reads the log,
// groups feedback by task_class, and emits a recommendation JSON either
// printed to stderr or written to a config file the weighted router can
// consume at startup.
//
// The produced weights are *adjustments* to the existing per-class weights,
// not absolute values. The recommended model for each task class is the
// one with the highest empirical (mean) satisfaction, and we recommend
// nudging the bench weight up by the relative empirical-confidence gain.
//
// Usage (CLI):
//
//	go run ./cmd/bandit-learn -log ./data/bandit.jsonl
//	go run ./cmd/bandit-learn -log ./data/bandit.jsonl -min 50
package banditlearn

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// FeedbackEvent is the row the bandit log emits. We intentionally mirror
// router.FeedbackEvent so we don't take a hard dependency in this package.
type FeedbackEvent struct {
	RunID        string    `json:"run_id"`
	NodeID       string    `json:"node_id"`
	BanditArmID  string    `json:"bandit_arm_id"`
	Model        string    `json:"model"`
	TaskClass    string    `json:"task_class"`
	Satisfaction float64   `json:"satisfaction"`
	LatencyMS    int       `json:"latency_ms"`
	CostUSD      float64   `json:"cost_usd"`
	Timestamp    time.Time `json:"timestamp"`
}

// ModelStats is the per-(task_class, model) bucket.
type ModelStats struct {
	Model        string
	Count        int
	SatSum       float64
	LatSumMS     int
	CostSumUSD   float64
	FailureCount int // satisfaction == 0
}

// ClassSummary aggregates the per-model stats for a single task class.
type ClassSummary struct {
	TaskClass string
	Models    []ModelStats
	// Winner is the best model by mean satisfaction (with a min-count guard).
	Winner string
	// RecommendedBenchBoost is the relative jump from current best to
	// winner, in [0, 1]. Apply to the bench weight in the router config.
	RecommendedBenchBoost float64
}

// Recommendation is the per-task-class output of the learner.
type Recommendation struct {
	GeneratedAt time.Time     `json:"generated_at"`
	TotalEvents int           `json:"total_events"`
	Classes     []ClassSummary `json:"classes"`
	// AbsoluteWeights, when non-nil, is a per-task-class set of weights the
	// weighted router can load at startup. Currently only BenchBoost is
	// emitted; the absolute weights fall back to the defaults.
	AbsoluteWeights map[string]float64 `json:"absolute_weights,omitempty"`
}

// Learn reads the JSONL feedback log, groups by task_class, computes
// per-model means, and returns the recommendation.
//
// minCount is the minimum number of feedback events per (model, task_class)
// bucket before we trust the mean. Models below the threshold are excluded
// from the winner selection.
func Learn(r io.Reader, minCount int) (*Recommendation, error) {
	if minCount < 1 {
		minCount = 1
	}
	groups := map[string]map[string]*ModelStats{}
	total := 0
	scanner := bufio.NewScanner(r)
	// Allow long lines.
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev FeedbackEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Skip malformed but be loud on stderr.
			fmt.Fprintf(os.Stderr, "bandit-learn: skip bad line: %v\n", err)
			continue
		}
		if ev.TaskClass == "" || ev.Model == "" {
			continue
		}
		if _, ok := groups[ev.TaskClass]; !ok {
			groups[ev.TaskClass] = map[string]*ModelStats{}
		}
		g := groups[ev.TaskClass]
		ms, ok := g[ev.Model]
		if !ok {
			ms = &ModelStats{Model: ev.Model}
			g[ev.Model] = ms
		}
		ms.Count++
		ms.SatSum += ev.Satisfaction
		ms.LatSumMS += ev.LatencyMS
		ms.CostSumUSD += ev.CostUSD
		if ev.Satisfaction == 0 {
			ms.FailureCount++
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	rec := &Recommendation{
		GeneratedAt: time.Now().UTC(),
		TotalEvents: total,
	}
	for tc, models := range groups {
		summary := ClassSummary{TaskClass: tc}
		for _, ms := range models {
			summary.Models = append(summary.Models, *ms)
		}
		// Sort by mean satisfaction desc, then by count desc.
		sort.Slice(summary.Models, func(i, j int) bool {
			if meanI, meanJ := mean(summary.Models[i]), mean(summary.Models[j]); meanI != meanJ {
				return meanI > meanJ
			}
			return summary.Models[i].Count > summary.Models[j].Count
		})

		// Pick the winner among models with enough samples.
		best := -1
		bestScore := math.Inf(-1)
		for i, ms := range summary.Models {
			if ms.Count < minCount {
				continue
			}
			if m := mean(ms); m > bestScore {
				bestScore = m
				best = i
			}
		}
		switch {
		case best < 0 || bestScore <= 0:
			summary.Winner = ""
		default:
			summary.Winner = summary.Models[best].Model
			// Boost = clamp(winner_mean - second_mean, 0, 1). With only
			// one viable model the boost is the winner's mean itself.
			if best+1 < len(summary.Models) {
				summary.RecommendedBenchBoost = clamp01(bestScore - mean(summary.Models[best+1]))
			} else {
				summary.RecommendedBenchBoost = clamp01(bestScore)
			}
		}
		rec.Classes = append(rec.Classes, summary)
	}
	sort.Slice(rec.Classes, func(i, j int) bool {
		return rec.Classes[i].TaskClass < rec.Classes[j].TaskClass
	})
	return rec, nil
}

// LearnFile is a small wrapper around Learn that opens a path.
func LearnFile(path string, minCount int) (*Recommendation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Learn(f, minCount)
}

// String returns a human-readable summary.
func (r *Recommendation) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bandit recommendation (generated %s, %d events):\n",
		r.GeneratedAt.Format(time.RFC3339), r.TotalEvents)
	for _, c := range r.Classes {
		fmt.Fprintf(&b, "  %s:\n", c.TaskClass)
		for _, ms := range c.Models {
			fmt.Fprintf(&b, "    %-40s n=%-4d mean=%.3f lat=%dms cost=$%.4f fails=%d\n",
				ms.Model, ms.Count, mean(ms), ms.LatSumMS/ms.Count, ms.CostSumUSD,
				ms.FailureCount)
		}
		if c.Winner != "" {
			fmt.Fprintf(&b, "    ⇒ winner: %s (boost bench weight by %.2f)\n",
				c.Winner, c.RecommendedBenchBoost)
		} else {
			fmt.Fprintf(&b, "    ⇒ no confident winner (need more feedback)\n")
		}
	}
	return b.String()
}

// JSON marshals the recommendation.
func (r *Recommendation) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func mean(s ModelStats) float64 {
	if s.Count == 0 {
		return 0
	}
	return s.SatSum / float64(s.Count)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
