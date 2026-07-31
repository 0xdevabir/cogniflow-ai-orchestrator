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
- [x] `Streamer` interface + `Chunk` struct
- [x] OpenAI adapter (real), Anthropic adapter (real), mock adapter
- [x] FastAPI ml-gateway provider endpoints
- [x] Go chat handler with SSE
- [x] Next.js `StreamPanel` listening to SSE
- **Demo:** "Explain CAP theorem" streams in the UI from OpenAI.
- **Build spec:** [`build-specs/phase-1-providers-chat.md`](build-specs/phase-1-providers-chat.md)

## Phase 2 — Prompt Decomposer  *(1 day)*
- [x] `plan.schema.json` (✅ already in `packages/schemas/`)
- [x] Versioned decomposer prompt (✅ in `packages/prompts/`)
- [x] Go decomposer with JSON-schema constrained decoding
- [x] `/v1/plan` debug endpoint
- [x] React Flow DAG viz
- **Demo:** A complex prompt renders as a 4-node DAG in the UI.
- **Build spec:** [`build-specs/phase-2-decomposer.md`](build-specs/phase-2-decomposer.md)

## Phase 3 — Model Router  *(1 day)*
- [x] `WeightedRouter` + `Bandit` interface
- [x] `benchmarks.json` + `cost_table.json`
- [x] Per-node routing annotations
- [x] "Why this model?" score-breakdown panel
- **Demo:** Each node of the DAG routes to a different model.
- **Build spec:** [`build-specs/phase-3-router.md`](build-specs/phase-3-router.md)

## Phase 4 — DAG executor + parallel sub-tasks  *(1 day)*
- [x] In-process DAG executor (goroutines, joins, context cancel)
- [x] Topological sort with cycle/dangling-edge detection
- [x] Upstream text injection into downstream prompts
- [x] Temporal adapter stub + `ExecutorMode` flag
- [x] `/v1/run` SSE endpoint emitting `plan`, `node_status`, `chunk`, `done`
- [x] `MultiStreamPanel` UI bucketed by `node_id`
- [x] Live DAG node color transitions + animated edges
- **Demo:** 4-node diamond plan executes in 3 topological levels, all nodes `ok`, UI shows each stream side-by-side.
- **Build spec:** [`build-specs/phase-4-dag-executor.md`](build-specs/phase-4-dag-executor.md)

## Phase 5 — Fusion Engine + Citations  *(1½ days)*
- [x] Incremental stream merger (heuristic + model-driven modes)
- [x] Immutable `CitationManifest` (`v: "citation.v1"`, `Span{ID,SubTaskID,Model,Text,DocID,DocSnippet,PromptHash}`)
- [x] SSE `manifest` event + `fusion_start` + `fusion` chunk stream
- [x] Judge LLM for disagreement detection (JSON verdict + winners[])
- [x] `FusionViewer` with `[1]` citations + hover cards (model/node/prompt_hash/doc)
- [x] Disagreement side-by-side panel (`DisagreementCard`)
- [x] `FusionRunner` wires the SSE fusion stream into the playground UI
- [x] DAG executor invokes `Fuser` after all upstream nodes finish
- **Demo:** Conflicting facts surface both verdicts with citations; live SSE end-to-end with `fusion_start → fusion chunks → manifest → done` verified via `curl`.
- **Build spec:** [`build-specs/phase-5-fusion-citations.md`](build-specs/phase-5-fusion-citations.md)

## Phase 6 — RAG + Memory (pgvector)  *(1½ days)*
- [x] Chunker (sentence-aware, 800/200 overlap) + lexical cosine fallback
- [x] `rag.Service` upload + `BuildInjectedSystemPrompt` (top-k=6 with `===DOC n | doc_id | "title"===` blocks)
- [x] OpenAI `text-embedding-3-small` embedder (1536 dims, batched)
- [x] `MemStore` for dev/test + pgvector SQL migration embedded (`migrations/0001_init.sql`); PgStore lands in Phase 8 alongside the pgx dep
- [x] `EntityStore` interface + `NoopStore` stub for Neo4j swap-in (Phase 8)
- [x] DAG executor injects retrieval into `NeedsRAG` nodes, stamps every emitted `chunk` with `cite[]` carrying `doc_id`/`char_start`/`char_end`
- [x] Citation manifest gains one span per retrieved doc so hover-cards can show source + char range
- [x] API: `POST /v1/docs` (multipart + JSON), `GET /v1/docs`, `DELETE /v1/docs/{id}`
- [x] `DocUploader` (drag-drop) + `DocList` (refresh + delete) wired into `/playground`
- [x] End-to-end smoke verified: doc uploaded → 8 chunks → run with `needs_rag=true` → chunks carry `cite[]` with the doc id + ranges
- **Demo:** Upload a doc, ask about a clause, every chunk in the SSE stream carries `cite[].doc_id` + char range; FusionViewer hover-card renders the doc snippet.
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
