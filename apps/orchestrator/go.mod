module github.com/cogniflow/orchestrator

go 1.25.0

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

require (
	github.com/santhosh-tekuri/jsonschema/v5 v5.3.1
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.44.0
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/trace v1.44.0
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
