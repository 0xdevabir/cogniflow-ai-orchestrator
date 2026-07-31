# Phase 2 — Prompt Decomposer

## Goal

A complex prompt is decomposed into a Plan (DAG) by an LLM with **JSON-schema-constrained decoding**. The web UI shows the DAG live (via React Flow). No routing yet — every node still routes to the default model.

**Demo moment:** Type *"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy."* → the UI shows a 4-node DAG (researcher, planner, critic, synthesizer) with edges. All nodes `pending` until execution starts in Phase 4.

## Prerequisites

- ✅ Phase 1 complete: chat works, `Streamer` abstraction in place.

## Architecture this phase lays down

```
[User types prompt + clicks "Show Plan"]
   │ POST /v1/plan {prompt}
   ▼
[api/plan.go]
   │ decomposer.Decompose(ctx, prompt)
   ▼
[decomposer/decomposer.go]
   │ build request with response_format=json_schema + system=pkg/prompts/decomposer.v1.md
   ▼ (reuses providers.Streamer interface — single non-streaming call)
[OpenAI / Anthropic w/ structured output]
   │ returns Plan JSON
   ▼
[validate against packages/schemas/plan.schema.json]
   │ OK → return Plan; FAIL → retry up to 3×; then fallback to passthrough plan
   ▼
[return Plan JSON to UI]
```

> **Architectural note:** the decomposer reuses the `Streamer` interface from Phase 1 but treats it as non-streaming — it just reads the final accumulated text after the stream ends. This avoids a separate code path.

## Files to create / modify

### Go — `apps/orchestrator/`

#### 1. `internal/decomposer/types.go`
Plan + Node + Edge types matching `packages/schemas/plan.schema.json`.

```go
package decomposer

type Plan struct {
    Version string `json:"version"`            // "plan.v1"
    Nodes   []Node `json:"nodes"`
    Edges   []Edge `json:"edges"`
}

type Role string

const (
    RoleResearcher  Role = "researcher"
    RolePlanner     Role = "planner"
    RoleCoder       Role = "coder"
    RoleSummarizer  Role = "summarizer"
    RoleCritic      Role = "critic"
    RoleSynthesizer Role = "synthesizer"
    RoleFactchecker Role = "factchecker"
    RoleTranslator  Role = "translator"
)

type TaskClass string

const (
    ClassReasoning     TaskClass = "reasoning"
    ClassSummarization TaskClass = "summarization"
    ClassCreative      TaskClass = "creative"
    ClassFactual       TaskClass = "factual"
    ClassCode          TaskClass = "code"
    ClassTranslation   TaskClass = "translation"
    ClassVision        TaskClass = "vision"
    ClassRouting       TaskClass = "routing"
)

type Modality string

const (
    ModalityText  Modality = "text"
    ModalityImage Modality = "image"
    ModalityAudio Modality = "audio"
    ModalityCode  Modality = "code"
)

type Requirements struct {
    TaskClass        TaskClass `json:"task_class"`
    Modality         Modality  `json:"modality"`
    LatencyBudgetMS  int       `json:"latency_budget_ms"`
    MaxCostUSD       float64   `json:"max_cost_usd"`
}

type Node struct {
    ID        string       `json:"id"`
    Role      Role         `json:"role"`
    Payload   string       `json:"payload"`
    DependsOn []string     `json:"depends_on"`
    NeedsRAG  bool         `json:"needs_rag"`
    Requires  Requirements `json:"requires"`
}

type Edge struct {
    From string `json:"from"`
    To   string `json:"to"`
}
```

#### 2. `internal/decomposer/prompts.go`
Embed the versioned prompt.

```go
package decomposer

import _ "embed"

//go:embed prompts/decomposer.v1.md
var decomposerPromptV1 string

// CurrentPrompt returns the active decomposer prompt.
func CurrentPrompt() string { return decomposerPromptV1 }
```

Then copy `packages/prompts/decomposer.v1.md` into `apps/orchestrator/internal/decomposer/prompts/decomposer.v1.md` (and `judge.v1.md` for Phase 5, `router_meta.v1.md` for Phase 8). **Don't import across module boundaries** — Go's embed needs the file inside the package.

#### 3. `internal/decomposer/schemas.go`
Embed the JSON schema files and expose a `SchemaJSON()` function.

```go
//go:embed schemas/plan.schema.json
var planSchemaJSON []byte

func PlanSchemaJSON() []byte { return planSchemaJSON }
```

Copy `packages/schemas/plan.schema.json` into `apps/orchestrator/internal/decomposer/schemas/plan.schema.json`.

#### 4. `internal/decomposer/decomposer.go`
Main decomposer logic.

```go
package decomposer

type Deps struct {
    Registry providers.Registry
    Model    string                            // "openai:gpt-4o", "anthropic:claude-3-5-sonnet-latest"
    Timeout  time.Duration                     // default 30s
    Retries  int                               // default 3
}

type Decomposer struct {
    d Deps
}

func New(d Deps) *Decomposer { ... }

// Decompose prompts an LLM to return a Plan. Validates against schema.
// On failure, retries up to d.Retries times. Falls back to PassthroughPlan.
func (x *Decomposer) Decompose(ctx context.Context, prompt string) (*Plan, error) { ... }

// PassthroughPlan returns a single-node plan that just answers the prompt.
func PassthroughPlan(prompt string) *Plan {
    return &Plan{
        Version: "plan.v1",
        Nodes: []Node{{
            ID: "n1", Role: RoleSynthesizer,
            Payload: prompt,
            DependsOn: []string{},
            Requires: Requirements{
                TaskClass: ClassReasoning, Modality: ModalityText,
                LatencyBudgetMS: 20000, MaxCostUSD: 0.20,
            },
        }},
        Edges: []Edge{},
    }
}

// Validate runs plan.schema.json validation on p.
func Validate(p *Plan) error { ... }
```

**Implementation details:**
- Reuse `providers.Streamer.Stream` with `Request.MaxTokens = 4096` and zero temperature.
- For OpenAI, post-process the final aggregated text: strip ```json fences if present, parse, validate.
- For Anthropic, request a tool-call with the JSON schema as input_schema (Phase 5+ uses this too).
- **Validation:** use `github.com/santhosh-tekuri/jsonschema/v5` (or `xeipuuv/gojsonschema`). Compile the schema once at startup; reuse.
- **Retry policy:**
  - Schema invalid → re-call LLM with the validation error message appended to the system prompt.
  - After `Retries` failures → return `PassthroughPlan(prompt)`, nil error.
- **Concurrency:** single-threaded. Cost is bounded; no need for goroutines here.

#### 5. `internal/decomposer/decomposer_test.go`
- `TestValidate_OK`: worked example from `decomposer.v1.md` validates.
- `TestValidate_RejectsBadRole`: `role: "hacker"` rejected.
- `TestPassthroughPlan_Validates`: single-node passthrough validates.
- `TestDecompose_RetryOnInvalidSchema`: mock provider returns invalid JSON twice, valid JSON once → succeeds.
- `TestDecompose_FallbackOnExhaustedRetries`: mock always returns invalid → returns passthrough.

Use a `mockStreamer` that returns predetermined text from `Request` → `Chunk{Text: "..."}`.

#### 6. `internal/api/plan.go`
Debug endpoint.

```go
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
    // 1. Decode {prompt}
    // 2. s.Decomposer.Decompose(ctx, prompt)
    // 3. Marshal Plan to JSON (with indentation)
    // 4. If ?stream=1, set up SSE that emits:
    //      event: node_status {node: n1, status: pending}
    //      ... (Phase 4 replaces this with real statuses)
    //      event: done
}
```

Wire into `cmd/server/main.go`:
```go
mux.HandleFunc("/v1/plan", s.handlePlan)
```

### Web — `apps/web/`

#### 7. `components/DAGCanvas.tsx`
Real implementation. Phase 0 was a placeholder.

**Required:**
```tsx
import { ReactFlow, Background, Controls, MarkerType } from "@xyflow/react"
import dagre from "dagre"
import "@xyflow/react/dist/style.css"

export type PlanNode = {
  id: string
  role: string
  payload: string
  status: "pending" | "running" | "ok" | "error" | "debating"
  model?: string
}

export type PlanEdge = { from: string; to: string }

export function DAGCanvas({ nodes, edges }: { nodes: PlanNode[]; edges: PlanEdge[] }) {
  // 1. Layout with dagre (rankdir=LR, nodesep=80, ranksep=120)
  // 2. Color nodes by status: pending=gray, running=blue(pulsing), ok=green, error=red, debating=orange
  // 3. Animated edge stroke between running→ok edges
  // 4. Each node card shows: id, role, first 60 chars of payload, model if set, status pill
}
```

Add dependencies in `apps/web/package.json`:
- `@xyflow/react@^12`
- `dagre@^0.8`
- `lib/sse.ts`: add a `streamPlan(prompt)` generator mirroring `streamChat` but pointing at `/v1/plan?stream=1`.

#### 8. `app/playground/page.tsx` (modify)
Add a "Show Plan" button next to "Send":
- Click "Show Plan" → `POST /v1/plan {prompt}` → receive Plan → mount `<DAGCanvas nodes={...} edges={...} />`.
- Click "Send" → same Phase 1 behavior (single-node passthrough for now).

**Demo flow:** user types a complex prompt → clicks "Show Plan" → sees the DAG light up immediately. Clicking "Send" actually executes the dag in Phase 4+.

### Tests to write

| Test | File | What it checks |
|---|---|---|
| `TestValidate_OK` | `decomposer_test.go` | The decomposer's worked example passes validation. |
| `TestValidate_RejectsBadRole` | `decomposer_test.go` | A node with `role: "hacker"` fails. |
| `TestPassthroughPlan_Validates` | `decomposer_test.go` | `PassthroughPlan()` returns a valid plan. |
| `TestDecompose_RetryOnInvalidJSON` | `decomposer_test.go` | Mock returns garbage twice, valid JSON once. Expect `Plan` returned, retries=3. |
| `TestDecompose_FallbackOnExhaustion` | `decomposer_test.go` | Mock always returns garbage. Expect `PassthroughPlan`. |
| `TestAPI_PlanEndpoint_OK` | `api/plan_test.go` | `POST /v1/plan` returns valid Plan JSON. |
| `TestAPI_PlanEndpoint_BadJSONBody` | `api/plan_test.go` | Missing field returns 400 with error. |

### End-to-end verification

1. Restart orchestrator with `OPENAI_API_KEY` set.
2. Open `http://localhost:3000/playground`.
3. Type:
   > *"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy."*
4. Click **Show Plan**.
5. **Expected:** canvas appears with 4 nodes (researcher, planner, critic, synthesizer) connected with edges. Synthesizer is at the right with no outgoing edges. All nodes show `pending`.
6. **Inspect the Plan JSON** at `localhost:8080/v1/plan?` curl:
   ```bash
   curl -X POST localhost:8080/v1/plan \
     -H 'content-type: application/json' \
     -d '{"prompt":"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple'"'"'s 2024 strategy."}'
   ```
7. The returned JSON validates against `packages/schemas/plan.schema.json`.

### Done checklist

- [ ] `Plan`, `Node`, `Edge` types compile and round-trip JSON.
- [ ] `Decompose()` validates against `plan.schema.json`.
- [ ] Retry-on-invalid + fallback-to-passthrough both tested.
- [ ] `POST /v1/plan` returns 200 + valid Plan for any prompt.
- [ ] `DAGCanvas.tsx` renders 1–6 nodes with dagre layout, status-colored.
- [ ] Worked example from the prompt validates unchanged.
- [ ] All Go tests pass.
- [ ] `Show Plan` button appears next to `Send` in `playground/page.tsx`.
