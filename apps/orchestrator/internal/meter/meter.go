// Package meter records per-usage events for downstream billing.
//
// Three implementations live here:
//
//   - NoopMeterer      — discards events (dev/test, default)
//   - JSONLMeterer     — appends JSONL to a local file (debug-friendly)
//   - StripeMeterer    — POSTs to the Stripe v1 /billing/meter_events
//                        ingestion API when STRIPE_API_KEY is configured.
//
// The interface stays narrow on purpose so the executor doesn't gain a
// hard Stripe dependency. Swap the implementation by setting Server.Meter
// in main.go.
//
// All events are encoded with a versioned schema so the on-disk log remains
// replayable across meter upgrades.
package meter

import "time"

// Event is one billable usage record. The schema is intentionally minimal;
// cost calculations happen downstream where we have the cost table.
type Event struct {
	V          string    `json:"v"`           // "usage.v1"
	RunID      string    `json:"run_id"`      // unique run id (uuid or trace id)
	NodeID     string    `json:"node_id"`     // sub-task that incurred cost
	Model      string    `json:"model"`       // "provider:model"
	TokensIn   int       `json:"tokens_in"`   // input tokens
	TokensOut  int       `json:"tokens_out"`  // output tokens
	CostUSD    float64   `json:"cost_usd"`    // estimated USD
	CarbonG    float64   `json:"carbon_g"`    // estimated gCO2
	LatencyMS  int       `json:"latency_ms"`  // wall-clock duration
	Workspace  string    `json:"workspace"`   // optional; empty if unscoped
	OccurredAt time.Time `json:"occurred_at"`
}

// Meterer is the billing interface.
type Meterer interface {
	// Record is non-blocking; implementations should buffer if they make
	// network calls so the orchestrator's hot path isn't slowed.
	Record(ev Event)
	// Flush drains any pending events before shutdown. Returns the first
	// error (if any). Safe to call concurrently.
	Flush() error
}

// Default returns the no-op meter so missing config never panics.
func Default() Meterer { return NoopMeterer{} }

// NoopMeterer discards every event. Used by tests + dev when no billing is
// desired. Zero cost.
type NoopMeterer struct{}

// Record is a no-op.
func (NoopMeterer) Record(Event) {}

// Flush is a no-op.
func (NoopMeterer) Flush() error { return nil }
