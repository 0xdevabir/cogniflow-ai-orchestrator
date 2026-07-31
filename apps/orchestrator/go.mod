module github.com/cogniflow/orchestrator

go 1.22

// CogniFlow Orchestrator — Go service.
//
// Architecture:
//   cmd/server          wires HTTP + SSE handlers, builds the DAG executor.
//   internal/api        HTTP/SSE handlers (gin or std-lib http in Phase 1).
//   internal/decomposer LLM → Plan with JSON-schema constrained decoding.
//   internal/router     Weighted heuristic + bandit interface.
//   internal/dag        DAG executor (in-proc goroutines; Temporal adapter later).
//   internal/fusion     Stream merger + judge call.
//   internal/citation   CitationManifest + SpanRef.
//   internal/budget     Cost + carbon gate.
//   internal/eval       LLM-as-judge faithfulness scoring.
//   internal/providers  Vendor-agnostic Streamer + per-provider adapters.
//
// Phases 0–1 just need a hello-world main.go here. Everything else is
// stubbed with package-level comments so the CI green-builds.

require github.com/santhosh-tekuri/jsonschema/v5 v5.3.1 // indirect
