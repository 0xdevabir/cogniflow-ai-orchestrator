# CogniFlow Demo Script

> 3-minute screen-recorded walkthrough that showcases every wow-feature.

## Setup

```bash
make up
make dev-web &
make dev-orchestrator &
make dev-ml-gateway &
# wait ~10s for all three to be ready
open http://localhost:3000
```

## The demo prompt

> **"Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy."**

This prompt is engineered to produce a 4-node DAG:

1. Research best ramen/izakaya neighborhoods in Tokyo (RAG not needed, web-ish)
2. Plan a daily itinerary under $500 (reasoning-heavy, needs budget math)
3. Compare NVIDIA vs Apple 2024 strategy (factual, debatable)
4. Synthesize all into one cohesive trip + comparison answer

## What you should narrate as you click

1. **Hit "Show Plan"** — the DAG appears with 4 nodes (React Flow). Mention JSON-schema-constrained decoding and how the decomposer LLM turned one prompt into a structured DAG.
2. **Hit "Run"** — nodes transition `pending → running`. Point out:
   - Each node routes to a **different model** (e.g. Mixtral for "research neighborhoods", Sonnet for "itinerary", GPT-4o for "comparison", Opus for final synthesis).
   - Open the "Why this model?" panel — show the cost/latency/benchmark score breakdown.
   - The "Money shot": each model's stream renders side-by-side as tokens flow.
3. **Hover the DAG nodes** — point out the per-node latency and cost badges.
4. **Wait for completion** — the **FusionViewer** appears with:
   - Inline `[1] [2]` citations.
   - Hover any cite → card shows model, prompt-hash, and source RAG doc (if any).
   - When two models disagreed (e.g. on Apple vs NVIDIA capex), show the **side-by-side disagreement panel** with the judge's reasoning.
5. **EvalBadge** at the bottom — faithfulness %, hallucination flags, total cost, latency, model mix.
6. **Set budget $0.05** in settings and re-run the same prompt — show cascade downgrade badge: "Opus → Sonnet → Mixtral (downgraded for budget)".

## Recruiter one-liners

- *"Every claim has a citation pointing back to the model and prompt that produced it."*
- *"Routing is a contextual bandit with offline replay — every decision is logged for self-improvement."*
- *"The orchestrator is written in Go for goroutine-native parallelism; the ML glue lives in FastAPI for ecosystem access."*
- *"Phase 8 swaps the in-process DAG executor for Temporal without changing app code — same interface."*
- *"This is not a wrapper around one API. It's a routing engine that treats AI as a team of specialists."*

## Cut-list if time is tight

- Skip the disagreement panel (still mention it exists).
- Skip budget cascade demo (mention it's in Phase 7).
- Show only 2 of 4 model streams in the sidebar; mention there are 4.