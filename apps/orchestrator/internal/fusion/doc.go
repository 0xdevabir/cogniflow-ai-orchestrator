// Package fusion merges multiple sub-task streams into one coherent answer.
//
// MVP (Phase 5): incremental merger that consumes Chunk streams and emits a
// final stream with citation spans attached. On disagreement, calls the
// judge LLM (see internal/eval) and surfaces both verdicts.
package fusion
