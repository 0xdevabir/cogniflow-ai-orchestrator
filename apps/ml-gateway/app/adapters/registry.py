"""Provider registry (Python mirror of `providers.NewRegistry`)."""

from __future__ import annotations

import os
from typing import Callable, AsyncIterator, TYPE_CHECKING

from . import openai as openai_adapter
from . import anthropic as anthropic_adapter
from . import groq as groq_adapter

if TYPE_CHECKING:
    from .base import ProviderRequest, ProviderChunk


ProviderFn = Callable[["ProviderRequest"], AsyncIterator["ProviderChunk"]]


_REGISTRY: dict[str, ProviderFn] = {
    "openai": openai_adapter.stream,
    "anthropic": anthropic_adapter.stream,
    "groq": groq_adapter.stream,
    "mock": openai_adapter.stream,  # mock routes to the same fallback stream
}


def get(model: str) -> ProviderFn:
    prefix, _ = model.split(":", 1) if ":" in model else (model, "")
    if not prefix:
        prefix = "mock"
    fn = _REGISTRY.get(prefix)
    if fn is None:
        return _REGISTRY["mock"]
    return fn


def list_names() -> list[str]:
    """Return available providers based on env (mirrors Go behavior)."""
    out = ["mock"]
    if os.getenv("OPENAI_API_KEY"):
        out.append("openai")
    if os.getenv("ANTHROPIC_API_KEY"):
        out.append("anthropic")
    if os.getenv("MISTRAL_API_KEY"):
        out.append("mistral")
    if os.getenv("HF_API_KEY"):
        out.append("hf")
    if os.getenv("OLLAMA_BASE_URL"):
        out.append("ollama")
    if os.getenv("GROQ_API_KEY"):
        out.append("groq")
    return sorted(set(out))
