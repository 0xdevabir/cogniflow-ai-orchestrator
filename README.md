# 🧠 CogniFlow — Autonomous Multi-Model AI Orchestration Platform

> **"One prompt. The world's best minds — coordinated."**

A self-orchestrating AI routing engine that decomposes a single prompt into specialized sub-tasks, routes each to the **best** model (OpenAI, Anthropic, local Ollama, …), runs them in parallel, debates conflicting answers, and fuses everything into a citation-rich response — all streamed live to a React UI that visualizes the DAG.

---

## 🚀 Quick start

```bash
cp .env.example .env       # add OPENAI_API_KEY / ANTHROPIC_API_KEY if you have them
make up                    # Postgres+pgvector, Redis, Qdrant
make dev-web               # terminal 1 — Next.js on :3000
make dev-orchestrator      # terminal 2 — Go on :8080
make dev-ml-gateway        # terminal 3 — FastAPI on :8081
```

Open `http://localhost:3000` and ask:

> *"Plan a 3-day foodie trip to Tokyo under $500 — also compare NVIDIA's vs Apple's 2024 strategy."*

You should see:
1. The prompt split into a DAG of sub-tasks (React Flow).
2. Each node light up as its assigned model streams tokens.
3. A final fused answer with `[1] [2]` citations and a hover-card on each.
4. An eval badge (faithfulness %, cost, latency, model mix).

---

## 🧱 Repository layout

```
cogniflow-ai-orchestrator/
├── apps/
│   ├── web/                  Next.js 14 + React Flow (live DAG viz)
│   ├── orchestrator/         Go — decomposer, router, DAG, fusion, eval
│   └── ml-gateway/           Python FastAPI — provider adapters, RAG, judge
├── packages/
│   ├── schemas/              JSON schemas (Plan, Citation, Chunk)
│   └── prompts/              Versioned prompt templates
├── docs/                     architecture, API, demo script
├── docker-compose.yml        Postgres+pgvector, Redis, Qdrant
├── Makefile                  single entrypoint for the whole repo
└── .github/workflows/ci.yml  lint+test pipeline
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       WEB  (Next.js 14)                      │
│  React Flow DAG · SSE streaming · citations · eval charts  │
└──────────────────────────┬──────────────────────────────────┘
                           │  /v1/chat  (HTTP + SSE)
┌──────────────────────────▼──────────────────────────────────┐
│              ORCHESTRATOR  (Go · Temporal Worker)            │
│  Decomposer · Router · DAG Executor · Fusion · Judge        │
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
| 🔁 Self-Improving Loops | Bandit logs replayed nightly to update router policy |
| 🛰️ Edge Inference | Falls back to local model if cloud latency > threshold |

---

## 🛣️ Phased build

See [`docs/phase-roadmap.md`](docs/phase-roadmap.md). The current status of each phase is tracked in the project's task list.

---

## 🧪 Verification

```bash
make test           # go test + pytest + vitest
make lint           # gofmt + ruff + eslint
```

End-to-end smoke (after `make up` + all dev-* running):

```bash
curl -N -X POST localhost:8080/v1/chat \
  -H 'content-type: application/json' \
  -d '{"prompt":"Plan a 3-day foodie trip to Tokyo under $500"}'
# expect SSE stream: nodes_running → chunks → manifest → done
```

---

## 📜 License

MIT