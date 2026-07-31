# 🎬 CogniFlow Demo Script

> A **3-minute** screen-recorded walkthrough that hits every wow-feature.
> Designed for a Loom / YouTube link in the repo description — every frame is gold for a recruiter.

## 0 · Setup (do once)

```bash
cp .env.example .env       # add OPENAI_API_KEY + ANTHROPIC_API_KEY
make up                    # Postgres+pgvector, Redis, Qdrant
make dev-orchestrator &    # Go on :8080  (stdout OTel traces)
make dev-web &             # Next.js on :3000
make dev-ml-gateway &      # FastAPI on :8081 (optional in this demo)
sleep 8                    # wait for all three to bind
open http://localhost:3000/playground
```

To prove Phase 8 polish while recording, also start a meter log and the bandit learner:

```bash
mkdir -p ./data
export METER_LOG=./data/usage.jsonl         # writes one JSONL line per node
export ROUTER_RECOMMENDATION=./data/recommendation.json
export OTEL_EXPORTER=stdout                 # default; one JSON span per request on stderr
```

Record at **1920×1080**, speaker notes inline below.

---

## 1 · The demo prompt

Paste into the playground:

> **"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy."**

This prompt is engineered to produce a **4-node DAG**:

| Node | Role | Why this routing decision |
|---|---|---|
| `n1` research | researcher | Mixtral — cheap, parallelizable, no reasoning needed |
| `n2` itinerary | planner | Sonnet — needs budget math + multi-day reasoning |
| `n3` compare | researcher | GPT-4o — factual + debatable; will trigger judge |
| `n4` synthesize | synthesizer | Haiku — fuses the three upstream streams into one answer |

Every screen below lines up with that structure.

---

## 2 · Frame-by-frame narration

### Frame 1 — Decomposition (≈10s)

Click **"Show Plan"**.

- A DAG canvas appears (React Flow + dagre layered layout) with **4 nodes** wired left-to-right.
- Hover any node → a `Routed` card slides out showing the **score breakdown**: `0.45·bench + 0.30·(1-cost) + 0.25·(1-latency)`.
- **Say:** *"One prompt goes in. JSON-schema-constrained decoding on the decomposer turns it into a typed DAG — the same shape every time. The router then scores each node and picks the right model per task class."*

### Frame 2 — Streaming money shot (≈45s)

Hit **"Run"**.

- Each DAG node transitions `pending → running → ok` with animated edges.
- Side panel `MultiStreamPanel` lights up: **one column per model**, tokens flowing live.
- Switch to the **terminal** running the orchestrator → trace JSON for `decomposer.decompose`, `dag.Run`, `dag.runNode`, `provider.stream`, `fusion.Fuse`, `eval.Judge` streams in.
- **Say:** *"Each sub-task is its own goroutine with its own OTel span. The provider interface is vendor-agnostic — OpenAI SSE, Anthropic content-block-deltas, and Ollama NDJSON all normalize into the same `Chunk` shape. Every chunk carries a `SpanRef` so citations are wired in from byte zero."*

### Frame 3 — Fusion + citations (≈30s)

Once all nodes hit `ok`, the `FusionViewer` appears.

- Inline `[1] [2] [3]` citations render after the fused answer.
- Hover any citation → card shows: `model`, `node_id`, `prompt_hash`, `doc_id` + `char_start`–`char_end`.
- For the Apple-vs-NVIDIA comparison, **two models disagreed** on capex → a `DisagreementCard` shows both verdicts side-by-side with the judge LLM's reasoning.
- **Say:** *"Fusion isn't a silent overwrite. When two streams conflict on a fact, both answers are surfaced and the judge picks the most-supported claim. Low-confidence answers get flagged for human review."*

### Frame 4 — Eval badge (≈15s)

Scroll to the bottom — `EvalBadge`:

- **Faithfulness:** 92% (judge LLM)
- **Uncited claims:** 1
- **Cost:** $0.018
- **Carbon:** 0.42 g CO₂
- **Latency:** 6.4s p50, 8.1s p95
- **Model mix:** `mixtral:1, sonnet:1, gpt-4o:1, haiku:1`

Switch to terminal → run:

```bash
go run ./apps/orchestrator/cmd/bandit-learn \
  -log ./data/bandit.jsonl -min 5 -json -out ./data/recommendation.json
cat ./data/recommendation.json | jq '.classes[0:2]'
```

- **Say:** *"Every routing decision is logged with the per-task-class satisfaction. The bandit learner rolls those up nightly — winners get a bench boost, losers get downgraded. Phase 8 closes the loop."*

### Frame 5 — Budget cascade (≈20s, optional)

In the playground, open **Settings → Budget**, set `max_cost_usd: 0.05`. Re-run the same prompt.

- An orange `downgrade` badge appears in the eval panel: *"Opus → Sonnet → Haiku → Mock (downgraded for budget)."*
- The run completes in roughly **half the cost** with marginally lower faithfulness.
- **Say:** *"Phase 7: a budget gate runs before decomposition. The estimator projects cost + carbon per node; if the total exceeds the cap, it cascades models down until it fits. The user is never silently throttled — they see the downgrade."*

### Frame 6 — Meter + Stripe (≈10s, optional)

Switch to terminal:

```bash
tail -f ./data/usage.jsonl | jq .
```

Each node produced one event:

```json
{
  "v": "usage.v1",
  "run_id": "run-3f9c…",
  "node_id": "n2",
  "model": "anthropic:claude-3-5-sonnet",
  "tokens_in": 612, "tokens_out": 318,
  "cost_usd": 0.0093, "carbon_g": 0.18,
  "latency_ms": 2410,
  "workspace": "default",
  "occurred_at": "2026-07-31T22:18:04Z"
}
```

- **Say:** *"Phase 8 ships a `Meterer` interface. Drop in `STRIPE_API_KEY` + `STRIPE_METER_ID` and the same events stream to Stripe's `billing/meter_events` API. Same data, different sink."*

---

## 3 · Recruiter one-liners

> Pick whichever fits the audience. Don't read all of these.

- *"Every claim has a citation — model, prompt-hash, source doc, character range. Hover any `[n]` to see the provenance."*
- *"The router is a weighted contextual bandit with offline replay. Decisions are logged to JSONL, the bandit learner re-weights per task class, the new weights are loaded on next boot."*
- *"The orchestrator is Go for goroutine-native parallelism. The ML glue is Python behind a FastAPI gateway so the open-source ecosystem stays accessible."*
- *"Provider adapters normalize OpenAI SSE, Anthropic content-block-deltas, and Ollama NDJSON into the same `Chunk`. Vendor-agnostic from day one."*
- *"OpenTelemetry traces correlate per-node latency, token usage, cost, and carbon onto one timeline. The trace id surfaces on the `done` SSE event so a recruiter can click from the UI straight into a collector."*
- *"This isn't a wrapper around one API. It's a routing engine that treats AI as a team of specialists — and tracks every decision it makes."*

---

## 4 · 30-second cut

If you only have a half-minute:

1. Hit **Show Plan** → 4-node DAG appears (5s).
2. Hit **Run** → streams flow side-by-side → fused answer with `[1] [2]` cites (15s).
3. Eval badge + budget downgrade badge (10s).

---

## 5 · Recording checklist

- [ ] `make up` is clean (`docker compose ps` shows all four services).
- [ ] All three dev servers are running with no startup errors.
- [ ] `OPENAI_API_KEY` and `ANTHROPIC_API_KEY` are set in `.env` (no mock-only runs).
- [ ] `METER_LOG` + `ROUTER_RECOMMENDATION` are set so the Phase 8 story closes.
- [ ] Terminal font is ≥ 16px; orchestrator's stdout traces are visible.
- [ ] Browser zoom is 100%; React Flow canvas fills the viewport.
- [ ] Cursor is visible — click deliberately.
- [ ] Audio test pass: voice is louder than the click sounds.
- [ ] Save the file as `demo-1920x1080.mp4`, upload to YouTube (unlisted), drop the link in the repo description.