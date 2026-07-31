# Phase 8 — Production Polish (Cherry-Pick Menu)

> **All items are independent.** Pick whichever you have time for. None of them are required to ship a working product — Phases 0–7 already do that.

## Menu

| Item | Effort | Standalone? | Resume value |
|---|---|---|---|
| Temporal durable DAG | 1 day | ✅ | 🔥🔥🔥 |
| OpenTelemetry + Langfuse | ½ day | ✅ | 🔥🔥 |
| Self-improving bandit loop | ½ day | ✅ | 🔥🔥🔥 |
| Neo4j entity graph | 1 day | needs Phase 6 | 🔥🔥 |
| Stripe metering | ½ day | ✅ | 🔥 |
| Helm chart for K8s | ½ day | ✅ | 🔥🔥 |
| TruLens + DeepEval harness | 1 day | ✅ | 🔥🔥 |
| Doc the demo script + screen-record | ½ day | ✅ | 🔥🔥🔥 |

## 8.1 — Temporal durable DAG (1 day)

**Why:** Temporal gives you free retries, durable timers, signal handling, and a built-in workflow UI at `localhost:8088`. The whole DAG visualization story goes from "we built it" to "the platform powers it."

**Implementation:**

Move the existing `internal/dag/executor.go` behind an interface:
```go
type Executor interface {
    Run(ctx context.Context, plan *decomposer.Plan, sink Sink) error
}
```

Then `TemporalExecutor` implements it by:
- One Workflow: `OrchestratorWorkflow(plan, sink)` returns `error`.
- One Activity per sub-task: `RunSubTaskActivity(node) → finalText + UsageEvent`.
- Workflow reads the Plan from input; spawns child workflows per level; signals the sink via Temporal's `GetSignalChannel` to emit events.
- For the sink to receive events, route Temporal signals → a Go channel → SSE.

**Files:**
- `apps/orchestrator/internal/dag/temporal.go` — real impl (replace the Phase 4 stub).
- `apps/orchestrator/internal/dag/executor.go` — interface extraction.
- `docker-compose.yml` — uncomment Temporal.
- `apps/orchestrator/cmd/server/main.go` — switch on `EXECUTOR_MODE` env.

**Demo moment:** Temporal Web UI at `localhost:8088` shows the live DAG with timing per node. Resume gold.

## 8.2 — OpenTelemetry + Langfuse (½ day)

**Why:** traces let you SEE the orchestrator think. Langfuse is a turn-key LLM-observability dashboard.

**Implementation:**
- `internal/observability/tracing.go`: init `otel` tracer.
- Wrap every provider call in a span tagged with `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, etc.
- Spans: `decomposer.decompose`, `router.route`, `provider.stream`, `fusion.fuse`, `eval.score`.
- Langfuse SDK auto-uploads spans; they appear at `cloud.langfuse.com`.

**Files:**
- `apps/orchestrator/internal/observability/otel.go`
- `apps/orchestrator/internal/observability/langfuse.go` (or HTTP exporter)
- `docker-compose.yml` — add `langfuse/langfuse` image for local UI.

## 8.3 — Self-improving bandit loop (½ day)

**Why:** the README promises it; this makes the promise real. Every routing decision logs to `data/bandit.jsonl`. A nightly job (cron or in-process goroutine) reads the log, computes per-(task_class, model) satisfaction, and writes a new `cost_table_effective.json` that re-weights the router.

**Implementation:**
- `internal/router/bandit.go` already logs decisions. Add Feedback collection from `eval.Judge.Score` → write `satisfaction = eval.Faithfulness * (1 - eval.HallucinationFlags/max)`.
- `cmd/bandit-replay/main.go`: separate binary that reads `data/bandit.jsonl`, groups by `(task_class, model)`, computes mean satisfaction, and emits `data/router_overrides.json`.
- `WeightedRouter` loads overrides at startup if the file exists; per-(task_class, model), the bench score is `0.5 * static + 0.5 * empirical_satisfaction`.
- Document the cron: `0 3 * * * /usr/local/bin/bandit-replay`.

**Files:**
- `apps/orchestrator/cmd/bandit-replay/main.go`
- `internal/router/weighted.go` — load + merge overrides.

## 8.4 — Neo4j entity graph (1 day)

**Why:** the README's "Graph + Vector hybrid memory." When RAG retrieves, also pull relationships (e.g. "Company X acquired Company Y in year Z").

**Implementation:**
- Replace the Phase 6 `entity.NoopStore` with `entity.Neo4jStore`.
- `internal/entity/neo4j.go`: use `github.com/neo4j/neo4j-go-driver/v5`.
- After a doc is uploaded, run an LLM-based entity/relation extraction over chunks (use a cheap model).
- Per-sub-task `needs_rag=true`: retrieve text chunks AND entity subgraphs.
- `docker-compose.yml`: add Neo4j.

**Files:**
- `apps/orchestrator/internal/entity/neo4j.go`
- `apps/orchestrator/internal/entity/extract.go` (LLM-based extraction)

## 8.5 — Stripe metering (½ day)

**Why:** the README mentions Stripe. Stand-up a billing-ready metering endpoint.

**Implementation:**
- `apps/orchestrator/internal/billing/stripe.go`: emits a Stripe `usage_record` per `/v1/run` based on `cost_usd` from the eval event.
- Webhook handler `POST /v1/stripe/webhook` for invoice finalization.
- A `POST /v1/billing/subscribe` endpoint that creates a Stripe customer + subscription.
- `apps/web/app/settings/billing/page.tsx` — minimal Stripe customer portal link.

**Files:**
- `apps/orchestrator/internal/billing/stripe.go`
- `apps/web/app/settings/billing/page.tsx`

## 8.6 — Helm chart (½ day)

**Why:** one chart to deploy the whole stack on any k8s cluster.

**Implementation:**
- `infra/helm/cogniflow/Chart.yaml`
- `infra/helm/cogniflow/templates/`: `orchestrator-deployment.yaml`, `web-deployment.yaml`, `ml-gateway-deployment.yaml`, `ingress.yaml`, `configmap.yaml`, `secrets.yaml`.
- `infra/helm/cogniflow/values.yaml`: defaults; cloud-specific values in `values-aws.yaml`, `values-gcp.yaml`.

## 8.7 — TruLens + DeepEval harness (1 day)

**Why:** automated offline prompt regression. Run your fixed-eval-set through the orchestrator nightly; compare against last week's score.

**Implementation:**
- `apps/ml-gateway/app/eval/harness.py`: reads `tests/fixtures/prompts.yaml`, sends each through the orchestrator, calls TruLens + DeepEval metrics, writes a CSV report.
- `tests/fixtures/prompts.yaml`: 20+ prompts spanning task classes.
- CI: a `nightly` GitHub Action runs this against the deployed staging env.

**Files:**
- `apps/ml-gateway/app/eval/harness.py`
- `tests/fixtures/prompts.yaml`
- `.github/workflows/nightly-eval.yml`

## 8.8 — Demo screen-record (½ day)

**Why:** the README calls this "resume gold." A polished 3-minute recording is worth more than any other item on this menu.

**Steps:**
1. Practice the script in `docs/demo-script.md` 3 times.
2. Use `ffmpeg` or OBS to record at 1920×1080.
3. Voice-over or subtitles.
4. Upload to YouTube (unlisted) + put link in the repo description.

## Done checklist (per item)

Each item's checklist lives in its own subsection above. Ship whichever combination of items fits your timeline.
