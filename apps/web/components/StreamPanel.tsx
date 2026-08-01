"use client";

import { useEffect, useRef, useState } from "react";
import {
  streamChat,
  type Chunk,
  type NodeStatusEvent,
  type SSEEvent,
} from "@/lib/sse";

type ModelOption = {
  value: string;
  label: string;
};

// Static fallback list — used only when the orchestrator is unreachable.
// When the orchestrator is up, the dropdown is populated dynamically from
// /v1/models so users only see providers that are actually wired.
const FALLBACK_MODEL_OPTIONS: ModelOption[] = [
  { value: "mock:echo-v1", label: "🧪 Mock (no API key needed)" },
];

type ProviderInfo = {
  name: string;
  default_model: string;
  default_label: string;
};

export type StreamPanelProps = {
  apiBase?: string;
  defaultModel?: string;
  initialPrompt?: string;
};

export function StreamPanel({
  apiBase,
  defaultModel = "mock:echo-v1",
  initialPrompt = "Explain CAP theorem in 3 sentences.",
}: StreamPanelProps) {
  const [prompt, setPrompt] = useState(initialPrompt);
  const [model, setModel] = useState(defaultModel);
  const [text, setText] = useState("");
  const [status, setStatus] = useState<"idle" | "running" | "ok" | "error">("idle");
  const [nodeStatus, setNodeStatus] = useState<NodeStatusEvent | null>(null);
  // Actual model that produced chunks — may differ from the requested model
  // when no API key is set and the orchestrator falls back to mock.
  const [respondedModel, setRespondedModel] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [chars, setChars] = useState(0);
  const [latencyMs, setLatencyMs] = useState(0);
  const [startedAt, setStartedAt] = useState<number | null>(null);
  // Dynamic dropdown options sourced from /v1/models; falls back to mock only
  // when the orchestrator is unreachable.
  const [modelOptions, setModelOptions] = useState<ModelOption[]>(FALLBACK_MODEL_OPTIONS);
  const [optionsSource, setOptionsSource] = useState<"loading" | "live" | "fallback">("loading");

  const controllerRef = useRef<AbortController | null>(null);

  // Fetch the registered providers from the orchestrator and rebuild the
  // dropdown to show only those. Runs once on mount.
  useEffect(() => {
    const base = apiBase ?? "/api";
    fetch(`${base}/v1/models`)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((data: { providers: ProviderInfo[] }) => {
        const opts: ModelOption[] = (data.providers ?? [])
          .map((p) => ({ value: p.default_model, label: p.default_label }))
          // Put mock first if present (always shown for dev), then the rest
          // in the order the orchestrator returned.
          .sort((a, b) => {
            const aMock = a.value.startsWith("mock:") ? -1 : 0;
            const bMock = b.value.startsWith("mock:") ? -1 : 0;
            return aMock - bMock;
          });
        if (opts.length > 0) {
          setModelOptions(opts);
          setOptionsSource("live");
          // If the currently selected model isn't in the live list, switch
          // to the orchestrator's first available option.
          if (!opts.some((o) => o.value === model)) {
            setModel(opts[0].value);
          }
        } else {
          setOptionsSource("fallback");
        }
      })
      .catch(() => {
        // Orchestrator down or /v1/models not available — fall back to mock.
        setModelOptions(FALLBACK_MODEL_OPTIONS);
        setOptionsSource("fallback");
      });
    // We deliberately only run on mount; we don't want to clobber the user's
    // selection while they're mid-conversation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // Tick a 100ms timer while running so latency feels alive.
    if (status !== "running") return;
    const t = setInterval(() => {
      if (startedAt) setLatencyMs(Date.now() - startedAt);
    }, 100);
    return () => clearInterval(t);
  }, [status, startedAt]);

  async function handleSend() {
    if (status === "running") return;
    setText("");
    setErrorMsg(null);
    setChars(0);
    setLatencyMs(0);
    setStatus("running");
    setNodeStatus(null);
    setRespondedModel(null);
    const started = Date.now();
    setStartedAt(started);

    const controller = new AbortController();
    controllerRef.current = controller;

    try {
      for await (const ev of streamChat(prompt, {
        model,
        apiBase,
        signal: controller.signal,
      })) {
        handleEvent(ev);
      }
    } catch (err: any) {
      // AbortError is the cancel path; don't surface as an error.
      if (err?.name !== "AbortError") {
        setErrorMsg(err?.message ?? "Unknown error");
        setStatus("error");
      } else {
        setStatus("idle");
      }
    } finally {
      controllerRef.current = null;
    }
  }

  function handleEvent(ev: SSEEvent) {
    switch (ev.event) {
      case "node_status":
        setNodeStatus(ev.data);
        break;
      case "chunk":
        setText((prev) => prev + ev.data.text);
        setChars((n) => n + ev.data.text.length);
        // Capture the actual responding model from the first chunk.
        if (!respondedModel && ev.data.model) {
          setRespondedModel(ev.data.model);
        }
        break;
      case "done":
        setStatus("ok");
        setLatencyMs(Date.now() - (startedAt ?? Date.now()));
        break;
      case "error":
        setErrorMsg(ev.data.message);
        setStatus("error");
        break;
    }
  }

  function handleCancel() {
    controllerRef.current?.abort();
  }

  return (
    <div
      style={{
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "1.25rem",
        background: "#fff",
        display: "flex",
        flexDirection: "column",
        gap: "0.75rem",
      }}
    >
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <strong>📡 Stream</strong>
        <span
          style={{
            padding: "2px 10px",
            borderRadius: 999,
            fontSize: "0.75rem",
            background: statusColor(status).bg,
            color: statusColor(status).fg,
            fontWeight: 600,
          }}
        >
          {status.toUpperCase()}
        </span>
        {/* Show the actual responding model. For single-chat, this comes
            from the first chunk; for multi-node, it comes from node_status. */}
        {(() => {
          const actual = respondedModel ?? nodeStatus?.model;
          if (!actual) return null;
          const requested = model;
          const fellBack = actual !== requested && actual.startsWith("mock:");
          return (
            <span style={{ fontSize: "0.85rem", color: "#666" }}>
              model: <code>{actual}</code>
              {fellBack && (
                <span
                  title="No API key for the requested provider — fell back to mock."
                  style={{
                    marginLeft: 6,
                    padding: "1px 6px",
                    borderRadius: 4,
                    background: "#fef3c7",
                    color: "#92400e",
                    fontSize: "0.7rem",
                    border: "1px solid #fcd34d",
                    cursor: "help",
                  }}
                >
                  fallback
                </span>
              )}
            </span>
          );
        })()}
        {status === "running" && (
          <span style={{ marginLeft: "auto", fontSize: "0.85rem", color: "#666" }}>
            {(latencyMs / 1000).toFixed(1)}s · {chars} chars
          </span>
        )}
        {status === "ok" && (
          <span style={{ marginLeft: "auto", fontSize: "0.85rem", color: "#666" }}>
            {(latencyMs / 1000).toFixed(1)}s · {chars} chars
          </span>
        )}
      </div>

      {/* Output */}
      <div
        style={{
          minHeight: 180,
          maxHeight: 360,
          overflowY: "auto",
          padding: "1rem",
          background: "#fafafa",
          border: "1px solid var(--cf-border)",
          borderRadius: 8,
          fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
          fontSize: "0.95rem",
          whiteSpace: "pre-wrap",
          lineHeight: 1.55,
        }}
      >
        {text || (
          <span style={{ color: "#999" }}>
            {status === "running" ? "▍ streaming…" : "Output will appear here."}
          </span>
        )}
      </div>

      {/* Error */}
      {errorMsg && (
        <div
          style={{
            padding: "0.75rem",
            borderRadius: 8,
            background: "#fef2f2",
            color: "#991b1b",
            border: "1px solid #fecaca",
          }}
        >
          ⚠ {errorMsg}
        </div>
      )}

      {/* Controls */}
      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          disabled={status === "running"}
          rows={3}
          style={{
            padding: "0.6rem 0.75rem",
            border: "1px solid var(--cf-border)",
            borderRadius: 8,
            fontFamily: "inherit",
            fontSize: "0.95rem",
            resize: "vertical",
          }}
        />
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <select
            value={model}
            onChange={(e) => setModel(e.target.value)}
            disabled={status === "running"}
            style={{
              padding: "0.45rem 0.6rem",
              border: "1px solid var(--cf-border)",
              borderRadius: 8,
              fontSize: "0.9rem",
              background: "#fff",
              maxWidth: 320,
            }}
          >
            {modelOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          {optionsSource === "fallback" && (
            <span
              title="Orchestrator unreachable — only mock is available."
              style={{ fontSize: "0.72rem", color: "#92400e" }}
            >
              ⚠ orchestrator offline
            </span>
          )}
          {optionsSource === "live" && (
            <span
              title="Showing providers the orchestrator has registered."
              style={{ fontSize: "0.72rem", color: "#166534" }}
            >
              ✓ {modelOptions.length} provider{modelOptions.length === 1 ? "" : "s"}
            </span>
          )}
          {status !== "running" ? (
            <button
              onClick={handleSend}
              style={{
                padding: "0.55rem 1.1rem",
                background: "var(--cf-accent)",
                color: "#fff",
                border: "none",
                borderRadius: 8,
                cursor: "pointer",
                fontWeight: 600,
              }}
            >
              Send
            </button>
          ) : (
            <button
              onClick={handleCancel}
              style={{
                padding: "0.55rem 1.1rem",
                background: "#ef4444",
                color: "#fff",
                border: "none",
                borderRadius: 8,
                cursor: "pointer",
                fontWeight: 600,
              }}
            >
              Cancel
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function statusColor(s: "idle" | "running" | "ok" | "error") {
  switch (s) {
    case "running":
      return { bg: "#dbeafe", fg: "#1d4ed8" };
    case "ok":
      return { bg: "#dcfce7", fg: "#166534" };
    case "error":
      return { bg: "#fee2e2", fg: "#991b1b" };
    default:
      return { bg: "#f3f4f6", fg: "#374151" };
  }
}