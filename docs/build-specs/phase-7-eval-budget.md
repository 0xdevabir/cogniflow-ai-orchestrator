# Phase 7 — Eval + Cost/Carbon Budget

## Goal

Every response gets scored (faithfulness, hallucination, latency, cost, carbon). Users can set a per-conversation `$budget` + `gCO₂` budget; if a request would exceed, the orchestrator **cascade-downgrades** models before running.

**Demo moment:**
1. Set budget `$0.05` in settings. Run a request that would naturally cost `$0.25`. See orchestrator show "Downgraded: Opus→Sonnet→Mixtral" badge before streaming.
2. EvalBadge at the bottom of every answer shows: faithfulness `92%`, hallucination flags `0`, cost `$0.04`, latency `3.2s`, carbon `0.1 gCO₂`, model mix `["claude-3-5-sonnet","mixtral-8x7b","gpt-4o-mini"]`.

## Prerequisites

- ✅ Phase 5 (fusion + citations) + Phase 6 (RAG for faithfulness sources).

## Architecture this phase lays down

```
[BEFORE plan runs]
budget.Estimate(plan) → projected_cost_usd, projected_carbon_g
  if projected > budget:
    loop over nodes:
      for n in plan.nodes:
        if n.assigned_model is "expensive":
          candidates = router.CheaperAlternatives(n)   // see below
          n.assigned_model = candidates[0]
        recompute projection
        if fits: break
    emit SSE event "downgraded" {original, new, savings_usd}

[AFTER plan runs]
eval.Score(answer, manifest, sources) → EvalResult
  faithfulness = LLM-as-judge (cheap model: gpt-4o-mini)
                 → "For each cited claim, verify against the source text"
                 → 0..1 score
  hallucination = claim decomposition + same verifier
                 → count of unsourced claims
  cost    = sum of all ChunkX deltas priced from cost_table
  carbon  = sum from carbon_g_per_million_tokens
  latency = wall-clock from /v1/run start to last chunk

emit SSE event "eval" {faithfulness, hallucination_flags, cost_usd, ...}
```

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/budget/budget.go`
```go
package budget

type Budget struct {
    MaxCostUSD    float64
    MaxCarbonG    float64
}

type Projection struct {
    CostUSD  float64
    CarbonG  float64
}

type Estimator struct {
    Costs *router.CostTable
}

func New(costs *router.CostTable) *Estimator

// PerNode estimates a single node's cost + carbon given an expected token budget.
func (e *Estimator) PerNode(model string, estPromptTokens, estOutputTokens int) Projection

// Total sums projections over a plan given a list of (model, promptTok, outTok).
func (e *Estimator) Total(items []Projection) Projection

// Downgrade cascades expensive models to cheaper candidates.
// Returns: new assignment + total projected savings.
type DowngradeResult struct {
    Original map[string]string  // node_id → original model
    New      map[string]string  // node_id → new (cheaper) model
    SavedUSD float64
    SavedG   float64
}

func (e *Estimator) PlanDowngrade(plan *decomposer.Plan, current map[string]router.Decision, b Budget) (*decomposer.Plan, DowngradeResult)

// CascadeOrder returns models from cheap→expensive per task_class.
var CascadeOrder = map[decomposer.TaskClass][]string{
    ClassReasoning:    {"mixtral:mistral-small", "openai:gpt-4o-mini", "claude-3-5-sonnet", "claude-3-opus"},
    ClassSummarization: {"claude-3-haiku", "openai:gpt-4o-mini", "claude-3-5-sonnet"},
    ...
}
```

#### 2. `internal/budget/budget_test.go`
| Test | What |
|---|---|
| `TestEstimator_PerNode` | GPT-4o, 500/1000 tokens → correct $2.50+$10.00 split. |
| `TestPlanDowngrade_FitsBudget` | Plan with Opus nodes, budget $0.10 → cascades to gpt-4o-mini, fits. |
| `TestPlanDowngrade_NoChangeIfFits` | Plan already cheap, no changes. |

#### 3. Modify `internal/router/router.go`
Add `CheaperAlternatives(taskClass) []string` to the `Router` interface. Implement in `WeightedRouter`:

```go
func (w *WeightedRouter) CheaperAlternatives(ctx context.Context, taskClass decomposer.TaskClass, exclude []string) []router.ModelID {
    // 1. Get all candidates for the task class
    // 2. Sort by estimated cost ASC
    // 3. Exclude already-tried models
    // 4. Return top 3
}
```

#### 4. Modify `internal/dag/executor.go`
- Before running, call `Estimator.PlanDowngrade` if a budget is set (Phase 7 UI passes it via `/v1/run` body).
- Emit `event: downgrade { ... }` on changes.
- Use the (possibly downgraded) plan for the rest of the execution.

#### 5. Modify `cmd/server/main.go`
- Add budget-cascade event to SSE output.

#### 6. `internal/eval/judge.go`
```go
package eval

type Result struct {
    FaithfulnessPct     float64                    `json:"faithfulness_pct"`
    HallucinationFlags  int                        `json:"hallucination_flags"`
    CostUSD             float64                    `json:"cost_usd"`
    CarbonG             float64                    `json:"carbon_g"`
    LatencyP95MS        int                        `json:"latency_p95_ms"`
    LatencyTotalMS      int                        `json:"latency_total_ms"`
    ModelMix            []string                   `json:"model_mix"`
    PerNode             map[string]NodeEval        `json:"per_node"`
    UncitedClaims       []string                   `json:"uncited_claims,omitempty"`
}

type NodeEval struct {
    Model            string
    CostUSD          float64
    LatencyMS        int
    TokensIn         int
    TokensOut        int
    DowngradedFrom   string                  // if budget cascade applied
}

type Judge struct {
    Registry providers.Registry
    Model    string                  // "openai:gpt-4o-mini"
    Costs    *router.CostTable
}

func New(...) *Judge

// Score evaluates an entire run.
func (j *Judge) Score(ctx context.Context, run RunRecord) (*Result, error)

type RunRecord struct {
    Plan        *decomposer.Plan
    Manifest    *citation.Manifest
    Streams     map[string]string            // node_id → final text
    PerNodeUsage []UsageEvent                 // one per node
    StartedAt   time.Time
    EndedAt     time.Time
    Cascade     *budget.DowngradeResult      // if any
}

type UsageEvent struct {
    NodeID        string
    Model         string
    TokensIn      int
    TokensOut     int
    DurationMS    int
}
```

**Faithfulness prompt:**
```
You are the CogniFaith evaluator.

Given:
- the synthesizer's final answer (verbatim),
- the cited spans (each with its source text),
- optional RAG doc snippets,

For each CITED claim in the answer, find the cited span and verify:
  supported   → the cited span text matches the source text
  conflicting → contradicts the source text
  unsourced   → no clear cite

Return JSON:
{
  "faithfulness_pct": 0..1,
  "unsourced_claims": ["...", ...],
  "conflicts": [{ "claim": "...", "span_id": "..." }]
}

Rules:
1. Be strict but fair. Uncertain → "unsourced".
2. Never invent spans. Only evaluate the citations present.
```

#### 7. `internal/eval/eval_test.go`
| Test | What |
|---|---|
| `TestScore_CalculatesCost` | Mock usage 1k output tokens on gpt-4o-mini → expected cost ≈ $0.0006. |
| `TestScore_ParsesJudgeResult` | Mock judge returns valid JSON → Result.FaithfulnessPct populated. |
| `TestScore_FlagsHallucination` | Judge returns 2 unsourced claims → `HallucinationFlags == 2`. |
| `TestScore_PerNodeBreakdown` | 3 nodes of different models → 3 entries in PerNode. |

#### 8. Modify `internal/dag/executor.go`
- Track per-node usage (start/end times + token count) and accumulate.
- After the run completes, call `eval.Judge.Score` and emit `event: eval { ... }`.

#### 9. SSE event: `eval`
```
event: eval
data: {
  "faithfulness_pct": 0.92,
  "hallucination_flags": 0,
  "cost_usd": 0.041,
  "carbon_g": 0.12,
  "latency_p95_ms": 4200,
  "latency_total_ms": 7800,
  "model_mix": ["anthropic:claude-3-5-sonnet-latest","openai:gpt-4o-mini"],
  "per_node": {
    "n1": { "model": "anthropic:...", "cost_usd": 0.012, "latency_ms": 1100, ... },
    ...
  }
}
```

### Web — `apps/web/`

#### 10. `components/EvalBadge.tsx`
Real implementation.

```
┌──────────────────────────────────────────────────────┐
│ 🛡️ Eval                                  [expand ▼] │
├──────────────────────────────────────────────────────┤
│ Faithfulness      ████████████░ 92%                  │
│ Hallucination     0 flagged                          │
│ Cost              $0.041                             │
│ Carbon            0.12 gCO₂                          │
│ Latency           3.2s p95   (7.8s total)            │
│ Model mix         claude-3-5-sonnet, gpt-4o-mini     │
│ 🔻 Downgraded     Opus → Sonnet → Mixtral ($0.18→$0.05) │
└──────────────────────────────────────────────────────┘
```

Click expand → per-node table.

#### 11. `components/BudgetSettings.tsx`
Side panel in `/playground` (or as a popover in the toolbar).

```tsx
<input type="number" step="0.01" placeholder="0.10" />
<label>$ budget per request</label>
<input type="number" step="0.1" placeholder="5.0" />
<label>gCO₂ budget per request</label>
<button>Save</button>
```

Saved to `localStorage`; included in every `/v1/run` body as `budget: { max_cost_usd, max_carbon_g }`.

#### 12. Modify `lib/sse.ts`
Add `EvalEvent` + `DowngradeEvent`:
```ts
export type DowngradeEvent = {
  reason: "budget"
  original: Record<string, string>      // node_id → old model
  new:      Record<string, string>      // node_id → new model
  saved_usd: number
  saved_g: number
}

export type EvalEvent = Result  // matches the Go type
```

#### 13. Modify `app/playground/page.tsx`
- Mount `BudgetSettings` (toggle in top-right).
- Mount `EvalBadge` below the FusionViewer when an eval event arrives.
- Mount a "downgraded" toast when a downgrade event arrives.
- If the eval event has `uncited_claims`, show a yellow ⚠️ strip on the FusionViewer.

### End-to-end verification

1. Restart orchestrator.
2. Settings → set budget `$0.05`.
3. Run a complex prompt that would normally pick Opus. Expected: SSE emits a `downgrade` event, then the plan runs on cheaper models, badge says "Downgraded: $0.18 → $0.05".
4. Run a simple prompt. Expected: no downgrade event, badge shows full faithfulness/cost.
5. **Hallucination case:** ask an obscure question (something the model genuinely doesn't know) → eval event shows ≥1 flag, ⚠️ strip appears.
6. **Carbon budget:** set budget `gCO₂ = 0.05` → request that streams many tokens → cascades.

### Done checklist

- [ ] `Estimator.PerNode` matches the cost_table per-million-token prices.
- [ ] `PlanDowngrade` reduces cost below budget whenever possible.
- [ ] Cascade order per task class is ordered cheap→expensive.
- [ ] Faithfulness judge returns well-formed JSON on successful parses.
- [ ] EvalEvent includes cost, carbon, model mix.
- [ ] EvalBadge in UI shows all metrics.
- [ ] Budget settings persist in localStorage and apply to next runs.
- [ ] Downgrade toast appears when cascade happens.
- [ ] All Go tests pass.
