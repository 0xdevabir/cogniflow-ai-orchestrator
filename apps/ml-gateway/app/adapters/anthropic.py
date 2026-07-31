"""Anthropic streaming adapter (Python mirror). Phase 1 stub."""

from __future__ import annotations

import os
from typing import AsyncIterator

from .base import ProviderRequest, ProviderChunk


async def stream(req: ProviderRequest) -> AsyncIterator[ProviderChunk]:
    if not os.getenv("ANTHROPIC_API_KEY"):
        for i, tok in enumerate(_mock_response(req.prompt)):
            yield ProviderChunk(
                stream_id=req.stream_id,
                node_id=req.node_id,
                text=tok + " ",
                model="mock:echo-v1",
                finish=(i == len(_mock_response(req.prompt)) - 1),
            )
        return
    raise NotImplementedError("Real Anthropic streaming lands in Phase 6.")


def _mock_response(prompt: str) -> list[str]:
    base = "Python mirror — the orchestrator does streaming in Go for Phase 1"
    return [base[i : i + 20] for i in range(0, len(base), 20)]
