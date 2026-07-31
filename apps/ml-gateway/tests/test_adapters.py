"""Tests for ml-gateway provider adapters (Phase 1)."""

from __future__ import annotations

import os
import pytest

# Force mock fallback (no keys set during tests by default).
os.environ.pop("OPENAI_API_KEY", None)
os.environ.pop("ANTHROPIC_API_KEY", None)

from app.adapters import openai, anthropic, registry  # noqa: E402
from app.adapters.base import ProviderRequest  # noqa: E402


@pytest.mark.asyncio
async def test_openai_mock_emits_finish_chunk() -> None:
    req = ProviderRequest(prompt="explain CAP theorem", model="mock:echo-v1")
    chunks = [c async for c in openai.stream(req)]
    assert chunks, "expected at least one chunk"
    assert chunks[-1].finish, "last chunk must be Finish=True"
    assert all(c.stream_id == "mock:echo-v1" or c.model == "mock:echo-v1" for c in chunks)


@pytest.mark.asyncio
async def test_anthropic_mock_emits_finish_chunk() -> None:
    req = ProviderRequest(prompt="explain CAP theorem", model="mock:echo-v1")
    chunks = [c async for c in anthropic.stream(req)]
    assert chunks
    assert chunks[-1].finish


def test_registry_falls_back_to_mock_on_unknown() -> None:
    fn = registry.get("totally-fake:model-x")
    assert callable(fn)


def test_registry_parses_prefix() -> None:
    fn = registry.get("openai:gpt-4o-mini")
    assert fn is openai.stream
    fn = registry.get("anthropic:claude-3-5-sonnet-latest")
    assert fn is anthropic.stream


def test_registry_list_contains_mock_always() -> None:
    names = registry.list_names()
    assert "mock" in names
