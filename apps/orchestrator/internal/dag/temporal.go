package dag

import (
	"context"
	"errors"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// TemporalExecutor is a Phase 8 stub. It implements the same role as the
// in-process Executor but routes work through Temporal workflows. Behind
// the same Executor interface so the API layer is agnostic.
//
// Phase 8 implementation:
//   - One workflow per run (RunWorkflow), one Activity per node.
//   - The Temporal DAG is built from the plan's edges.
//   - On ctx cancel, Temporal cancels the activity futures.
//
// Phase 4: returns ErrTemporalNotImplemented.
type TemporalExecutor struct {
	Plan *decomposer.Plan
	// Phase 8 will add:
	//   Client    client.Client
	//   TaskQueue string
	//   WorkflowID string
}

// ErrTemporalNotImplemented is the stub error.
var ErrTemporalNotImplemented = errors.New("dag: TemporalExecutor not implemented in Phase 4 — set ExecutorMode=local")

// Run is the Phase 4 stub. Returns ErrTemporalNotImplemented.
func (t *TemporalExecutor) Run(ctx context.Context) error {
	return ErrTemporalNotImplemented
}

// Mode selects which executor to use. "local" (default) uses the
// in-process executor; "temporal" would use the Temporal adapter.
type Mode string

const (
	ModeLocal    Mode = "local"
	ModeTemporal Mode = "temporal"
)

// ParseMode returns the Mode from a string, defaulting to Local on unknown input.
func ParseMode(s string) Mode {
	switch Mode(s) {
	case ModeTemporal:
		return ModeTemporal
	default:
		return ModeLocal
	}
}
