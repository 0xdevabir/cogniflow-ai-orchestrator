# 🧠 CogniFlow — Autonomous Multi-Model AI Orchestration Platform

> **"One prompt. The world's best minds — coordinated."**

A self-orchestrating AI routing engine that decomposes a single prompt into specialized sub-tasks, routes each to the **best** model (OpenAI, Anthropic, local Ollama, …), runs them in parallel, debates conflicting answers, and fuses everything into a citation-rich response — all streamed live to a React UI that visualizes the DAG. Every decision is traced (OpenTelemetry), metered (Stripe / JSONL), and replayed nightly to self-improve (bandit learner).

---

## 🚀 Quick start

```bash
cp .env.example .env       # add OPENAI_API_KEY / ANTHROPIC_API_KEY
make up                    # Postgres+pgvector, Redis, Qdrant
make dev-orchestrator &    # terminal 1 — Go on :8080 (OTel on stderr)
make dev-web &             # terminal 2 — Next.js on :3000
make dev-ml-gateway &      # terminal 3 — FastAPI on :8081 (optional)
```

Open `http://localhost:3000/playground` and ask:

> *"Plan a 3-day foodie trip to Tokyo under $500 — also compare NVIDIA's vs Apple's 2024 strategy."*

You should see:
1. The prompt split into a DAG of sub-tasks (React Flow) — click **"Show Plan"**.
2. Each node light up as its assigned model streams tokens — click **"Run"**.
3. A final fused answer with `[1] [2]` citations and a hover-card on each.
4. An `EvalBadge` (faithfulness %, cost, carbon, latency, model mix).
5. A `DowngradeBadge` if your budget forced a cascade.

Full screen-recording walkthrough: [`docs/demo-script.md`](docs/demo-script.md).

---

## 🧱 Repository layout

```
cogniflow-ai-orchestrator/
├── apps/
│   ├── web/                      Next.js 14 + React Flow (live DAG viz)
│   ├── orchestrator/             Go — decomposer, router, DAG, fusion, eval
│   │   ├── cmd/
│   │   │   ├── server/           main orchestrator binary
│   │   │   └── bandit-learn/     offline bandit rebalancer
│   │   └── internal/
│   │       ├── api/              HTTP + SSE handlers
│   │       ├── decomposer/       LLM → Plan with JSON schema
│   │       ├── router/           WeightedRouter + Bandit + recommendation loader
│   │       ├── dag/              In-proc executor + Temporal stub
│   │       ├── fusion/           Stream merger + judge
│   │       ├── citation/         Manifest + SpanRef
│   │       ├── budget/           Cost + carbon estimator + cascade
│   │       ├── eval/             Faithfulness judge (LLM-as-judge)
│   │       ├── rag/              pgvector + mem store, embedder
│   │       ├── meter/            Stripe + JSONL + Noop
│   │       └── obs/              OpenTelemetry tracer + middleware
│   └── ml-gateway/               Python FastAPI — provider adapters, RAG, judge
├── packages/
│   ├── schemas/                  JSON schemas (Plan, Citation, Chunk)
│   └── prompts/                  Versioned prompt templates
├── deploy/
│   └── cogniflow/                Helm chart (apiVersion v2)
├── docs/                         architecture, API, demo script, phase roadmap
├── docker-compose.yml            Postgres+pgvector, Redis, Qdrant
├── Makefile                      single entrypoint for the whole repo
└── .github/workflows/ci.yml      lint+test pipeline
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       WEB  (Next.js 14)                      │
│  React Flow DAG · SSE streaming · citations · eval badges  │
└──────────────────────────┬──────────────────────────────────┘
                           │  /v1/run  (HTTP + SSE)
                           │  · plan
                           │  · node_status
                           │  · chunk
                           │  · fusion_start / fusion
                           │  · manifest
                           │  · downgrade / eval
                           │  · done  (with trace_id, span_id)
┌──────────────────────────▼──────────────────────────────────┐
│              ORCHESTRATOR  (Go · OpenTelemetry)             │
│  Decomposer · Router · DAG Executor · Fusion · Judge        │
│  Budget gate · Meterer (Stripe / JSONL) · Bandit log        │
└──────────────────────────┬──────────────────────────────────┘
                           │  in-process Go calls
┌──────────────────────────▼──────────────────────────────────┐
│              ML GATEWAY   (Python · FastAPI)                │
│  Provider adapters · chunker · embedding · RAG · judge      │
└─────────────────────────────────────────────────────────────┘
                           │
              ┌────────────┼────────────┬──────────────┐
              ▼            ▼            ▼              ▼
          Postgres      Redis       Qdrant        OpenAI / Anthropic
          (pgvector)   (BullMQ)    (vectors)      + Ollama stub
```

See [`docs/architecture.md`](docs/architecture.md) for the deep dive and [`docs/phase-roadmap.md`](docs/phase-roadmap.md) for the build phases.

---

## 🎯 Core features

| Capability | Description |
|---|---|
| 🔀 Dynamic Model Router | Per-task model selection by cost / latency / benchmark |
| 🧩 Prompt Decomposer | LLM turns a prompt into a structured DAG of sub-tasks |
| ⚔️ Agent Debater | LLM-as-judge reconciles conflicting answers with reasoning traces |
| 🧠 RAG Memory Layer | pgvector for transactional metadata, Qdrant for scale |
| 💸 Cost & Carbon Budget | Per-conversation caps, automatic cascade downgrade |
| 📊 Eval Dashboard | Faithfulness, hallucination, cost, latency per response |
| 🔌 Bring Your Own Model | Plug in any OpenAI-compatible API, HF, or local LLM |
| 🪪 Citation Engine | Every claim traces back to source doc or model+prompt |
| 🔁 Self-Improving Loops | Bandit logs replayed nightly → router re-weights |
| 🛰️ Edge Inference | Falls back to local model if cloud latency > threshold |
| 🛰️ OpenTelemetry Traces | Per-request spans across decomposer→router→provider→fusion→judge |
| 💳 Usage Metering | Per-node Stripe `meter_events` or JSONL for self-hosted billing |
| ⎈ Helm Chart | One-command deploy to any K8s cluster |

---

## 🌐 HTTP API

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Service status + registered providers |
| `/v1/plan` | POST | Decompose prompt → DAG + per-node routing decisions (JSON) |
| `/v1/run` | POST | Execute Plan (or prompt) → SSE stream with all phases |
| `/v1/chat` | POST | Single-model streaming chat (no DAG) — Phase 1 endpoint |
| `/v1/docs` | POST | Upload a doc for RAG (multipart or JSON) |
| `/v1/docs` | GET | List docs in a workspace |
| `/v1/docs/{id}` | DELETE | Remove a doc |

Every `/v1/run` `done` event includes `trace_id` and `span_id` so a recruiter can click from the playground straight into a Jaeger / Tempo / Langfuse UI.

---

## ⚙️ Environment variables

| Var | Default | Notes |
|---|---|---|
| `OPENAI_API_KEY` | — | Enables OpenAI adapters + `text-embedding-3-small` embedder |
| `ANTHROPIC_API_KEY` | — | Enables Anthropic adapters |
| `MISTRAL_API_KEY` | — | Adapter stub; ready for Phase 8+ |
| `HF_API_KEY` | — | Hugging Face adapter stub |
| `OLLAMA_BASE_URL` | — | Local model endpoint |
| `DECOMP_MODEL` | `openai:gpt-4o-mini` | Decomposer model (any `provider:model`) |
| `BANDIT_LOG` | `./data/bandit.jsonl` | Where routing + feedback events are appended |
| `ROUTER_RECOMMENDATION` | — | Path to JSON output of `bandit-learn`; applied on boot |
| `METER_LOG` | — | Path for JSONL meter output (one event per node) |
| `STRIPE_API_KEY` | — | Enables Stripe meter events (takes precedence over `METER_LOG`) |
| `STRIPE_METER_ID` | — | Required when `STRIPE_API_KEY` is set |
| `OTEL_EXPORTER` | `stdout` | `stdout` / `otlp` / `none` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Required when `OTEL_EXPORTER=otlp` |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Set `true` for plaintext OTLP collectors |
| `OTEL_ENV` | `dev` | `deployment.environment` resource attribute |
| `ORCH_PORT` | `:8080` | Bind address |

---

## 🛠️ CLI tools

### `bandit-learn` — offline router rebalancer

```bash
# Human-readable summary
go run ./apps/orchestrator/cmd/bandit-learn -log ./data/bandit.jsonl

# JSON for next-boot load
go run ./apps/orchestrator/cmd/bandit-learn \
  -log ./data/bandit.jsonl -min 5 -json -out ./data/recommendation.json

# Restart orchestrator with new weights
ROUTER_RECOMMENDATION=./data/recommendation.json make dev-orchestrator
```

Reads every `(task_class, model, score, was_chosen, satisfaction)` row from the bandit log, groups per task class, declares a winner, and writes a recommendation that `WeightedRouter.SetWeights` will pick up on boot. Wrap in cron:

```cron
0 3 * * * cd /srv/cogniflow && \
  /usr/local/bin/bandit-learn -log /data/bandit.jsonl -min 50 -json -out /data/recommendation.json
```

---

## ⎈ Helm chart

```bash
helm lint ./deploy/cogniflow
helm template cogniflow ./deploy/cogniflow --namespace cogniflow

# Real install
helm install cogniflow ./deploy/cogniflow \
  --namespace cogniflow --create-namespace \
  --set secrets.openaiApiKey=$OPENAI_API_KEY \
  --set secrets.anthropicApiKey=$ANTHROPIC_API_KEY \
  --set persistence.enabled=true \
  --set orchestrator.autoscaling.enabled=true
```

Renders: `ServiceAccount`, `Secret`, `PersistentVolumeClaim`, `Deployment`+`Service`+`HorizontalPodAutoscaler` for the orchestrator (port 8080), `Deployment`+`Service` for the web app (port 3000), optional Ingress. Pods run as non-root with a read-only root filesystem; bandit log persists on a 1Gi PVC mounted at `/data`.

After deploy, run the bandit learner inside the pod:

```bash
kubectl exec -it deploy/cogniflow-orchestrator -- \
  bandit-learn -log /data/bandit.jsonl -json -out /data/recommendation.json
kubectl exec -it deploy/cogniflow-orchestrator -- kill -HUP 1
```

---

## 🛣️ Phased build

See [`docs/phase-roadmap.md`](docs/phase-roadmap.md). Per-phase deep specs live in [`docs/build-specs/`](docs/build-specs/README.md). Phases 0–7 ship a working product. Phase 8 is cherry-pick; the items shipped in this repo are:

- ✅ OpenTelemetry traces (`stdout`/`otlp`/`none`)
- ✅ `Meterer` interface — Stripe billing events + JSONL fallback
- ✅ Bandit self-improvement loop (`bandit-learn` CLI + recommendation loader)
- ✅ Helm chart for K8s deploy
- ✅ Demo script (`docs/demo-script.md`)

Deferred (documented in [`docs/build-specs/phase-8-polish.md`](docs/build-specs/phase-8-polish.md)): Temporal durable DAG, Neo4j entity graph, TruLens/DeepEval harness, Clerk auth, Terraform.

---

## 🧪 Verification

```bash
make test           # go test + pytest + vitest
make lint           # gofmt + ruff + eslint
```

End-to-end smoke (after `make up` + all dev-* running):

```bash
curl -s localhost:8080/healthz | jq .
# {"status":"ok","service":"cogniflow-orchestrator","providers":["openai","anthropic","mock"]}

curl -N -X POST localhost:8080/v1/run \
  -H 'content-type: application/json' \
  -d '{"prompt":"Plan a 3-day foodie trip to Tokyo under $500"}'
# expect SSE stream:
#   event: plan
#   event: node_status
#   event: chunk
#   event: fusion_start → fusion → manifest
#   event: eval
#   event: done  (includes trace_id, span_id, run_id, total cost/carbon)
```

---

## 📜 License

MIT