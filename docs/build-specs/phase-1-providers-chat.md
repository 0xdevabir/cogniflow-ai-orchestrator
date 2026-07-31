# Phase 1 — Provider Abstraction + Single Happy-Path Chat

## Goal

A user types a prompt into the playground UI. The orchestrator routes it to a single model (OpenAI by default), streams tokens back over SSE, and the `StreamPanel` renders them live. No decomposition, no routing, no fusion — just prove the streaming pipeline works end-to-end.

**Demo moment:** Open `http://localhost:3000/playground`, type *"Explain CAP theorem in 3 sentences"*, watch tokens stream into the panel from OpenAI.

## Prerequisites

- ✅ Phase 0 complete (Go service runs, ml-gateway runs, web loads at `:3000`, infra via `make up`).
- `.env` has `OPENAI_API_KEY` (and ideally `ANTHROPIC_API_KEY`). If missing, the `mock` adapter kicks in automatically.

## Architecture this phase lays down

```
[Next.js StreamPanel]
   │ POST /v1/chat {prompt}
   ▼ EventSource (SSE)
[Go api/chat.go]
   │ provider.Stream(ctx, {prompt, model:"openai"}, sink)
   ▼
[providers/openai.go] ─── real SSE parse ───▶ OpenAI /v1/chat/completions
[providers/anthropic.go] ─── real SSE ───▶ Anthropic /v1/messages
[providers/mock.go] ─── deterministic ───▶ no upstream
```

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/providers/streamer.go` — **the architectural keystone**
Defines the `Streamer` interface + `Chunk` struct + `ChunkSink` interface. Every other provider file implements these.

**Required exports:**

```go
package providers

// Request is what the orchestrator sends to a model.
type Request struct {
    Prompt       string
    Model        string                  // e.g. "openai:gpt-4o-mini", "anthropic:claude-3-5-sonnet"
    SystemMsg    string
    MaxTokens    int
    Temperature  float64
    StreamID     string                  // sub-task id (Phase 4+; for now: "default")
    NodeID       string
}

// Chunk is one streaming unit, vendor-agnostic.
// Wire format MUST match packages/schemas/chunk.schema.json (json tags use snake_case).
type Chunk struct {
    V        string    `json:"v"`         // always "chunk.v1"
    StreamID string    `json:"stream_id"`
    NodeID   string    `json:"node_id,omitempty"`
    Model    string    `json:"model,omitempty"`
    Text     string    `json:"text"`
    Conf     float64   `json:"conf,omitempty"`
    Finish   bool      `json:"finish,omitempty"`
    Cite     []SpanRef `json:"cite,omitempty"`
}

type SpanRef struct {
    SubTaskID  string `json:"sub_task_id"`
    Model      string `json:"model"`
    PromptHash string `json:"prompt_hash,omitempty"`
    DocID      string `json:"doc_id,omitempty"`
    CharStart  int    `json:"char_start,omitempty"`
    CharEnd    int    `json:"char_end,omitempty"`
}

// ChunkSink receives Chunks. Returned error cancels the upstream call.
type ChunkSink interface {
    Send(ctx context.Context, c Chunk) error
}

// Streamer is the vendor-agnostic contract.
type Streamer interface {
    Stream(ctx context.Context, req Request, sink ChunkSink) error
}

// Registry returns a Streamer by name. e.g. "openai", "anthropic", "mock".
type Registry interface {
    Get(model string) (Streamer, error)
}

// NewRegistry builds the default registry from env. Pass nil cfg to use env.
// If OPENAI_API_KEY is set → real OpenAI; else mock. Same for Anthropic.
func NewRegistry(cfg *RegistryConfig) Registry { ... }

type RegistryConfig struct {
    OpenAIKey    string
    AnthropicKey string
    HTTPClient   *http.Client            // optional, for tests
}
```

**Behavior:**
- `Stream` MUST emit a final `Chunk{Finish: true, ...}` regardless of success/error path.
- `Stream` MUST respect `ctx.Done()` (cancel upstream immediately).
- Implementations MUST NOT block longer than `Request.MaxTokens*ctx` budget — error out early if so.

#### 2. `internal/providers/openai.go`
Real implementation. ~80 lines.

**Required:**
- Imports: `github.com/openai/openai-go` (use v2 SDK if available, v1.40+ otherwise — pin in `go.mod`).
- `openai` struct holds `*openai.Client` + http.
- `Stream`:
  1. Call `client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{...}, openai.WithStreamOpt())`.
  2. Iterate the stream: for each `completionChoice`, send `Chunk{Text: choice.Delta.Content, ...}`.
  3. Send `Chunk{Finish: true}` after the final token.
- Map model IDs: `gpt-4o-mini`, `gpt-4o`, `gpt-4-turbo`. Parse from `req.Model` after the `openai:` prefix.

#### 3. `internal/providers/anthropic.go`
Real implementation. ~90 lines.

- Imports: `github.com/anthropics/anthropic-sdk-go`.
- `Stream`:
  1. Call `client.Messages.New(ctx, params, anthropic.WithStream())`.
  2. Event types: handle ONLY `MessageStreamContentBlockDeltaEvent` → extract `delta.Text`.
  3. Skip `MessageStreamMessageStart`, `ContentBlockStart`, `ContentBlockStop`, `MessageDelta`, `MessageStop`.
  4. On `MessageStop`, emit `Finish: true`.
- Model IDs: `claude-3-5-sonnet-latest`, `claude-3-opus-latest`.

#### 4. `internal/providers/mock.go`
Deterministic, no upstream.

- Splits a fixed response into ~5-token chunks with 20ms delay each.
- Pick a response based on prompt hash (4 hardcoded responses, rotate).
- `Chunk.Model = "mock:echo-v1"`.

#### 5. `internal/providers/registry.go`
Builds the default registry. ~50 lines.

```go
func NewRegistry(cfg *RegistryConfig) Registry { ... }
```

- If `cfg.OpenAIKey != ""`, register `&openai{client: openai.NewClient(...)}` keyed `openai`.
- If `cfg.AnthropicKey != ""`, register `&anthropic{...}` keyed `anthropic`.
- Always register `&mock{}` keyed `mock` (fallback).
- `Get(model)` parses `model` as `"<key>:<model_id>"`. If only `openai` → default to `gpt-4o-mini`.

#### 6. `internal/api/chat.go`
HTTP handler for `POST /v1/chat` returning `text/event-stream`.

**Required behavior:**
1. Decode JSON body: `{prompt, model?, conversation_id?}`.
2. Pull model from request, default `"openai:gpt-4o-mini"`.
3. Set response headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`.
4. Pick a flusher: `http.Flusher` from the `ResponseWriter`.
5. Get a `Streamer` from the registry.
6. Call `streamer.Stream(ctx, req, sink)` where `sink` is an adapter that serializes each `Chunk` as SSE:
   ```
   event: chunk
   data: {"v":"chunk.v1","stream_id":"...","text":"..."}
   ```
   followed by `\n\n`.
7. On final chunk emit:
   ```
   event: done
   data: {"ok":true}
   ```
8. On error emit:
   ```
   event: error
   data: {"message":"..."}
   ```
9. Handle client disconnect: when the request context cancels, stop streaming.

**Skeleton:**
```go
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) { ... }
```

#### 7. Update `cmd/server/main.go`
- Build registry with env keys.
- Register `s.handleChat` on `mux.HandleFunc("/v1/chat", s.handleChat)`.
- Wire it up so `/healthz` still works.

### Python — `apps/ml-gateway/`

For Phase 1 the ml-gateway is **standby only** (orchestrator talks to OpenAI/Anthropic directly). But stub the directory:

#### 8. `app/adapters/openai.py`
Mirrors the Go OpenAI adapter but as an async generator. Used by Future phases (RAG, judge).

```python
async def stream(req: "ProviderRequest") -> AsyncIterator[bytes]:
    """Yield SSE-formatted bytes matching chunk.v1 schema."""
    ...
```

#### 9. `app/adapters/__init__.py`
Exports the registry pattern.

#### 10. `tests/test_adapters.py`
Pytest that mocks `httpx` and verifies `mock` adapter emits 5 chunks with `Finish: True` last.

### Web — `apps/web/`

#### 11. `lib/sse.ts`
Real SSE client. Phase 0 threw an error; Phase 1 implements it.

**Required:**
```ts
export type Chunk = {
  v: "chunk.v1"
  stream_id: string
  text: string
  model?: string
  conf?: number
  finish?: boolean
  cite?: SpanRef[]
}

export type SpanRef = {
  sub_task_id: string
  model: string
  prompt_hash?: string
  doc_id?: string
  char_start?: number
  char_end?: number
}

export async function* streamChat(
  prompt: string,
  opts?: { model?: string; apiBase?: string; signal?: AbortSignal }
): AsyncGenerator<{ event: "chunk" | "done" | "error"; data: Chunk | { message?: string } }> {
  // POST /v1/chat, parse text/event-stream
}
```

- Use `fetch()` + ReadableStream + a tiny SSE parser (split on `\n\n`, split on `\n`, key = left of `:`).
- Auto-retry once on transient error (5xx, network). AbortSignal honored.

#### 12. `components/StreamPanel.tsx`
Real implementation. Phase 0 was a placeholder.

**Required behavior:**
- Props: `{ apiBase?: string; model?: string }`.
- Render an input (`<textarea>`), a "Send" button, and a streaming output panel.
- On submit: call `streamChat(prompt)`. For each yielded `{event, data}`:
  - `chunk` → append `data.text` to a buffer, re-render.
  - `done` → mark "complete".
  - `error` → show error message.
- Display: model name, latency-so-far, and total chars streamed (huge for the wow factor).
- Disable the input while streaming.
- Cancel button: triggers `controller.abort()`.

#### 13. Update `app/playground/page.tsx`
Swap the placeholder for:
```tsx
import { StreamPanel } from "@/components/StreamPanel"

export default function PlaygroundPage() {
  return (
    <main style={{ maxWidth: 900, margin: "2rem auto", padding: "0 1.5rem" }}>
      <h1>🎮 Playground</h1>
      <StreamPanel />
    </main>
  )
}
```

### Tests to write

**Go (`apps/orchestrator/internal/providers/`):**

| Test | File | What it checks |
|---|---|---|
| `TestMockStreamer_EmitsFinish` | `mock_test.go` | Mock emits ≥1 chunk with `Finish: true` as the last one. |
| `TestMockStreamer_RespectsCtx` | `mock_test.go` | Cancelling ctx stops the stream within 100ms. |
| `TestRegistry_DefaultOpenAI` | `registry_test.go` | With `OpenAIKey` set, `Get("openai")` returns a non-mock `Streamer`. |
| `TestRegistry_FallbackMock` | `registry_test.go` | With NO keys, `Get("openai")` returns the mock. |
| `TestRegistry_PrefixParse` | `registry_test.go` | `Get("openai:gpt-4o")` → openai streamer. `Get("foo")` → error. |
| `TestStreamer_ChunkJSONShape` | `streamer_test.go` | Chunk marshals to JSON matching `packages/schemas/chunk.schema.json` keys (`v`, `stream_id`, `text`, etc.). |

**Go API (`apps/orchestrator/internal/api/`):**

| Test | File | What it checks |
|---|---|---|
| `TestChatHandler_SetsSSEHeaders` | `chat_test.go` | Response has `Content-Type: text/event-stream`, etc. |
| `TestChatHandler_StreamsUntilDone` | `chat_test.go` | Reading the body yields ≥1 `event: chunk` and one `event: done`. |
| `TestChatHandler_EmitsErrorOnBadJSON` | `chat_test.go` | POST with garbage body emits `event: error`. |

Use `httptest.NewServer` + `httptest.NewRecorder` and a mock registry.

**Web (`apps/web/`):**

If you add tests, use Vitest. Skippable in Phase 1; the manual UI flow is sufficient.

### Wire-up steps

1. `apps/orchestrator/go.mod`: add `github.com/openai/openai-go` and `github.com/anthropics/anthropic-sdk-go` (latest stable).
2. `cd apps/orchestrator && go mod tidy`.
3. `cd apps/web && npm install` (no new deps needed for Phase 1; SSE is hand-rolled).
4. Update root `apps/orchestrator/cmd/server/main.go` to build the registry and wire `/v1/chat`.

### End-to-end verification

```bash
# Terminal 1 — infra
make up

# Terminal 2 — orchestrator
cd apps/orchestrator && OPENAI_API_KEY=sk-... go run ./cmd/server

# Terminal 3 — web
cd apps/web && npm run dev
```

1. Open `http://localhost:3000/playground`.
2. Type *"Explain CAP theorem in 3 sentences"*, click **Send**.
3. **Expected:** tokens stream into the panel one-by-one. After ~3s, status flips to "complete". The model badge shows `openai:gpt-4o-mini`.
4. Switch model to `anthropic:claude-3-5-sonnet-latest` if you have the key; verify response streams from Anthropic.
5. **Mock fallback:** unset both keys, restart orchestrator, send a prompt, verify mock responds with one of its 4 hardcoded answers.

**curl smoke test:**
```bash
curl -N -X POST localhost:8080/v1/chat \
  -H 'content-type: application/json' \
  -d '{"prompt":"hi","model":"openai:gpt-4o-mini"}'
```
Expected: stream of `event: chunk` lines followed by `event: done`.

### Done checklist

- [ ] `Streamer` interface + `Chunk` + `SpanRef` compile and are documented.
- [ ] All provider adapters implement `Streamer` and emit a `Finish: true` chunk last.
- [ ] `NewRegistry` returns the right adapter per env.
- [ ] `POST /v1/chat` returns 200 + `Content-Type: text/event-stream` and streams chunks.
- [ ] Errors during streaming surface as `event: error` (not a silent cut-off).
- [ ] Client disconnect cancels the upstream call within 200ms.
- [ ] `StreamPanel` renders tokens live (no big "wait then dump" feel).
- [ ] All Go tests pass: `cd apps/orchestrator && go test ./...`.
- [ ] Demo works with OpenAI key, Anthropic key, and neither (mock fallback).
