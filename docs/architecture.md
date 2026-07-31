# CogniFlow Architecture

> Deep dive into the moving parts. For the build phases see [`phase-roadmap.md`](phase-roadmap.md).

## Goals

1. Take ONE user prompt.
2. Decompose it into a **DAG of sub-tasks** with an LLM.
3. Route each sub-task to the **best** model (OpenAI, Anthropic, Ollama, …).
4. Run sub-tasks in **parallel**; stream tokens live.
5. **Debate** conflicting claims with a judge agent.
6. **Fuse** all streams into one cohesive answer with **citations**.
7. Enforce a **cost + carbon budget**; auto-downgrade if over.
8. Score every response (faithfulness, hallucination, latency, cost).
9. Let the operator plug in any model (OpenAI-compatible, HuggingFace, local).
10. Self-improve: every routing decision is logged for offline bandit replay.

## High-level shape

Three runtimes:

| Runtime | Why | What lives there |
|---|---|---|
| **Web** (Next.js 14) | UX, live DAG viz, streaming | React Flow, SSE client, eval charts |
| **Orchestrator** (Go) | Concurrency, low latency, fast routing | Decomposer, router, DAG executor, fusion, judge |
| **ML Gateway** (Python) | ML ecosystem (HF, LangChain, TruLens) | Provider adapters, embed, RAG, eval heuristics |

The orchestrator talks to the gateway in-process (gRPC in prod, HTTP for the MVP). Both talk to the web via SSE.

## Hard problems & how we solve them

### 1. Vendor-agnostic streaming

Each provider speaks a different streaming dialect. We normalize at the boundary:

```go
type Streamer interface {
    Stream(ctx context.Context, req Request, sink ChunkSink) error
}
type Chunk struct {
    Text      string
    StreamID  string
    Cite      []SpanRef
    Conf      float64
    Finish    bool
}
type SpanRef struct {
    SubTaskID   string
    Model       string
    PromptHash  string
    DocID       string // RAG doc, if any
    CharStart   int
    CharEnd     int
}
```

Per-provider adapters: ≤80 lines each. OpenAI emits `delta.content` chunks; Anthropic emits `content_block_delta` events; Ollama emits NDJSON lines. The fusion engine doesn't care which it got — they all become `Chunk`.

### 2. DAG execution

Topological sort + goroutines. Bounded parallelism (default: 4). Cancellation via `context.Context` (budget overrun aborts).

A `Temporal` adapter behind the same interface swaps in for production durability (Phase 8) — no app code changes.

### 3. Citation tracking

A single immutable `CitationManifest` is threaded through the DAG. Every `Chunk` carries `SpanRef`s. The web app receives a `manifest` SSE event at the end with the full graph. UI renders inline `[1]`-style citations and a hover-card per cite.

The manifest is **versioned** (`v: "citation.v1"`) so we can evolve the schema without breaking older web builds.

### 4. Decomposer with structured output

OpenAI `response_format: json_schema` (or Anthropic tool-use) with a strict `Plan` schema. The system prompt includes a worked example. Field validators reject malformed plans and fall back to a single-node passthrough.

### 5. Model router — weighted heuristic + bandit

```text
score(m) = 0.45·bench(m, task_class) + 0.30·(1 - cost_norm(m)) + 0.25·(1 - latency_norm(m))
```

Every `(task_class, model, score, was_chosen, user_satisfaction)` tuple is logged. Phase 7 swaps in LinUCB from `bandit-go` for online learning.

### 6. Debate / fusion

Citation-aware LLM-as-judge. When two streams conflict on a factual claim:
- Each model emits claim + claim-citations.
- Judge picks the one with most-supported, non-contradicted claims.
- UI surfaces both verdicts side-by-side; never silently overwrites.

### 7. Cost + carbon budget

Pre-DAG estimator projects (cost, carbon). If over budget: cascade downgrades (Opus → Sonnet → Mixtral → local) until it fits. UI surfaces "downgraded for budget" badge.

### 8. Eval

Per response:
- **Faithfulness**: LLM-as-judge verifies cited claims against RAG doc snippets.
- **Hallucination**: claim decomposition + verification.
- Always logged: cost, latency p95, carbon, model mix.

### 9. Live DAG viz

React Flow + dagre layout. Nodes transition `pending → running → ok/error/debating`. Edges animate from completed to running node. Streams keyed by sub-task id appear in a sidebar `StreamPanel`.

## Deferred to Phase 8

These are explicitly stubbed behind interfaces so they can be added without refactoring:

- Temporal SDK for durable DAG
- Clerk OAuth2
- OpenTelemetry + Langfuse + Grafana
- Stripe metering
- Neo4j entity graph
- K8s + Helm + Terraform + ArgoCD

## Local infra

`docker-compose.yml` brings up:

| Service | Port | Purpose |
|---|---|---|
| Postgres + pgvector | 5432 | Transactional + small vectors |
| Redis | 6379 | Queue + cache |
| Qdrant | 6333/6334 | Large-scale vectors (stub for now) |