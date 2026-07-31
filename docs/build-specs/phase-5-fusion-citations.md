# Phase 5 — Fusion Engine + Citations

## Goal

Streams from sub-tasks are fused into a single, coherent answer with **per-token citations** (`[1] [2]`). When two streams disagree on a fact, both verdicts are shown side-by-side with a judge's reasoning — not silently overwritten.

**Demo moment:** the same 4-node tokyo plan now produces:
- A coherent `FusionViewer` panel at the bottom with clickable citations.
- A hover-card per cite showing `model`, `prompt_hash`, RAG doc snippet (RAG chunks arrive in Phase 6).
- For the "NVIDIA vs Apple" critic node, two model verdicts side-by-side with the judge's pick highlighted.

## Prerequisites

- ✅ Phase 4: parallel DAG executing, streams arriving.

## Architecture this phase lays down

```
[Each sub-task's aggregated text + cited spans]
   │ for each pair (n2, n3) of converging nodes that produced overlapping claims:
   │   judgment.Disagreement(ctx, claimA, claimB) → Verdict
   │ emit CitationManifest with merged spans
   ▼
[fusion.Fuse] (Phase 5)
   │ for the synthesizer node:
   │   1. Build system prompt with all upstream texts + their citations
   │   2. Add a "you MUST include [n] citations inline" instruction
   │   3. Stream tokens. Tag each token chunk with its source SpanRef.
   │ OR
   │   For simple fusion: post-process the synthesizer output to inject citations
   │   from the upstream CitationManifest.
   ▼
[End-of-run: emit "manifest" SSE event with the full graph]
```

> **Two modes:**
> 1. **Model-driven fusion (default):** the synthesizer LLM does the merging and emits `[1]`-style markers. We map markers → cites via the upstream manifest.
> 2. **Heuristic fusion:** for short plans with ≤2 streams, concatenate with paragraph-level citations. (Phase 5 implements both, configurable.)

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/citation/manifest.go`
```go
package citation

const ManifestVersion = "citation.v1"

type Manifest struct {
    V     string `json:"v"`
    Spans []Span `json:"spans"`
}

type Span struct {
    ID          string `json:"id"`            // unique, e.g. "sp_001"
    SubTaskID   string `json:"sub_task_id"`
    Model       string `json:"model"`
    Text        string `json:"text"`          // the verbatim substring claimed
    DocID       string `json:"doc_id,omitempty"`
    DocSnippet  string `json:"doc_snippet,omitempty"`  // RAG context (Phase 6 fills)
    PromptHash  string `json:"prompt_hash,omitempty"`
    CharStart   int    `json:"char_start,omitempty"`
    CharEnd     int    `json:"char_end,omitempty"`
}

// NewManifest builds a manifest. Hashes prompts for provenance.
func NewManifest() *Manifest

// Add appends a span and returns its ID.
func (m *Manifest) Add(s Span) string

// Lookup returns spans by ID.
func (m *Manifest) Lookup(id string) (Span, bool)
```

JSON tags MUST match `packages/schemas/citation.schema.json`.

#### 2. `internal/citation/manifest_test.go`
- `TestManifest_AddSerializes`: Add 3 spans, marshal to JSON, expect `"v":"citation.v1"` + sorted IDs.
- `TestManifest_Lookup`: Add + lookup round-trip.

#### 3. `internal/fusion/fusion.go`
```go
package fusion

type Mode string

const (
    ModeModelDriven  Mode = "model"  // synthesize via LLM with citation markers
    ModeHeuristic    Mode = "heuristic"  // concatenate + paragraph cites
)

type Config struct {
    Mode        Mode
    JudgeModel  string   // e.g. "openai:gpt-4o-mini"  (cheap)
    Threshold   float64  // disagreement similarity threshold (default 0.85)
}

type Fuser interface {
    Fuse(ctx context.Context, fr FusionRequest, sink providers.ChunkSink) error
}

type FusionRequest struct {
    Plan       *decomposer.Plan
    Streams    map[string]*NodeStream  // node.id → aggregated text + manifests
    Prompt     string                 // original user prompt
    SynthNode  decomposer.Node        // the synthesizer node
}

type NodeStream struct {
    NodeID    string
    Text      string
    Manifest  *citation.Manifest
}

func New(cfg Config, registry providers.Registry) Fuser { ... }

// Default returns a config that chooses mode automatically:
//   1 stream          → heuristic
//   2+ streams, same task_class → model
//   2+ streams, disagreeing claims → model + judge
```

**Implementation outline (`ModelDriven` mode):**
1. Build the synthesizer prompt:
   ```
   SYSTEM:
   You are the CogniFlow Synthesizer.

   Merge the following sub-task outputs into one cohesive answer to the user's prompt.
   Every claim must cite its source as [n] inline, where n is the ID from the SPANS table.

   USER PROMPT:
   {prompt}

   === UPSTREAM OUTPUTS ===

   -- researcher (anthropic:claude-3-5-sonnet) [spans: sp_001, sp_002]
   {aggregated text}

   -- planner (openai:gpt-4o-mini) [spans: sp_003]
   {aggregated text}

   === SPAN TABLE ===
   [sp_001] ...verbatim claim...
   [sp_002] ...verbatim claim...
   [sp_003] ...verbatim claim...

   RESPONSE FORMAT:
   <cohesive answer with inline [n] citations>
   ```
2. Stream the response. For each chunk, tag with `Cite: [spanRefs]`. The mapping `[n] → spanID` is sent to the UI in the manifest event.

#### 4. `internal/fusion/judge.go`
```go
type Judge struct {
    Registry providers.Registry
    Model    string       // e.g. "openai:gpt-4o-mini"
    Prompt   string       // loaded from packages/prompts/judge.v1.md
}

type Claim struct {
    Text     string
    SpanIDs  []string
    Model    string
}

type Verdict struct {
    Pick       string    // "A" | "B" | "tie"
    Confidence float64
    Reasoning  string
    Winners    []string  // span IDs that the judge endorsed
}

func (j *Judge) Compare(ctx context.Context, a, b Claim) (*Verdict, error)
```

Detect disagreement: when 2 nodes both produced overlapping claims (cosine similarity > 0.85, computed by a simple bag-of-tokens Jaccard since we don't have embeddings here, OR by string containment). If disagreement → call `Judge.Compare`.

> **Embedding-based similarity is Phase 8 (adds an embed provider).** For Phase 5 use Jaccard on tokenized sentences.

#### 5. `internal/fusion/fusion_test.go`
| Test | What |
|---|---|
| `TestHeuristicFusion_ConcatenatesWithCites` | Two streams → output contains both + a paragraph cite marker for each. |
| `TestModelDriven_BuildsPromptWithSpans` | Verify the synthesizer prompt includes the SPANS table. |
| `TestJudge_VerdictShape` | Mock provider returns judge JSON; Verdict parses. |
| `TestFuser_DefaultHeuristicForSingle` | Single stream → heuristic. |
| `TestFuser_DefaultModelForMulti` | Two streams with different conclusions → model + judge called. |

#### 6. Embed prompts + schemas
Same embed trick as Phase 2:
- `apps/orchestrator/internal/fusion/prompts/judge.v1.md` ← copy from `packages/prompts/judge.v1.md`
- `apps/orchestrator/internal/fusion/prompts/fusion_synthesizer.v1.md` ← write fresh; see `docs/build-specs/judge.v1.md` template.

> Add `fusion_synthesizer.v1.md` to `packages/prompts/` first (then copy into the embed location).

#### 7. `internal/citation/spans.go`
Helpers to build span IDs deterministically (`sp_<5-char nanoid>` or simple counter).

#### 8. Modify `internal/dag/executor.go`
- After `runNode` finishes, store its aggregated text + its per-node Manifest slice in `outputs`.
- Pass everything to `fusion.Fuse` at the very end, with the synthesizer node as `SynthNode`.
- The fusion stream becomes the FINAL stream — its chunks are emitted as the canonical "answer" stream (not tagged with `node_id`).

#### 9. SSE event additions
Two new event types emitted at end of run:

```
event: verdict
data: {"node_a":"n3","node_b":"n5","verdict":"A","confidence":0.82,"reasoning":"Model A cites a primary source...","winners":["sp_012","sp_014"]}

event: manifest
data: {"v":"citation.v1","spans":[{...},{...}]}
```

`internal/dag/executor.go` emits these after the fusion finishes.

### Web — `apps/web/`

#### 10. `components/FusionViewer.tsx`
Real implementation. Phase 0 was placeholder.

**Required behavior:**
- Props: `{ manifest: CitationManifest; text: string }`.
- Tokenize the text on `[n]` markers; render each with a small superscript badge that's clickable.
- Hover a badge → card showing:
  - Model name
  - Prompt hash + first 60 chars of the prompt that produced it
  - Sub-task role
  - RAG doc snippet (Phase 6 fills `DocSnippet`)
- Click a badge → smooth-scroll/highlight the source node on the DAG.

#### 11. `components/DisagreementCard.tsx`
Side-by-side panel.

**Layout:**
```
┌───────────────────────────────────────┐
│ ⚖️ Disagreement detected              │
├───────────────────────────────────────┤
│ ⬅ A: anthropic:claude-3-5-sonnet     │
│ "Apple's capex fell 8% YoY..."        │
│ [sp_014]                              │
│                                       │
│ ➡ B: openai:gpt-4o                   │
│ "Apple's capex GREW 12% in 2024..."   │
│ [sp_021]                              │
│                                       │
│ ⚖ Judge: openai:gpt-4o-mini          │
│ Verdict: A  (conf 0.82)               │
│ "A cites the 10-K filing; B cites..." │
└───────────────────────────────────────┘
```

A is highlighted with a green border, B with red. Sub-task nodes link back to their DAG positions.

#### 12. Modify `components/StreamPanel.tsx`
Now also renders an "Answer" tab once the fusion stream starts. The synth node's stream is highlighted gold.

#### 13. `lib/sse.ts` extends
```ts
export type VerdictEvent = {
  node_a: string
  node_b: string
  verdict: "A" | "B" | "tie"
  confidence: number
  reasoning: string
  winners: string[]
}

export type ManifestEvent = {
  v: "citation.v1"
  spans: Span[]
}
```

#### 14. Modify `app/playground/page.tsx`
After the run completes:
- Show `FusionViewer` with the final answer text + manifest.
- Render `DisagreementCard` per verdict event.
- Mark all DAG nodes green (`ok`).

### End-to-end verification

1. Restart orchestrator.
2. Open playground, paste:
   > *"Compare NVIDIA's and Apple's 2024 strategy and identify one point where mainstream reporting disagrees."*
3. Click **Run**.
4. **Expected:**
   - Plan has 1–2 nodes (or maybe critic + synthesizer).
   - If 2 streams disagree on a fact, the `DisagreementCard` appears with both verdicts.
   - At the end, the `FusionViewer` renders the final answer with `[1] [2]` inline citations.
   - Hovering `[1]` shows: model, prompt hash, source role, doc snippet (Phase 6).
   - The full manifest arrives in a `manifest` SSE event.

**curl:**
```bash
# Same as Phase 4 /v1/run, just watch the new "verdict" and "manifest" events at the end.
```

### Done checklist

- [ ] `CitationManifest` round-trips through JSON matching schema.
- [ ] `Fuser` runs heuristic mode for single stream, model mode for multi.
- [ ] `Judge.Compare` parses responses and rejects malformed verdicts.
- [ ] Synthesizer output contains `[n]` markers when upstream spans exist.
- [ ] Disagreement triggers a verdict event.
- [ ] Manifest arrives as a single SSE event at the end.
- [ ] `FusionViewer` renders citations with hover-cards.
- [ ] `DisagreementCard` shows side-by-side with judge reasoning.
- [ ] All Go tests pass.
