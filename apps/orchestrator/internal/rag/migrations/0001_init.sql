-- CogniFlow RAG schema (Phase 6).
--
-- Requires the pgvector extension. The pgvector/pgvector Docker image has
-- it pre-installed; for bare Postgres run `CREATE EXTENSION IF NOT EXISTS vector;`
-- manually before starting the orchestrator.
--
-- Two tables: documents (metadata) + chunks (text + vector). The chunks
-- embedding column is `vector(1536)` which matches text-embedding-3-small.
-- Phases 7+ may add additional indexes (HNSW); Phase 8 uses this schema as
-- the source of truth for the self-improving bandit loop.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS documents (
    id           UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    title        TEXT NOT NULL,
    source       TEXT NOT NULL DEFAULT 'upload',
    mime_type    TEXT NOT NULL DEFAULT 'text/plain',
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    chunk_count  INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS documents_workspace_idx
    ON documents (workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS chunks (
    id           UUID PRIMARY KEY,
    doc_id       UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    text         TEXT NOT NULL,
    embedding    vector(1536),
    char_start   INTEGER NOT NULL DEFAULT 0,
    char_end     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS chunks_workspace_idx
    ON chunks (workspace_id);

CREATE INDEX IF NOT EXISTS chunks_doc_idx
    ON chunks (doc_id);