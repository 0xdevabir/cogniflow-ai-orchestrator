"""Common types for provider adapters.

These mirror the Go `providers.Request` / `providers.Chunk` shapes defined
in `apps/orchestrator/internal/providers/streamer.go`. The json fields are
the wire format used in the SSE stream.
"""

from __future__ import annotations

from dataclasses import dataclass, field, asdict
from typing import Optional


@dataclass
class ProviderRequest:
    prompt: str
    model: str
    stream_id: str = "default"
    node_id: str = "default"
    system_msg: str = ""
    max_tokens: int = 0
    temperature: float = 0.0
    top_p: float = 0.0


@dataclass
class SpanRef:
    sub_task_id: str = ""
    model: str = ""
    prompt_hash: str = ""
    doc_id: str = ""
    char_start: int = 0
    char_end: int = 0

    def to_dict(self) -> dict:
        return {k: v for k, v in asdict(self).items() if v}


@dataclass
class ProviderChunk:
    stream_id: str
    node_id: str
    text: str = ""
    model: str = ""
    conf: float = 1.0
    finish: bool = False
    cite: list[SpanRef] = field(default_factory=list)

    def to_dict(self) -> dict:
        d: dict = {
            "v": "chunk.v1",
            "stream_id": self.stream_id,
            "node_id": self.node_id,
            "model": self.model,
            "text": self.text,
        }
        if self.conf != 1.0:
            d["conf"] = self.conf
        if self.finish:
            d["finish"] = True
        if self.cite:
            d["cite"] = [c.to_dict() for c in self.cite]
        return d
