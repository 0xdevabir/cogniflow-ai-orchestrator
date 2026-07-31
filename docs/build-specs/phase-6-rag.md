# Phase 6 — RAG + Memory (pgvector)

## Goal

Users upload documents. Sub-tasks with `needs_rag: true` retrieve relevant chunks and inject them into the prompt. Citations now include the source `DocSnippet`.

**Demo moment:** upload a 30-page NDA PDF. Ask: *"What's the termination clause?"* → orchestrator decomposes → a `factchecker` node with `needs_rag: true` retrieves top-6 chunks, synthesizes an answer with `[1]`-style cites pointing back to the source document and char range in it.

## Prerequisites

- ✅ Phase 5: fusion + citations in place.

## Architecture this phase lays down

```
[UI: drag-drop upload]  ──▶  POST /v1/docs/upload   ──▶  [chunker → embedder → pgvector]
                                                                   │
                                                                   ▼
                                                              chunks table
                                                              embeddings table

[Per-sub-task with needs_rag = true]
   │ rag.Retrieve(ctx, query, workspace_id)
   │   ├─ embed query (text-embedding-3-small)
   │   └─ SELECT top-k=6 by cosine similarity
   │ build prompt prefix:
   │   "===DOC 1=== {snippet}\n===DOC 2=== {...}\n..."
   │ inject as system_msg into the Streamer call
   ▼
[citation.DocSnippet populated from the top-k chunks]
```

## Files to create

### Go — `apps/orchestrator/`

#### 1. `internal/rag/types.go`
```go
package rag

type Chunk struct {
    ID         string
    DocID      string
    DocTitle   string
    Text       string
    Embedding  []float32
    CharStart  int
    CharEnd    int
}

type Document struct {
    ID         string
    WorkspaceID string
    Title      string
    Source     string   // "upload" | "url" | "paste"
    MimeType   string
    Size       int
    CreatedAt  time.Time
}

type Store interface {
    UpsertChunks(ctx context.Context, chunks []Chunk) error
    Retrieve(ctx context.Context, workspaceID, query string, k int) ([]ScoredChunk, error)
    DeleteDoc(ctx context.Context, docID string) error
    ListDocs(ctx context.Context, workspaceID string) ([]Document, error)
}

type ScoredChunk struct {
    Chunk
    Score float64
}

type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Model() string
}
```

#### 2. `internal/rag/pgvector.go`
The pgvector implementation.

```go
package rag

// PostgresStore uses pgvector for similarity search.
// Schema (auto-migrated on startup):
//   CREATE TABLE documents (
//       id UUID PRIMARY KEY,
//       workspace_id TEXT NOT NULL,
//       title TEXT,
//       source TEXT,
//       mime_type TEXT,
//       size_bytes INT,
//       created_at TIMESTAMPTZ DEFAULT NOW()
//   );
//   CREATE TABLE chunks (
//       id UUID PRIMARY KEY,
//       doc_id UUID REFERENCES documents(id) ON DELETE CASCADE,
//       workspace_id TEXT NOT NULL,
//       text TEXT,
//       embedding vector(1536),         -- text-embedding-3-small dim
//       char_start INT,
//       char_end INT
//   );
//   CREATE INDEX chunks_embedding_idx ON chunks USING ivfflat (embedding vector_cosine_ops);
//
// (MVP uses sequential scan; ivfflat index in Phase 8.)
//
// Add migration: apps/orchestrator/internal/rag/migrations/0001_init.sql

type PgStore struct {
    db *pgxpool.Pool
}

func NewPgStore(ctx context.Context, dsn string) (*PgStore, error)

func (s *PgStore) UpsertChunks(ctx context.Context, chunks []Chunk) error
func (s *PgStore) Retrieve(ctx context.Context, workspaceID, query string, k int) ([]ScoredChunk, error)
func (s *PgStore) DeleteDoc(ctx context.Context, docID string) error
func (s *PgStore) ListDocs(ctx context.Context, workspaceID string) ([]Document, error)

func (s *PgStore) AutoMigrate(ctx context.Context) error  // runs migrations on startup
```

Add `github.com/jackc/pgx/v5` to `go.mod`.

#### 3. `internal/rag/openai_embedder.go`
```go
package openaiembed

import "github.com/openai/openai-go"

type Embedder struct {
    client *openai.Client
    model  string  // default "text-embedding-3-small" (1536 dims)
}

func New(client *openai.Client) *Embedder { ... }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // batch up to 100 texts per call
    // returns []float32 of len 1536 per text
}

func (e *Embedder) Model() string { return "openai:text-embedding-3-small" }
```

#### 4. `internal/rag/chunker.go`
```go
package rag

// Chunk splits text into ~800-char chunks with ~200-char overlap.
// Uses sentence boundaries where possible (regex on `[.!?]`).
func Chunk(text string, opts ChunkOpts) []Chunk

type ChunkOpts struct {
    MaxChars       int  // default 800
    OverlapChars   int  // default 200
    MinChunkChars  int  // default 100 (drop tiny chunks)
}
```

For PDFs: use `github.com/ledongthuc/pdf` to extract text. For other MIMEs: best-effort, otherwise `text/plain`.

#### 5. `internal/rag/retrieve.go`
```go
package rag

// RetrieveInjectedPrompt returns a system message with retrieved docs.
// Top-k=6 by default. Rerank with simple score blending later (Phase 8).
func BuildInjectedSystemPrompt(query, workspaceID string, store Store, embedder Embedder) (string, []Chunk, error)
```

Output:
```
The following are retrieved documents relevant to your task. Use them and cite them.

===DOC 1 | doc_abc | "NDA-2024-v3.pdf"===
{chunk text}

===DOC 2 | doc_abc | "NDA-2024-v3.pdf"===
{chunk text}

===TASK===
{original prompt}
```

#### 6. `internal/rag/rag_test.go`
| Test | What |
|---|---|
| `TestChunk_FixedSize` | 2000 chars input → exactly 3 chunks of ~800 with ~200 overlap. |
| `TestChunk_RespectsMinChunk` | Tiny input → dropped. |
| `TestPgStore_UpsertAndRetrieve` | Spin up a test container or use sqlmock; upsert 10 chunks, retrieve top-3, expect correct order by cosine. |
| `TestEmbedder_BatchesAndShape` | Mock HTTP, send 50 texts, expect 1 batched call, all 1536-dim. |
| `TestBuildInjectedPrompt_Structure` | Verify output contains `===DOC 1===` markers. |

Use `testcontainers-go` for an integration test that spins up pgvector (optional — gate behind `INTEGRATION=1`).

#### 7. `internal/api/docs.go`
```go
func (s *Server) handleDocsUpload(w http.ResponseWriter, r *http.Request) {
    // 1. Multipart form-data, file field "file"
    // 2. Extract text per MIME type
    // 3. Chunk + embed + upsert into pgvector
    // 4. Return { doc_id, title, chunk_count }
}

func (s *Server) handleDocsList(w http.ResponseWriter, r *http.Request) {
    // 1. Query store.ListDocs(workspaceID)
    // 2. Return JSON array
}

func (s *Server) handleDocsDelete(w, r)  // DELETE /v1/docs/{id}
```

Endpoints:
- `POST /v1/docs/upload`
- `GET /v1/docs?workspace=<id>`
- `DELETE /v1/docs/:id`

#### 8. Update `internal/dag/executor.go`
- Before calling `Streamer.Stream` for a node with `needs_rag: true`:
  1. Build query from `node.Payload + prompt context`.
  2. Call `BuildInjectedSystemPrompt`.
  3. Replace `req.SystemMsg` with the result.
  4. Tag every emitted Chunk with the source `DocID` → passed to fusion as `Span.DocID`.

#### 9. Add Neo4j-ready interface (stub)
```go
// internal/entity/entity.go — Phase 8 fills with Neo4j.
package entity

type Entity struct {
    ID    string
    Name  string
    Type  string
    DocID string
}

type Store interface {
    Upsert(ctx context.Context, docID string, entities []Entity) error
    Query(ctx context.Context, name string) ([]Entity, error)
}

type NoopStore struct{}

func (NoopStore) Upsert(...) error { return nil }
func (NoopStore) Query(...) ([]Entity, error) { return nil, nil }
```

Wire in main: `s.EntityStore = entity.NoopStore{}` for now.

### Python mirror (optional)
For RAG-heavy work, you may want the embedder and chunker in `apps/ml-gateway/app/rag/`. **Recommendation: keep embeddings + chunking in Go for Phase 6.** Phase 8 may move to FastAPI for richer PDF parsing.

If you do want it in Python:
- `app/rag/chunker.py` (Python `langchain.text_splitter.RecursiveCharacterTextSplitter` for nicer sentence boundaries)
- `app/rag/embeddings.py` (OpenAI Python SDK)
- `app/rag/store.py` (pgvector via `psycopg2-binary` or asyncpg)

### Web — `apps/web/`

#### 10. `components/DocUploader.tsx`
Drag-drop zone in `/playground`.
- Accepts: PDF, TXT, MD.
- POST to `/v1/docs/upload` with workspace id.
- Shows progress bar (chunked XHR for upload progress).
- On success: add to doc list.

#### 11. `components/DocList.tsx`
- Lists docs in workspace (title, size, upload date, chunk count).
- Per-row delete button.

#### 12. Modify `components/FusionViewer.tsx`
- For cites with `DocSnippet` set, the hover-card now shows:
  ```
  Source: NDA-2024-v3.pdf (chunk 4 of 312)
  "...The terminating party shall provide 30 days written notice..."
  ```
  with a clickable link that opens the doc with the char range highlighted.

### Wire-up

In `cmd/server/main.go`:
```go
pg, err := rag.NewPgStore(ctx, os.Getenv("ORCH_DATABASE_URL"))
if err != nil { log.Fatal(err) }
if err := pg.AutoMigrate(ctx); err != nil { log.Fatal(err) }
s.Rag = rag.Service{Store: pg, Embedder: openaiembed.New(openaiClient)}
```

Migrations path: `apps/orchestrator/internal/rag/migrations/*.sql` is loaded at startup (simple `os.ReadDir` + sequential exec).

### End-to-end verification

1. `make up` (already running pgvector from Phase 0).
2. Restart orchestrator — check logs for `pgvector migrations applied`.
3. Open `/playground`. Drag a PDF or TXT into the upload zone.
4. After upload, doc appears in `DocList`. Click → upload timestamp + chunk count.
5. Switch to chat:
   > *"Summarize the termination clause from the document I just uploaded."*
6. Click **Run**.
7. **Expected:**
   - Decomposer creates a `factchecker` node with `needs_rag: true`.
   - DAGCanvas shows the retrieval step (a small "rag" badge in the node card).
   - Final answer cites the doc with `[1]`, hover shows the doc snippet.
   - `manifest` SSE event includes `doc_snippet` fields on relevant spans.

### Done checklist

- [ ] pgvector schema auto-applied on startup.
- [ ] PDF + TXT files chunked (800/200 overlap).
- [ ] Embeddings via `text-embedding-3-small` (1536 dims).
- [ ] `/v1/docs/upload` accepts multipart, returns doc id.
- [ ] `ExecutePlan` injects retrieved docs into needs_rag nodes.
- [ ] Spans carry `doc_id` + `doc_snippet` after Phase 5 fusion runs.
- [ ] DocUploader + DocList work end-to-end in the UI.
- [ ] All Go tests pass.
