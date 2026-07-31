# CogniFlow — Phase Roadmap

> Each phase ends with a **green demo moment**.
>
> 📘 **For detailed per-phase build specs** (the actual instruction set you hand to an AI builder), see [`build-specs/`](build-specs/README.md).

## Phase 0 — Repo skeleton & dev loop  *(½ day)*
- [x] Monorepo dirs
- [x] `docker-compose.yml`
- [x] `Makefile`
- [x] GitHub Actions CI
- [x] Root README
- [x] Architecture doc
- **Demo:** `make up` brings up Postgres+pgvector, Redis, Qdrant.
- **Spec:** see Phase 0 in [`docs/architecture.md`](architecture.md) — was completed in the initial bootstrap.

## Phase 1 — Provider abstraction + happy-path chat  *(1 day)*
- [ ] `Streamer` interface + `Chunk` struct
- [ ] OpenAI adapter (real), Anthropic adapter (real), mock adapter
- [ ] FastAPI ml-gateway provider endpoints
- [ ] Go chat handler with SSE
- [ ] Next.js `StreamPanel` listening to SSE
- **Demo:** "Explain CAP theorem" streams in the UI from OpenAI.
- **Build spec:** [`build-specs/phase-1-providers-chat.md`](build-specs/phase-1-providers-chat.md)

## Phase 2 — Prompt Decomposer  *(1 day)*
- [ ] `plan.schema.json` (✅ already in `packages/schemas/`)
- [ ] Versioned decomposer prompt (✅ in `packages/prompts/`)
- [ ] Go decomposer with JSON-schema constrained decoding
- [ ] `/v1/plan` debug endpoint
- [ ] React Flow DAG viz
- **Demo:** A complex prompt renders as a 4-node DAG in the UI.
- **Build spec:** [`build-specs/phase-2-decomposer.md`](build-specs/phase-2-decomposer.md)

## Phase 3 — Model Router  *(1 day)*
- [ ] `WeightedRouter` + `Bandit` interface
- [ ] `benchmarks.json` + `cost_table.json`
- [ ] Per-node routing annotations
- [ ] "Why this model?" score-breakdown panel
- **Demo:** Each node of the DAG routes to a different model.
- **Build spec:** [`build-specs/phase-3-router.md`](build-specs/phase-3-router.md)

## Phase 4 — DAG executor + parallel sub-tasks  *(1 day)*
- [ ] In-process DAG executor (goroutines, joins, context cancel)
- [ ] Temporal adapter stub
- [ ] UI DAG node status transitions
- **Demo:** 4-node plan runs in parallel, all nodes `ok`, UI shows each stream.
- **Build spec:** [`build-specs/phase-4-dag-executor.md`](build-specs/phase-4-dag-executor.md)

## Phase 5 — Fusion Engine + Citations  *(1½ days)*
- [ ] Incremental stream merger
- [ ] Immutable `CitationManifest`
- [ ] SSE `manifest` event
- [ ] `FusionViewer` with `[1]` citations + hover cards
- [ ] Disagreement side-by-side panel
- **Demo:** Conflicting facts surface both verdicts with citations.
- **Build spec:** [`build-specs/phase-5-fusion-citations.md`](build-specs/phase-5-fusion-citations.md)

## Phase 6 — RAG + Memory (pgvector)  *(1½ days)*
- [ ] Upload → chunk → embed → pgvector
- [ ] Per-node `needs_rag` retrieval + injection
- [ ] `EntityStore` interface stub (Neo4j later)
- [ ] Upload UI
- **Demo:** Upload a PDF, ask about a clause, get cited answer.
- **Build spec:** [`build-specs/phase-6-rag.md`](build-specs/phase-6-rag.md)

## Phase 7 — Eval + Cost/Carbon Budget  *(1 day)*
- [ ] Faithfulness LLM-judge
- [ ] Cost + carbon estimator + cascade downgrade
- [ ] `EvalBadge` in UI
- [ ] Settings page
- **Demo:** Set budget $0.10, big request cascades to cheap models.
- **Build spec:** [`build-specs/phase-7-eval-budget.md`](build-specs/phase-7-eval-budget.md)

## Phase 8 — Production polish  *(3–4 days, cherry-pick)*
- [ ] Temporal durable DAG
- [ ] OpenTelemetry + Langfuse
- [ ] Stripe metering
- [ ] Neo4j entity store
- [ ] Self-improving bandit loop
- [ ] Helm chart
- [ ] Demo script (`docs/demo-script.md`)
- **Build spec:** [`build-specs/phase-8-polish.md`](build-specs/phase-8-polish.md)

## Effort & dependency map

| Phase | Effort | Demoable? | Depends On |
|---|---|---|---|
| 0 — skeleton | ½ day | ✅ | — |
| 1 — providers + chat | 1 day | ✅ | 0 |
| 2 — decomposer | 1 day | ✅ | 1 |
| 3 — router | 1 day | ✅ | 2 |
| 4 — DAG executor + parallel | 1 day | ✅ | 3 |
| 5 — fusion + citations | 1½ days | ✅ | 4 |
| 6 — RAG | 1½ days | ✅ | 1 |
| 7 — eval + budget | 1 day | ✅ | 5 |
| 8 — production polish | 3–4 days | ✅ | 1–7 |

**Total core (Phases 0–7): ~8 days of focused work.** Phase 8 is cherry-pick.
