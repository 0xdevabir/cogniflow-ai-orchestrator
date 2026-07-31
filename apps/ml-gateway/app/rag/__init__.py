"""RAG layer (Phase 6).

  - chunking: 800/200 overlap
  - embeddings: text-embedding-3-small
  - store: pgvector (transactions) + Qdrant stub
  - retrieve: top-k=6 with rerank
  - inject: ===DOC n=== blocks into the sub-task prompt
"""
