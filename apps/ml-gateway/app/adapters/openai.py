"""OpenAI streaming adapter (Python mirror of the Go provider).

Used in Phase 6+ for RAG embeddings and judge calls. The orchestrator
talks to OpenAI directly in Phase 1, so this is a Phase 1 stub kept
for symmetry — the public surface is identical to future real impls.
"""

from __future__ import annotations

import os
from typing import AsyncIterator

from .base import ProviderRequest, ProviderChunk


async def stream(req: ProviderRequest) -> AsyncIterator[ProviderChunk]:
    """Yield SSE-shaped chunks. Default fallback to mock when no key."""
    if not os.getenv("OPENAI_API_KEY"):
        # Mirror the Go mock provider's behavior in Python.
        for i, tok in enumerate(_mock_response(req.prompt)):
            yield ProviderChunk(
                stream_id=req.stream_id,
                node_id=req.node_id,
                text=tok + " ",
                model="mock:echo-v1",
                finish=(i == len(_mock_response(req.prompt)) - 1),
            )
        return

    raise NotImplementedError(
        "Real OpenAI streaming lands in Phase 6 — the orchestrator calls OpenAI directly in Phase 1."
    )


def _mock_response(prompt: str) -> list[str]:
    base = "Python mock — the orchestrator handles streaming in Go for Phase 1"
    return [base[: len(base) // 4], base[len(base) // 4 : len(base) // 2], base[len(base) // 2 :]]
