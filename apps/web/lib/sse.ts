// SSE client for the CogniFlow orchestrator.
//
// Two endpoints:
//   POST /v1/chat  → streamChat()    (Phase 1 single-model chat)
//   POST /v1/run   → streamRun()     (Phase 4 multi-node DAG execution)
//
// Both yield typed SSE events as an AsyncGenerator. Wire format must match
// the Go side in apps/orchestrator/internal/api/chat.go and run.go.

export type SpanRef = {
  sub_task_id: string;
  model: string;
  prompt_hash?: string;
  doc_id?: string;
  char_start?: number;
  char_end?: number;
};

export type Chunk = {
  v: "chunk.v1";
  stream_id: string;
  node_id?: string;
  model?: string;
  text: string;
  conf?: number;
  finish?: boolean;
  cite?: SpanRef[];
};

export type NodeStatusEvent = {
  node_id: string;
  status: "pending" | "running" | "ok" | "error" | "debating";
  model?: string;
  score?: number;
  breakdown?: Record<string, number>;
  reason?: string;
  arm_id?: string;
  message?: string;
};

export type PlanEvent = {
  version: string;
  total_nodes: number;
  levels: number;
};

export type FusionStartEvent = {
  synth_node: string;
  streams: number;
};

export type VerdictEvent = {
  node_a: string;
  node_b: string;
  verdict: "A" | "B" | "tie";
  confidence: number;
  reasoning: string;
  winners: string[];
  claim_a?: string;
  claim_b?: string;
  model_a?: string;
  model_b?: string;
};

export type Span = {
  id: string;
  sub_task_id: string;
  model: string;
  text: string;
  doc_id?: string;
  doc_snippet?: string;
  prompt_hash?: string;
  char_start?: number;
  char_end?: number;
};

export type ManifestEvent = {
  v: "citation.v1";
  spans: Span[];
};

export type ChatDone = { ok: boolean; node_id?: string };
export type RunDone = { ok: boolean; total_nodes: number; cancelled: boolean };
export type ChatError = { message: string; code?: string };

// Phase 7: budget cascade event. The planner projected the initial cost
// against the per-run budget and cascade-downgraded some node models to
// cheaper alternatives. The original/new maps are node_id → model string.
export type DowngradeEvent = {
  original: Record<string, string>;
  new: Record<string, string>;
  saved_usd: number;
  saved_g: number;
  final_cost_usd: number;
  final_carbon_g: number;
  downgraded: number;
  unachievable: boolean;
};

// Phase 7: judge output. Faithfulness is 0–100; uncited/conflicts are
// lists of claims the judge couldn't tie back to a citation span.
export type EvalEvent = {
  faithfulness_pct: number;
  uncited_claims: string[];
  conflicts: string[];
  reasoning: string;
  judged_by?: string;
  hallucination_flags?: number;
  cost_usd?: number;
  carbon_g?: number;
  latency_total_ms?: number;
  latency_p95_ms?: number;
  model_mix?: string[];
  per_node?: Record<
    string,
    {
      model: string;
      cost_usd: number;
      carbon_g: number;
      latency_ms: number;
      tokens_in: number;
      tokens_out: number;
      downgraded_from?: string;
    }
  >;
  downgraded?: number;
  unachievable?: boolean;
};

export type SSEEvent =
  | { event: "plan"; data: PlanEvent }
  | { event: "node_status"; data: NodeStatusEvent }
  | { event: "chunk"; data: Chunk }
  | { event: "fusion_start"; data: FusionStartEvent }
  | { event: "fusion"; data: Chunk }
  | { event: "verdict"; data: VerdictEvent }
  | { event: "manifest"; data: ManifestEvent }
  | { event: "downgrade"; data: DowngradeEvent }
  | { event: "eval"; data: EvalEvent }
  | { event: "done"; data: ChatDone | RunDone }
  | { event: "error"; data: ChatError };

export type StreamChatOpts = {
  model?: string;
  apiBase?: string;
  conversationId?: string;
  signal?: AbortSignal;
};

export type StreamRunOpts = {
  apiBase?: string;
  signal?: AbortSignal;
  parallelism?: number;
  workspace?: string;
  budget?: { max_cost_usd?: number; max_carbon_g?: number };
  eval?: boolean;
};

import type { Plan } from "@/components/DAGCanvas";

/** Stream a chat completion over SSE. Yields typed events as they arrive. */
export async function* streamChat(
  prompt: string,
  opts: StreamChatOpts = {},
): AsyncGenerator<SSEEvent> {
  const apiBase = opts.apiBase ?? "/api";
  const model = opts.model ?? "openai:gpt-4o-mini";

  const url = `${apiBase}/v1/chat`;
  let res: Response;
  try {
    res = await fetch(url, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "text/event-stream",
      },
      body: JSON.stringify({
        prompt,
        model,
        conversation_id: opts.conversationId,
      }),
      signal: opts.signal,
    });
  } catch (e: any) {
    // Network / connection error — the orchestrator is probably down.
    yield {
      event: "error",
      data: {
        message: `Can't reach orchestrator at ${apiBase}. Is it running on :8080?`,
        code: "network_error",
      },
    };
    return;
  }

  if (!res.ok) {
    const bodyText = await res.text().catch(() => "");
    yield {
      event: "error",
      data: {
        message: `HTTP ${res.status}: ${res.statusText}${bodyText ? ` — ${bodyText.slice(0, 200)}` : ""}`,
        code: "http_error",
      },
    };
    return;
  }

  yield* readSSE(res);
}

/** Stream a Plan execution over SSE. Yields typed events as they arrive. */
export async function* streamRun(
  plan: Plan,
  opts: StreamRunOpts = {},
): AsyncGenerator<SSEEvent> {
  const apiBase = opts.apiBase ?? "/api";

  const url = `${apiBase}/v1/run`;
  const body: Record<string, any> = {
    plan,
    parallelism: opts.parallelism ?? 4,
  };
  if (opts.workspace) body.workspace = opts.workspace;
  if (opts.budget) {
    const b: Record<string, number> = {};
    if (typeof opts.budget.max_cost_usd === "number")
      b.max_cost_usd = opts.budget.max_cost_usd;
    if (typeof opts.budget.max_carbon_g === "number")
      b.max_carbon_g = opts.budget.max_carbon_g;
    if (Object.keys(b).length > 0) body.budget = b;
  }
  if (typeof opts.eval === "boolean") body.eval = opts.eval;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "text/event-stream",
    },
    body: JSON.stringify(body),
    signal: opts.signal,
  });

  if (!res.ok) {
    yield {
      event: "error",
      data: { message: `HTTP ${res.status}: ${res.statusText}`, code: "http_error" },
    };
    return;
  }

  yield* readSSE(res);
}

/** Shared SSE stream reader. */
async function* readSSE(res: Response): AsyncGenerator<SSEEvent> {
  if (!res.body) {
    yield { event: "error", data: { message: "No response body", code: "no_body" } };
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const raw = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const parsed = parseSSEBlock(raw);
      if (parsed) yield parsed;
    }
  }

  if (buffer.trim()) {
    const parsed = parseSSEBlock(buffer);
    if (parsed) yield parsed;
  }
}

/** Parse one SSE event block (lines between blank lines). */
function parseSSEBlock(block: string): SSEEvent | null {
  let eventName: string | null = null;
  let dataLine: string | null = null;
  for (const line of block.split("\n")) {
    if (line.startsWith("event: ")) {
      eventName = line.slice("event: ".length).trim();
    } else if (line.startsWith("data: ")) {
      dataLine = line.slice("data: ".length);
    } else if (line.startsWith("data:")) {
      dataLine = line.slice("data:".length);
    }
  }
  if (!eventName || dataLine === null) return null;

  let parsed: any;
  try {
    parsed = JSON.parse(dataLine);
  } catch {
    parsed = { raw: dataLine };
  }

  switch (eventName) {
    case "plan":
      return { event: "plan", data: parsed as PlanEvent };
    case "node_status":
      return { event: "node_status", data: parsed as NodeStatusEvent };
    case "chunk":
      return { event: "chunk", data: parsed as Chunk };
    case "fusion_start":
      return { event: "fusion_start", data: parsed as FusionStartEvent };
    case "fusion":
      return { event: "fusion", data: parsed as Chunk };
    case "verdict":
      return { event: "verdict", data: parsed as VerdictEvent };
    case "manifest":
      return { event: "manifest", data: parsed as ManifestEvent };
    case "downgrade":
      return { event: "downgrade", data: parsed as DowngradeEvent };
    case "eval":
      return { event: "eval", data: parsed as EvalEvent };
    case "done":
      return { event: "done", data: parsed as ChatDone | RunDone };
    case "error":
      return { event: "error", data: parsed as ChatError };
    default:
      return null;
  }
}
