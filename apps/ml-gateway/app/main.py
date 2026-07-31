"""CogniFlow ML Gateway.

Phase 0: hello-world FastAPI app with /healthz.
Phase 1+: provider adapters under app/adapters/, RAG under app/rag/, etc.
"""

from fastapi import FastAPI

app = FastAPI(
    title="CogniFlow ML Gateway",
    version="0.1.0",
    description="Provider adapters, RAG, and eval heuristics for CogniFlow.",
)


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok", "service": "cogniflow-ml-gateway", "phase": 0}
