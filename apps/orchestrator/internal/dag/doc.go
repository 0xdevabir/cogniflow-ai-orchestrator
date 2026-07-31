// Package dag executes a Plan (DAG of sub-tasks) in parallel.
//
// MVP (Phase 4): in-process executor using goroutines + channels with bounded
// parallelism. Cancellation via context.Context.
//
// Phase 8: Temporal adapter behind the same Executor interface.
package dag
