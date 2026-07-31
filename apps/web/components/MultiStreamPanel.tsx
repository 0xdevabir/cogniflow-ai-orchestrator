"use client";

import { useEffect, useRef, useState } from "react";
import {
  streamRun,
  type Chunk,
  type NodeStatusEvent,
  type SSEEvent,
  type PlanEvent,
} from "@/lib/sse";
import type { Plan, NodeStatus } from "@/components/DAGCanvas";

type StreamRow = {
  nodeId: string;
  status: NodeStatus;
  model?: string;
  text: string;
  startedAt?: number;
  finishedAt?: number;
  error?: string;
};

export type MultiStreamPanelProps = {
  plan: Plan;
  apiBase?: string;
  onStreamEvent?: (ev: SSEEvent) => void;
  onClose?: () => void;
};

export function MultiStreamPanel({
  plan,
  apiBase,
  onStreamEvent,
  onClose,
}: MultiStreamPanelProps) {
  const [rows, setRows] = useState<Record<string, StreamRow>>(() => {
    const init: Record<string, StreamRow> = {};
    for (const n of plan.nodes) {
      init[n.id] = { nodeId: n.id, status: "pending", text: "" };
    }
    return init;
  });
  const [running, setRunning] = useState(false);
  const [planEvent, setPlanEvent] = useState<PlanEvent | null>(null);
  const [done, setDone] = useState<{ ok: boolean; cancelled: boolean } | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const controllerRef = useRef<AbortController | null>(null);

  // Reset when plan changes.
  useEffect(() => {
    const init: Record<string, StreamRow> = {};
    for (const n of plan.nodes) {
      init[n.id] = { nodeId: n.id, status: "pending", text: "" };
    }
    setRows(init);
    setDone(null);
    setErrorMsg(null);
    setPlanEvent(null);
  }, [plan]);

  function applyStatus(ev: NodeStatusEvent) {
    setRows((prev) => {
      const r = prev[ev.node_id] ?? { nodeId: ev.node_id, status: "pending", text: "" };
      const next: StreamRow = { ...r, status: ev.status };
      if (ev.status === "running") {
        next.startedAt = Date.now();
        next.model = ev.model ?? next.model;
      }
      if (ev.status === "ok") {
        next.finishedAt = Date.now();
      }
      if (ev.status === "error") {
        next.error = ev.message;
        next.finishedAt = Date.now();
      }
      return { ...prev, [ev.node_id]: next };
    });
  }

  function applyChunk(c: Chunk) {
    const nodeId = c.node_id ?? c.stream_id;
    if (!nodeId) return;
    setRows((prev) => {
      const r = prev[nodeId] ?? { nodeId, status: "running", text: "" };
      const next: StreamRow = { ...r, text: r.text + c.text };
      if (c.model) next.model = c.model;
      return { ...prev, [nodeId]: next };
    });
  }

  async function handleRun() {
    if (running) return;
    const controller = new AbortController();
    controllerRef.current = controller;
    setRunning(true);
    setDone(null);
    setErrorMsg(null);
    // Reset rows.
    setRows((prev) => {
      const init: Record<string, StreamRow> = {};
      for (const k of Object.keys(prev)) {
        init[k] = { nodeId: prev[k].nodeId, status: "pending", text: "" };
      }
      return init;
    });
    try {
      for await (const ev of streamRun(plan, {
        apiBase,
        signal: controller.signal,
        parallelism: 4,
      })) {
        onStreamEvent?.(ev);
        switch (ev.event) {
          case "plan":
            setPlanEvent(ev.data);
            break;
          case "node_status":
            applyStatus(ev.data);
            break;
          case "chunk":
            applyChunk(ev.data);
            break;
          case "done":
            setDone({ ok: (ev.data as any).ok, cancelled: !!(ev.data as any).cancelled });
            break;
          case "error":
            setErrorMsg(ev.data.message);
            break;
        }
      }
    } catch (err: any) {
      if (err?.name !== "AbortError") {
        setErrorMsg(err?.message ?? "Unknown error");
      }
    } finally {
      setRunning(false);
      controllerRef.current = null;
    }
  }

  function handleCancel() {
    controllerRef.current?.abort();
  }

  const totalNodes = plan.nodes.length;
  const okCount = Object.values(rows).filter((r) => r.status === "ok").length;
  const errorCount = Object.values(rows).filter((r) => r.status === "error").length;
  const runningCount = Object.values(rows).filter((r) => r.status === "running").length;
  const progressPct = totalNodes > 0 ? ((okCount + errorCount) / totalNodes) * 100 : 0;

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
      <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
        <strong>⚡ Run</strong>
        <span
          style={{
            padding: "2px 10px",
            borderRadius: 999,
            fontSize: "0.75rem",
            background: running ? "#dbeafe" : done ? "#dcfce7" : "#f3f4f6",
            color: running ? "#1d4ed8" : done ? "#166534" : "#374151",
            fontWeight: 600,
          }}
        >
          {running ? "RUNNING" : done ? (done.ok ? "OK" : "CANCELLED") : "IDLE"}
        </span>
        {planEvent && (
          <span style={{ fontSize: "0.8rem", color: "#666" }}>
            {planEvent.total_nodes} nodes · {planEvent.levels} levels
          </span>
        )}
        <span style={{ marginLeft: "auto", fontSize: "0.85rem", color: "#666" }}>
          {okCount}/{totalNodes} ok · {runningCount} running · {errorCount} error
        </span>
        {onClose && !running && (
          <button
            onClick={onClose}
            style={{
              padding: "0.3rem 0.6rem",
              background: "transparent",
              border: "1px solid var(--cf-border)",
              borderRadius: 6,
              cursor: "pointer",
              fontSize: "0.8rem",
            }}
          >
            Hide
          </button>
        )}
      </div>

      {/* Progress bar */}
      <div
        style={{
          width: "100%",
          height: 6,
          background: "#f3f4f6",
          borderRadius: 999,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            width: `${progressPct}%`,
            height: "100%",
            background: errorCount > 0 ? "#f59e0b" : "#22c55e",
            transition: "width 0.3s ease-out",
          }}
        />
      </div>

      {/* Per-node rows */}
      <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: 320, overflowY: "auto" }}>
        {plan.nodes.map((n) => {
          const r = rows[n.id] ?? { nodeId: n.id, status: "pending" as NodeStatus, text: "" };
          const c = statusColor(r.status);
          return (
            <div
              key={n.id}
              style={{
                border: `1px solid ${c.border}`,
                background: c.bg,
                borderRadius: 8,
                padding: "0.5rem 0.75rem",
                display: "flex",
                flexDirection: "column",
                gap: 4,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: "0.85rem" }}>
                <strong>{n.id}</strong>
                <span
                  style={{
                    fontSize: "0.65rem",
                    textTransform: "uppercase",
                    padding: "1px 8px",
                    borderRadius: 999,
                    background: c.pill,
                    color: c.pillFg,
                    fontWeight: 700,
                  }}
                >
                  {r.status}
                </span>
                {r.model && (
                  <span style={{ fontSize: "0.75rem", color: "#555" }}>
                    🧠 <code>{r.model}</code>
                  </span>
                )}
                {r.startedAt && r.finishedAt && (
                  <span style={{ marginLeft: "auto", fontSize: "0.75rem", color: "#666" }}>
                    {((r.finishedAt - r.startedAt) / 1000).toFixed(1)}s
                  </span>
                )}
              </div>
              {r.text && (
                <div
                  style={{
                    fontSize: "0.82rem",
                    color: "#222",
                    fontFamily: "ui-monospace, SFMono-Regular, monospace",
                    whiteSpace: "pre-wrap",
                    maxHeight: 90,
                    overflow: "hidden",
                  }}
                >
                  {r.text.slice(0, 400)}
                  {r.text.length > 400 && "…"}
                </div>
              )}
              {r.error && (
                <div style={{ fontSize: "0.8rem", color: "#991b1b" }}>⚠ {r.error}</div>
              )}
            </div>
          );
        })}
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
      <div style={{ display: "flex", gap: 8 }}>
        {!running ? (
          <button
            onClick={handleRun}
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
            ▶ Run Plan
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
  );
}

function statusColor(s: NodeStatus): {
  bg: string;
  border: string;
  pill: string;
  pillFg: string;
} {
  switch (s) {
    case "running":
      return { bg: "#eff6ff", border: "#3b82f6", pill: "#dbeafe", pillFg: "#1d4ed8" };
    case "ok":
      return { bg: "#f0fdf4", border: "#22c55e", pill: "#dcfce7", pillFg: "#166534" };
    case "error":
      return { bg: "#fef2f2", border: "#ef4444", pill: "#fee2e2", pillFg: "#991b1b" };
    case "debating":
      return { bg: "#fff7ed", border: "#f97316", pill: "#ffedd5", pillFg: "#9a3412" };
    default:
      return { bg: "#fafafa", border: "#d4d4d8", pill: "#f4f4f5", pillFg: "#52525b" };
  }
}
