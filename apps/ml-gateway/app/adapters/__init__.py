"""Provider adapters — one module per upstream.

Each adapter implements a common interface (openai.AsyncStream, anthropic.AsyncStream,
ollama NDJSON loop, mistral wrapper, hf inference client, mock) and yields
``Chunk`` objects that match the Go-side ``Chunk`` schema in
``apps/orchestrator/internal/providers/streamer.go``.

Phases:
    1 — openai + anthropic (real), mock (deterministic).
    6 — adds ollama + mistral + hf stubs that return the same shape.
"""
