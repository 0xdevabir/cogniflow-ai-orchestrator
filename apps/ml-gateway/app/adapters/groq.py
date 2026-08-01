"""Groq streaming adapter (Python mirror of the Go provider).

Mirrors `apps/orchestrator/internal/providers/groq.go` for the Python
ml-gateway. The orchestrator talks to Groq directly in Phase 1, so this
is a stub kept for symmetry — the real streaming lands in Phase 6+
alongside the OpenAI/Anthropic adapters.
"""

from __future__ import annotations

import os
from typing import AsyncIterator

from .base import ProviderChunk, ProviderRequest


async def stream(req: ProviderRequest) -> AsyncIterator[ProviderChunk]:
    """Yield SSE-shaped chunks. Falls back to mock when GROQ_API_KEY unset."""
    if not os.getenv("GROQ_API_KEY"):
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
        "Real Groq streaming lands in Phase 6+ — the orchestrator calls Groq directly in Phase 1."
    )


def _mock_response(prompt: str) -> list[str]:
    base = "Python mock — the orchestrator handles Groq streaming in Go for Phase 1"
    return [base[: len(base) // 4], base[len(base) // 4 : len(base) // 2], base[len(base) // 2 :]]
