// Package budget enforces cost + carbon caps per conversation.
//
// MVP (Phase 7): pre-DAG estimator projects (cost, carbon). If over budget,
// cascade downgrades Opus → Sonnet → Mixtral → local until it fits.
package budget
