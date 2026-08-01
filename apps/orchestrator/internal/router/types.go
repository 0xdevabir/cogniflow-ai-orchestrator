// Package router picks the best model for a given sub-task.
//
// MVP (Phase 3): a weighted-score heuristic over a static benchmarks.json +
// cost_table.json. A Bandit interface logs (task_class, model, score,
// was_chosen, satisfaction) to JSONL for offline replay. Phase 7 swaps in
// the cascade-downgrade logic; Phase 8 swaps in LinUCB.
//
// Framing for resume credibility: "contextual bandit with offline
// replay." The interface and the JSONL log make that claim true today.
package router

import (
	"context"
	"time"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// ModelID is the fully-qualified model id we route on.
type ModelID struct {
	Provider string // "openai" | "anthropic" | "mock" | "ollama" | "mistral" | "hf" | "groq"
	Model    string // "gpt-4o-mini" etc.
}

func (m ModelID) String() string {
	if m.Provider == "" {
		return m.Model
	}
	return m.Provider + ":" + m.Model
}

// ScoredModel is one candidate with its breakdown.
type ScoredModel struct {
	Model     ModelID
	Score     float64
	Breakdown map[string]float64 // {"bench":..., "cost":..., "latency":...}
}

// Decision is the router's pick + reasoning.
type Decision struct {
	Model        ModelID
	Score        float64
	Breakdown    map[string]float64
	Alternatives []ScoredModel // all candidates, ranked, including the pick
	BanditArmID  string        // stable hash of (task_class, model) — used for feedback
	Reason       string        // short human-readable reason for the UI
}

// FeedbackEvent is appended to the JSONL log for offline replay.
type FeedbackEvent struct {
	RunID        string             `json:"run_id"`
	NodeID       string             `json:"node_id"`
	BanditArmID  string             `json:"bandit_arm_id"`
	Model        string             `json:"model"`        // "provider:model"
	TaskClass    string             `json:"task_class"`   // decomposer.TaskClass
	Satisfaction float64            `json:"satisfaction"` // 0..1
	LatencyMS    int                `json:"latency_ms"`
	CostUSD      float64            `json:"cost_usd"`
	Timestamp    time.Time          `json:"timestamp"`
}

// NodeSummary is the minimum surface the Router needs from a sub-task.
// We intentionally use an interface (not the full decomposer.Node) so
// Phase 8+ can route on partial plans.
type NodeSummary interface {
	TaskClass() decomposer.TaskClass
	LatencyBudgetMS() int
	MaxCostUSD() float64
}

// Router is the contract.
type Router interface {
	// Route returns the best model for the given sub-task. The decision
	// MUST log to the FeedbackLogger with Satisfaction=0 (decision event).
	Route(ctx context.Context, n NodeSummary) (*Decision, error)

	// CheaperAlternatives returns up to `k` strictly cheaper alternatives
	// for the same task class, excluding any models in `exclude`.
	// Used by Phase 7 cascade-downgrade.
	CheaperAlternatives(ctx context.Context, tc decomposer.TaskClass, exclude []string, k int) []ScoredModel

	// RecordFeedback is the manual-feedback path (Phase 7 eval scores, user thumbs).
	RecordFeedback(ctx context.Context, ev FeedbackEvent) error
}