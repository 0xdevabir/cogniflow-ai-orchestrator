// Package api hosts HTTP + SSE handlers.
//
// Endpoints (added per phase):
//   GET  /healthz                    (Phase 0)
//   POST /v1/chat  → SSE             (Phase 1)
//   POST /v1/plan  → JSON            (Phase 2, debug)
//   POST /v1/docs/upload  → JSON     (Phase 6)
package api
