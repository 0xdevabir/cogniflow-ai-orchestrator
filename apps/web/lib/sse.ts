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

export type ChatDone = { ok: boolean; node_id?: string };
export type RunDone = { ok: boolean; total_nodes: number; cancelled: boolean };
export type ChatError = { message: string; code?: string };

export type SSEEvent =
  | { event: "plan"; data: PlanEvent }
  | { event: "node_status"; data: NodeStatusEvent }
  | { event: "chunk"; data: Chunk }
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
};

import type { Plan } from "@/components/DAGCanvas";

/** Stream a chat completion over SSE. Yields typed events as they arrive. */
export async function* streamChat(
  prompt: string,
  opts: StreamChatOpts = {},
): AsyncGenerator<SSEEvent> {
  const apiBase = opts.apiBase ?? "/api";
  const model = opts.model ?? "openai:gpt-4o-mini";

  const url = `${apiBase}/chat`;
  const res = await fetch(url, {
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

  if (!res.ok) {
    yield {
      event: "error",
      data: { message: `HTTP ${res.status}: ${res.statusText}`, code: "http_error" },
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

  const url = `${apiBase}/run`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "text/event-stream",
    },
    body: JSON.stringify({ plan, parallelism: opts.parallelism ?? 4 }),
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
    case "done":
      return { event: "done", data: parsed as ChatDone | RunDone };
    case "error":
      return { event: "error", data: parsed as ChatError };
    default:
      return null;
  }
}
