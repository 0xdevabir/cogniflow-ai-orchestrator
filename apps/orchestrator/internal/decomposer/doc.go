// Package decomposer turns a user prompt into a Plan (a DAG of sub-tasks).
//
// Implementation outline (Phase 2):
//
//  1. Read packages/prompts/decomposer.v1.md (embedded as a Go string).
//  2. Send the prompt to an LLM with response_format = plan.schema.json.
//  3. Validate the returned JSON against the schema. On failure, retry up to 3×.
//  4. Return a *plan.Plan or a single-node "passthrough" fallback.
package decomposer
