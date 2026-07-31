// SSE client for the CogniFlow orchestrator.
//
// Real implementation in Phase 1. Streams `/v1/chat` SSE events and exposes
// them as a typed AsyncGenerator. The wire format must match the Go side in
// `apps/orchestrator/internal/api/chat.go`.

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
  message?: string;
};

export type ChatDone = { ok: boolean; node_id?: string };
export type ChatError = { message: string; code?: string };

export type SSEEvent =
  | { event: "node_status"; data: NodeStatusEvent }
  | { event: "chunk"; data: Chunk }
  | { event: "done"; data: ChatDone }
  | { event: "error"; data: ChatError };

export type StreamChatOpts = {
  model?: string;
  apiBase?: string;
  conversationId?: string;
  signal?: AbortSignal;
};

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

    // SSE events are separated by a blank line.
    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const raw = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);

      const parsed = parseSSEBlock(raw);
      if (parsed) yield parsed;
    }
  }

  // Flush any trailing block.
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
      // Some servers omit the space after data:
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
    case "node_status":
      return { event: "node_status", data: parsed as NodeStatusEvent };
    case "chunk":
      return { event: "chunk", data: parsed as Chunk };
    case "done":
      return { event: "done", data: parsed as ChatDone };
    case "error":
      return { event: "error", data: parsed as ChatError };
    default:
      return null;
  }
}