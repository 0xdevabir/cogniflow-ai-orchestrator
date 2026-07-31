// Package router picks the best model for a given sub-task.
//
// MVP (Phase 3): a weighted-score heuristic over a static benchmarks.json +
// cost_table.json. A Bandit interface logs (task_class, model, score, was_chosen)
// for offline replay. Phase 7 swaps in LinUCB.
package router
