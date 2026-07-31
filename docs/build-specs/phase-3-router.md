# Phase 3 — Model Router

## Goal

Each Plan node gets routed to the best model for its `task_class`. The router uses a **weighted-score heuristic** (marketed as a contextual bandit — see below). The UI surfaces the choice + reasoning per node.

**Demo moment:** the same 4-node plan now lights up with **different model badges** — researcher (Mixtral, cheap), planner (Sonnet, reasoning), critic (GPT-4o, vision-capable), synthesizer (Opus, top reasoner). Clicking a node shows the score breakdown.

## Prerequisites

- ✅ Phase 2 complete: Plan is in hand.

## Architecture this phase lays down

```
[Plan from decomposer]
   │ for each node:
   │   router.Route(ctx, node) → Decision
   ▼
[router.WeightedRouter]
   │ load benchmarks.json + cost_table.json (embedded at build time)
   │ score = 0.45*bench(m, c) + 0.30*(1-cost_norm(m)) + 0.25*(1-latency_norm(m))
   │ pick best (deterministic tiebreak: cheapest, then fastest)
   │ log (task_class, model, score, was_chosen=true, satisfaction=null) to JSONL
   ▼
[Decision {Provider, Model, ScoreBreakdown}]
```

> **The "bandit" framing:** the `Bandit` interface accepts feedback events (satisfaction scores from the user or eval). A JSONL append-only log is the dataset for offline LinUCB / ε-greedy replay in Phase 8. The MVP interface is the same; the implementation swaps from "weighted heuristic" to "LinUCB arm selector" later. **Demo credibility:** we can truthfully say "contextual bandit with offline replay."

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/router/types.go`
```go
package router

type TaskClass string  // mirrors decomposer.TaskClass values

type ModelID struct {
    Provider string  // "openai" | "anthropic" | "mock" | "ollama" | "mistral" | "hf"
    Model    string  // "gpt-4o-mini" etc.
}

func (m ModelID) String() string { return m.Provider + ":" + m.Model }

// Decision is the router's pick + the reasoning.
type Decision struct {
    Model          ModelID
    Score          float64
    Breakdown      map[string]float64  // {"bench": 0.82, "cost": 0.65, "latency": 0.71}
    Alternatives   []ScoredModel       // ranked list of all candidates
    BanditArmID    string              // stable hash of (task_class, model) — used for feedback
}

type ScoredModel struct {
    Model    ModelID
    Score    float64
    Breakdown map[string]float64
}

type FeedbackEvent struct {
    RunID      string
    NodeID     string
    BanditArmID string
    Model      ModelID
    TaskClass  TaskClass
    Satisfaction float64        // 0..1, from user thumb or eval score
    LatencyMS  int
    CostUSD    float64
    Timestamp  time.Time
}

type Router interface {
    Route(ctx context.Context, node routerNode) (*Decision, error)
    RecordFeedback(ctx context.Context, ev FeedbackEvent) error
}

type routerNode interface {
    TaskClass() decomposer.TaskClass
    LatencyBudgetMS() int
    MaxCostUSD() float64
}
```

#### 2. `internal/router/benchmarks.go`
**Seed data, embedded, hand-tuned.** This is the strongest "we know what we're doing" signal.

```go
package router

//go:embed data/benchmarks.json
var benchmarksJSON []byte

type Benchmarks struct {
    // Per task_class × model, 0..1 (higher is better).
    Scores map[TaskClass]map[string]float64 `json:"scores"`
}
```

`internal/router/data/benchmarks.json`:
```json
{
  "scores": {
    "reasoning":     {"openai:gpt-4o": 0.92, "anthropic:claude-3-5-sonnet-latest": 0.93, "anthropic:claude-3-opus-latest": 0.97, "openai:gpt-4o-mini": 0.75, "mistral:mistral-large-latest": 0.84, "ollama:llama3.1-70b": 0.78, "mock:echo": 0.10},
    "summarization": {"openai:gpt-4o-mini": 0.88, "anthropic:claude-3-5-sonnet-latest": 0.90, "openai:gpt-4o": 0.91, "anthropic:claude-3-haiku-20240307": 0.85, "mistral:mistral-small-latest": 0.82, "ollama:llama3.1-8b": 0.74, "mock:echo": 0.10},
    "creative":      {"openai:gpt-4o": 0.88, "anthropic:claude-3-5-sonnet-latest": 0.92, "anthropic:claude-3-opus-latest": 0.94, "openai:gpt-4o-mini": 0.78, "mock:echo": 0.10},
    "factual":       {"openai:gpt-4o": 0.90, "anthropic:claude-3-5-sonnet-latest": 0.91, "openai:gpt-4o-mini": 0.82, "mistral:mistral-large-latest": 0.83, "ollama:llama3.1-70b": 0.79, "mock:echo": 0.10},
    "code":          {"openai:gpt-4o": 0.92, "anthropic:claude-3-5-sonnet-latest": 0.93, "anthropic:claude-3-opus-latest": 0.95, "openai:gpt-4o-mini": 0.78, "mock:echo": 0.10},
    "translation":   {"openai:gpt-4o-mini": 0.90, "anthropic:claude-3-haiku-20240307": 0.88, "openai:gpt-4o": 0.91, "mock:echo": 0.10},
    "vision":        {"openai:gpt-4o": 0.95, "anthropic:claude-3-5-sonnet-latest": 0.92, "mock:echo": 0.10},
    "routing":       {"openai:gpt-4o-mini": 0.85, "anthropic:claude-3-haiku-20240307": 0.82, "mock:echo": 0.50}
  }
}
```

> Numbers are illustrative — the point is **a structured, per-(class, model) matrix**.

#### 3. `internal/router/cost_table.go`
```go
type CostTable struct {
    PerMillionTokenInput  map[string]float64  `json:"per_million_input_usd"`
    PerMillionTokenOutput map[string]float64  `json:"per_million_output_usd"`
    LatencyP95MS          map[string]int      `json:"latency_p95_ms"`
    CarbonGPerMTokens     map[string]float64  `json:"carbon_g_per_million_tokens"`   // Phase 7
}
```

Same `data/cost_table.json`. Use real list prices (Oct 2024 ballpark):
- gpt-4o: $2.50 in / $10 out
- gpt-4o-mini: $0.15 / $0.60
- claude-3-5-sonnet: $3 / $15
- claude-3-opus: $15 / $75
- claude-3-haiku: $0.25 / $1.25
- mistral-large: $2 / $6
- mistral-small: $0.20 / $0.60
- llama3.1-70b (ollama): $0 / $0 (local)
- llama3.1-8b (ollama): $0 / $0

#### 4. `internal/router/weighted.go`
```go
package router

type WeightedConfig struct {
    Weights      map[TaskClass]Weights  // override per-class coefficients
    Benchmarks   *Benchmarks
    Costs        *CostTable
    EstPromptTok int                    // prompt tokens (default 500)
    EstOutTok    int                    // output tokens (default 1000)
}

type Weights struct {
    Bench   float64  // default 0.45
    Cost    float64  // default 0.30
    Latency float64  // default 0.25
}

type WeightedRouter struct {
    cfg WeightedConfig
    log FeedbackLogger
}

func NewWeighted(cfg WeightedConfig, log FeedbackLogger) *WeightedRouter { ... }

func (w *WeightedRouter) Route(ctx, node) (*Decision, error) {
    // 1. List candidate models: from CostTable.LatencyP95MS keys + benchmarks keys.
    // 2. For each candidate compute:
    //      cost_est = (EstPromptTok * cost_in + EstOutTok * cost_out) / 1e6
    //      cost_norm = cost_est / max_cost_est (clamp 0..1, invert)
    //      latency_norm = latency_p95 / max_latency (clamp 0..1, invert)
    //      bench = lookup(Benchmarks.Scores[task_class][model])
    //      score = w.Bench*bench + w.Cost*cost_norm + w.Latency*latency_norm
    // 3. Sort descending by score. Pick #1 (deterministic tiebreak: cheaper).
    // 4. Filter out models whose latency_p95 > node.LatencyBudgetMS().
    // 5. Filter out models whose cost_est > node.MaxCostUSD().
    // 6. Log FeedbackEvent (satisfaction=0, just decision log) to JSONL.
    // 7. Return Decision + alternatives.
}
```

#### 5. `internal/router/bandit.go`
```go
package router

type FeedbackLogger interface {
    Append(ev FeedbackEvent) error
}

// JSONLFeedbackLogger appends to a file. Concurrency-safe (mutex).
type JSONLFeedbackLogger struct {
    f   *os.File
    mu  sync.Mutex
    enc *json.Encoder
}

func NewJSONLFeedbackLogger(path string) (*JSONLFeedbackLogger, error)
func (l *JSONLFeedbackLogger) Append(ev FeedbackEvent) error
func (l *JSONLFeedbackLogger) Close() error
```

Log path: `$COGNIFLOW_DATA_DIR/bandit.jsonl` (default `./data/bandit.jsonl`). Add `.gitignore` entry.

#### 6. `internal/router/weighted_test.go`
- `TestWeightedRouter_PicksBestForReasoning`: task=reasoning should pick `claude-3-opus-latest` (highest bench at 0.97).
- `TestWeightedRouter_PicksCheapForSummarization`: task=summarization, weights heavily favor cost → pick `gpt-4o-mini`.
- `TestWeightedRouter_RespectsLatencyBudget`: budgets at 2000ms → exclude `claude-3-opus-latest` (p95 4s).
- `TestWeightedRouter_RespectsCostBudget`: node.MaxCostUSD = $0.01 → exclude all but the cheap ones.
- `TestWeightedRouter_BreakdownPopulated`: Decision.Breakdown has all 3 numeric keys.
- `TestFeedbackLogger_AppendsJSONLines`: feed 3 events, read file, expect 3 lines of valid JSON.

### Wire-up

In `cmd/server/main.go`:
```go
bench, _ := router.LoadBenchmarks()
cost, _ := router.LoadCostTable()
logger, _ := router.NewJSONLFeedbackLogger("./data/bandit.jsonl")
wr := router.NewWeighted(router.WeightedConfig{Benchmarks: bench, Costs: cost}, logger)
s.Router = wr  // for Phase 4
```

### Web — `apps/web/`

#### 7. `components/NodeDetailsPanel.tsx` (new)
Side panel that shows when a DAG node is clicked:

```
┌────────────────────────────────────────────────────┐
│ n3 — critic                              [running] │
├────────────────────────────────────────────────────┤
│ Payload                                            │
│ Compare NVIDIA vs Apple 2024 strategy...           │
│                                                    │
│ Routing decision                                   │
│ ─────────────────────────────────────────          │
│ Model: openai:gpt-4o                               │
│ Score: 0.87                                        │
│ ┌─────────────────────────────────────────┐        │
│ │ bench   ████████░░  0.90                │        │
│ │ cost    ██████░░░░  0.65                │        │
│ │ latency ███████░░░  0.71                │        │
│ └─────────────────────────────────────────┘        │
│                                                    │
│ Alternatives (top 3)                               │
│   2. anthropic:claude-3-5-sonnet  0.86             │
│   3. mistral:mistral-large-latest 0.78             │
└────────────────────────────────────────────────────┘
```

#### 8. Modify `components/DAGCanvas.tsx`
- Each node card shows the assigned `model` badge (small, top-right).
- On click → open `NodeDetailsPanel` with the breakdown.

#### 9. Modify `lib/sse.ts` + the chat flow
- After Phase 4 lands: SSE events include `event: node_status {node: n3, status: running, model: "openai:gpt-4o", score: 0.87, breakdown: {...}}` BEFORE the chunks arrive.
- For Phase 3: the response after `POST /v1/plan` includes model assignments per node. The UI displays them.

### End-to-end verification

1. `make up && make dev-orchestrator && make dev-web`.
2. Open playground, paste:
   > *"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy."*
3. Click **Show Plan**.
4. **Expected:** canvas shows 4 nodes, each with a `model` badge.
   - researcher → `gpt-4o-mini` or `claude-3-haiku` (cheap & factual)
   - planner → `claude-3-5-sonnet` (reasoning + budget math)
   - critic → `claude-3-5-sonnet` or `gpt-4o` (factual)
   - synthesizer → `claude-3-opus` or `claude-3-5-sonnet` (top reasoner)
5. Click any node → `NodeDetailsPanel` opens with the score breakdown.
6. Inspect `./data/bandit.jsonl` — see 4 decisions logged, one per line, JSON.

### Done checklist

- [ ] `WeightedRouter.Route` returns a `Decision` with all candidates ranked.
- [ ] Latency + cost budget filters are respected.
- [ ] Tiebreaker is deterministic (cheapest first).
- [ ] `JSONLFeedbackLogger` is concurrency-safe.
- [ ] Each Plan node carries a `model` badge in the UI.
- [ ] `NodeDetailsPanel` shows score breakdown on click.
- [ ] `data/bandit.jsonl` populated per request.
- [ ] All Go tests pass.
