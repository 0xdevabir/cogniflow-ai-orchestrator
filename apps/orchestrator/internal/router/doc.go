// Package router picks the best model for a given sub-task.
//
// MVP (Phase 3): a weighted-score heuristic over a static benchmarks.json +
// cost_table.json. A Bandit interface logs (task_class, model, score,
// was_chosen, satisfaction) to JSONL for offline replay.
//
// Score (default weights):
//
//	score = 0.45 · bench(model, task_class)
//	      + 0.30 · (1 - normalized_cost)
//	      + 0.25 · (1 - normalized_p95_latency)
//
// Overrides per task class are accepted in WeightedConfig.Weights. The router:
//
//  1. Filters candidates by latency_budget_ms + max_cost_usd before scoring.
//  2. Falls back to mock:echo-v1 when nothing fits (always-available, free).
//  3. Picks the highest score; ties broken deterministically by cost ↑, then
//     latency ↑.
//  4. Returns a Decision with the full scored Alternatives list, the per-
//     component Breakdown, a human-readable Reason, and a stable
//     BanditArmID = sha256(task_class|model)[:6] for offline replay.
//
// Every decision is appended to the FeedbackLogger (when configured) as a
// FeedbackEvent with Satisfaction=0 — the user-feedback loop (Phase 7) writes
// a second event with the actual satisfaction to enable offline policy
// improvement.
//
// Phase 7 swaps in cascade-downgrade logic on top of this score; Phase 8
// swaps in LinUCB and replays the JSONL log into a learned policy.
package router
