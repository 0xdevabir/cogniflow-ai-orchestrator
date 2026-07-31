# Phase 4 — DAG Executor + Parallel Sub-Tasks

## Goal

Run the Plan: every node executes in parallel (respecting `depends_on`), streams tokens live, and updates UI status (`pending → running → ok/error`). The `DAGCanvas` lights up as work happens.

**Demo moment:** click **Run** on the 4-node tokyo plan → all nodes transition from `pending` to `running` near-simultaneously, each `StreamPanel` row starts filling with tokens, nodes turn green when done. Final synthesizer node has incoming edges from n2 + n3 → it can't start until both finish.

## Prerequisites

- ✅ Phase 2 (decomposer) + Phase 3 (router).
- `Streamer` interface + `Decision` types in hand.

## Architecture this phase lays down

```
[ui clicks "Run"]
   │ POST /v1/run {plan, conversation_id}
   ▼
[api/run.go]
   │ dag.NewExecutor(plan).Run(ctx, sink)
   ▼
[internal/dag/executor.go]
   │
   │ 1. Topological sort → levels
   │ 2. For each level:
   │      spawn N goroutines (bounded by parallelism = 4)
   │      each goroutine:
   │          call router.Route(node) → Decision
   │          emit SSE event "node_status" {node, status:"running", model}
   │          call providers.Stream(ctx, req, sink)
   │          as chunks arrive, emit "chunk" events tagged with streamID=node.id
   │          emit "node_status" {node, status:"ok"}
   │ 3. If any node fails, mark it "error", cancel downstream via ctx.
   │ 4. Synthesizer runs last. Its stream becomes the final fused stream.
   ▼
[UI: DAGCanvas + StreamPanel both render from same SSE feed]
```

> **Synthesizer stream ≠ fusion yet.** In Phase 4 the synthesizer's output is the "final answer" but it doesn't yet merge the upstream streams. Phase 5 adds real fusion. So in Phase 4, if the synthesizer needs upstream content, pass it via the system prompt (concatenated upstream text).

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/dag/topology.go`
```go
package dag

// TopoSort returns node ids in execution levels. len(levels) == max depth + 1.
// levels[i] can run in parallel; levels[i+1] depends only on levels[i].
func TopoSort(nodes []decomposer.Node, edges []decomposer.Edge) ([][]string, error)

// Validate returns an error if there is a cycle, a dangling edge, or a node with
// an unknown dependency.
func Validate(nodes []decomposer.Node, edges []decomposer.Edge) error

// Roots returns nodes with no dependencies.
func Roots(nodes []decomposer.Node) []string
```

#### 2. `internal/dag/executor.go`
The in-process executor.

```go
package dag

type Sink interface {
    Emit(event string, data any) error
}

type Executor struct {
    Plan       *decomposer.Plan
    Router     router.Router
    Providers  providers.Registry
    Sink       Sink                 // SSE-bound in production; test sink in unit tests
    Parallelism int                 // default 4
}

func New(p *decomposer.Plan, r router.Router, reg providers.Registry, sink Sink) *Executor

// Run executes the plan to completion. Returns error only on catastrophic failure;
// per-node errors are propagated as "node_status: error" events.
func (e *Executor) Run(ctx context.Context) error
```

**Implementation:**

```go
func (e *Executor) Run(ctx context.Context) error {
    levels, err := TopoSort(e.Plan.Nodes, e.Plan.Edges)
    if err != nil { return err }

    var wg sync.WaitGroup
    sem := make(chan struct{}, e.Parallelism)

    // Map node id → aggregated text (so the synthesizer can read upstream content).
    outputs := sync.Map{}

    for _, level := range levels {
        for _, id := range level {
            wg.Add(1)
            sem <- struct{}{}
            go func(nodeID string) {
                defer wg.Done()
                defer func() { <-sem }()
                e.runNode(ctx, nodeID, &outputs)
            }(id)
        }
        wg.Wait()   // synchronize level-by-level
    }
    return nil
}

func (e *Executor) runNode(ctx context.Context, id string, outs *sync.Map) {
    // 1. Find node by id
    // 2. For each dep in node.DependsOn, fetch output text from outs and concatenate
    //    as "===UPSTREAM: {dep.role} ===\n{text}\n" and prepend to the prompt.
    // 3. Route
    // 4. Emit "node_status" {node:id, status:"running", model}
    // 5. Call providers.Stream into a sinkAdapter that emits "chunk" SSE events
    //    AND accumulates text into outs.Store(id, ...)
    // 6. On error, emit "node_status" {status:"error", message}
    // 7. On Finish, emit "node_status" {status:"ok"}
}
```

**Critical details:**
- `ctx` cancellation propagates: if a budget overrun cancels (Phase 7), all in-flight goroutines stop.
- A node's status pill is one of: `pending | running | ok | error | debating`.
- The chunk sink adapter per-node prepends `data: {"node_id": id, ...}` to every chunk so the UI can bucket them.

#### 3. `internal/dag/executor_test.go`
Use a `mockSink` and `mockRegistry` for tests:

| Test | What |
|---|---|
| `TestTopoSort_LinearPlan` | A→B→C plan returns `[[A],[B],[C]]`. |
| `TestTopoSort_DiamondPlan` | A→{B,C}→D returns `[[A],[B,C],[D]]`. |
| `TestValidate_DetectsCycle` | A→B→A errors. |
| `TestValidate_DanglingEdge` | node references unknown dep errors. |
| `TestExecutor_RunsAllNodes` | 4-node plan; all nodes emit "ok" event. |
| `TestExecutor_RespectsParallelism` | Force parallelism=2, 4 nodes all run, level-by-level sync holds. |
| `TestExecutor_PropagatesUpstreamText` | Node B has dep A; check B's prompt in mock contains "===UPSTREAM: A===\n...". |
| `TestExecutor_StopsOnCtxCancel` | Cancel mid-flight; second-level nodes don't run. |

#### 4. `internal/dag/temporal.go` (stub)
```go
package dag

// TemporalExecutor is a Phase 8 stub. Same interface.
type TemporalExecutor struct {
    Plan *decomposer.Plan
    // Temporal client + workflow defs in Phase 8.
}

func (t *TemporalExecutor) Run(ctx context.Context) error {
    return errors.New("TemporalExecutor not implemented in Phase 4 — switch via ExecutorMode")
}
```

`ExecutorMode` env var: `"local"` (default) | `"temporal"`.

#### 5. `internal/api/run.go`
```go
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
    // 1. Decode {plan: <Plan>, conversation_id?}
    // 2. Validate plan.
    // 3. Set SSE headers.
    // 4. Build executor + sink adapter.
    // 5. Run in a goroutine; main goroutine flushes after each event.
    // 6. On done: emit "event: done"
    // 7. On ctx cancel: emit "event: cancelled"
}
```

SSE event schema:
```
event: node_status
data: {"node_id":"n1","status":"running","model":"anthropic:claude-3-5-sonnet-latest","score":0.87}

event: chunk
data: {"v":"chunk.v1","stream_id":"n1","text":"The CAP theorem..."}

event: node_status
data: {"node_id":"n1","status":"ok"}

...

event: done
data: {"ok":true,"total_nodes":4}
```

### Wire-up

In `cmd/server/main.go`:
```go
mux.HandleFunc("/v1/run", s.handleRun)
```

### Web — `apps/web/`

#### 6. `lib/sse.ts` (extend)
```ts
export type NodeStatusEvent = {
  node_id: string
  status: "pending" | "running" | "ok" | "error" | "debating"
  model?: string
  score?: number
  breakdown?: Record<string, number>
  message?: string
  alternatives?: { model: string; score: number }[]
}

export type SSEEvent =
  | { event: "node_status"; data: NodeStatusEvent }
  | { event: "chunk";       data: Chunk }
  | { event: "done";        data: { ok: boolean } }
  | { event: "error";       data: { message: string } }

export async function* streamRun(plan: Plan, opts?: {...}): AsyncGenerator<SSEEvent>
```

#### 7. `components/DAGCanvas.tsx` (extend)
- Status state per node id (Map<id, NodeStatusEvent>).
- Subscribe to SSE; on `node_status` → update the node's color:
  - `pending`: gray
  - `running`: blue, pulsing border (CSS keyframe)
  - `ok`: green
  - `error`: red
  - `debating`: orange (Phase 5)
- Animate edges: stroke becomes solid + colored green when both endpoints are `ok`.

#### 8. `components/StreamPanel.tsx` (extend)
- Bucket by `stream_id` (= `node_id`).
- Render one row per `stream_id`.
- Each row shows: model badge, status pill, live token stream (current Phase 1 panel).
- Add an overall progress bar (4/4 nodes done).

#### 9. `app/playground/page.tsx`
Add a third button: **Run** (next to Show Plan / Send).
- Show Plan: just call `/v1/plan`, mount DAGCanvas (Phase 2/3 behavior).
- Run: call `/v1/run`, stream everything into DAGCanvas + StreamPanel.
- Send: Phase 1 single-model behavior, kept for quick replies.

### End-to-end verification

1. With both `OPENAI_API_KEY` + `ANTHROPIC_API_KEY` set, restart orchestrator.
2. Open playground, paste the tokyo prompt.
3. Click **Show Plan** → DAG appears with model badges.
4. Click **Run** → all 4 nodes transition to `running`, edges animate, stream panel fills row-by-row.
5. Synthesizer (n4) does not start until n2 + n3 complete.
6. **Expected end state:** all 4 nodes `ok`, stream panel shows the final fused-by-string synthesis (Phase 4 synthesizer is a single LLM call, NOT a true fusion yet — that lands in Phase 5).

**curl:**
```bash
curl -N -X POST localhost:8080/v1/run \
  -H 'content-type: application/json' \
  -d @<(curl -s -X POST localhost:8080/v1/plan \
        -H 'content-type: application/json' \
        -d '{"prompt":"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA vs Apple 2024 strategy."}')
```

Watch the stream: nodes go `running` → `ok` in level order.

### Done checklist

- [ ] TopoSort handles cycles, diamonds, single-node plans.
- [ ] Executor runs nodes in parallel (bounded), level-by-level.
- [ ] Upstream outputs injected into downstream prompts.
- [ ] ctx cancellation stops in-flight + skips queued nodes.
- [ ] All node statuses emit as SSE events.
- [ ] `DAGCanvas` transitions colors in real time.
- [ ] `StreamPanel` buckets by `node_id`.
- [ ] Temporal executor stub exists behind a mode flag.
- [ ] All Go tests pass.
