"use client";

import { useState } from "react";
import { StreamPanel } from "@/components/StreamPanel";
import { DAGCanvas, type Plan, type PlanNode } from "@/components/DAGCanvas";
import { NodeDetailsPanel, type RoutedNode } from "@/components/NodeDetailsPanel";

type PlanState =
  | { kind: "idle" }
  | { kind: "loading" }
  | {
      kind: "ready";
      plan: Plan;
      passthrough: boolean;
      routed: Record<string, RoutedNode>;
    }
  | { kind: "error"; message: string };

export default function PlaygroundPage() {
  const [planState, setPlanState] = useState<PlanState>({ kind: "idle" });
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  async function showPlan(prompt: string) {
    setPlanState({ kind: "loading" });
    setSelectedNodeId(null);
    try {
      const res = await fetch("/api/proxy/v1/plan", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ prompt }),
      });
      if (!res.ok) {
        const body = await res.text();
        setPlanState({ kind: "error", message: `HTTP ${res.status}: ${body}` });
        return;
      }
      const data = await res.json();

      // Index routing decisions by node_id for fast lookup at click time.
      const routed: Record<string, RoutedNode> = {};
      for (const r of data.routed ?? []) {
        routed[r.node_id] = r;
      }

      // Hydrate the orchestrator's snake_case into a Plan object.
      const plan: Plan = {
        version: data.plan?.version ?? "plan.v1",
        nodes: (data.plan?.nodes ?? []).map((n: any): PlanNode => {
          const r = routed[n.id];
          return {
            id: n.id,
            role: n.role,
            payload: n.payload,
            status: "pending",
            needs_rag: !!n.needs_rag,
            task_class: n?.requires?.task_class,
            model: r?.model,
            score: r?.score,
            breakdown: r?.breakdown,
            reason: r?.reason,
          };
        }),
        edges: (data.plan?.edges ?? []).map((e: any) => ({ from: e.from, to: e.to })),
      };
      setPlanState({ kind: "ready", plan, passthrough: !!data.passthrough, routed });
    } catch (e: any) {
      setPlanState({ kind: "error", message: e?.message ?? "Unknown error" });
    }
  }

  const selectedNode =
    planState.kind === "ready" && selectedNodeId
      ? planState.plan.nodes.find((n) => n.id === selectedNodeId) ?? null
      : null;
  const selectedRouted =
    planState.kind === "ready" && selectedNodeId ? planState.routed[selectedNodeId] ?? null : null;

  return (
    <main
      style={{
        maxWidth: 980,
        margin: "2rem auto",
        padding: "0 1.5rem",
        fontFamily: "system-ui, -apple-system, sans-serif",
      }}
    >
      <h1 style={{ marginBottom: 4 }}>🎮 Playground</h1>
      <p style={{ color: "#666", marginTop: 0 }}>
        Phase 3 — Decompose a prompt into a DAG, route each node to a model, then stream a
        single-model answer.
      </p>

      <section style={{ marginTop: "1.25rem", marginBottom: "1.5rem" }}>
        <PlanSection state={planState} onShowPlan={showPlan} onNodeClick={setSelectedNodeId} />
      </section>

      <section style={{ marginTop: "1.5rem" }}>
        <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>📡 Single-model stream</h2>
        <StreamPanel
          apiBase="/api/proxy"
          defaultModel="mock"
          initialPrompt="Explain CAP theorem in 3 sentences."
        />
      </section>

      <p style={{ marginTop: "1.5rem", fontSize: "0.85rem", color: "#888" }}>
        Click a node in the DAG to see the routing breakdown. Phase 4 will add a
        <strong> Run</strong> button that executes the routed plan in parallel.
      </p>

      <p>
        <a href="/">← Back home</a>
      </p>

      {selectedNode && (
        <NodeDetailsPanel
          node={selectedNode}
          routed={selectedRouted}
          onClose={() => setSelectedNodeId(null)}
        />
      )}
    </main>
  );
}

function PlanSection({
  state,
  onShowPlan,
  onNodeClick,
}: {
  state: PlanState;
  onShowPlan: (prompt: string) => void;
  onNodeClick: (id: string) => void;
}) {
  const [prompt, setPrompt] = useState(
    "Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy.",
  );

  return (
    <div>
      <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>🧩 Plan + Router (Phase 3)</h2>

      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        disabled={state.kind === "loading"}
        rows={3}
        style={{
          width: "100%",
          padding: "0.6rem 0.75rem",
          border: "1px solid var(--cf-border)",
          borderRadius: 8,
          fontFamily: "inherit",
          fontSize: "0.95rem",
          resize: "vertical",
          boxSizing: "border-box",
        }}
      />

      <div style={{ display: "flex", gap: 8, marginTop: 8, alignItems: "center", flexWrap: "wrap" }}>
        <button
          onClick={() => onShowPlan(prompt)}
          disabled={state.kind === "loading"}
          style={{
            padding: "0.55rem 1.1rem",
            background: "var(--cf-accent)",
            color: "#fff",
            border: "none",
            borderRadius: 8,
            cursor: state.kind === "loading" ? "wait" : "pointer",
            fontWeight: 600,
            opacity: state.kind === "loading" ? 0.7 : 1,
          }}
        >
          {state.kind === "loading" ? "Decomposing + Routing…" : "Show Plan"}
        </button>

        {state.kind === "ready" && state.passthrough && (
          <span
            style={{
              padding: "0.35rem 0.75rem",
              background: "#fef3c7",
              color: "#92400e",
              border: "1px solid #fcd34d",
              borderRadius: 999,
              fontSize: "0.8rem",
            }}
          >
            ⚠ Decomposer fell back to single-node passthrough
          </span>
        )}

        {state.kind === "ready" && !state.passthrough && (
          <span
            style={{
              padding: "0.35rem 0.75rem",
              background: "#dcfce7",
              color: "#166534",
              border: "1px solid #86efac",
              borderRadius: 999,
              fontSize: "0.8rem",
            }}
          >
            ✓ {state.plan.nodes.length}-node DAG · {Object.keys(state.routed).length} routed
          </span>
        )}

        {state.kind === "ready" && !state.passthrough && (
          <span
            style={{
              padding: "0.35rem 0.75rem",
              background: "#eef2ff",
              color: "#3730a3",
              border: "1px solid #c7d2fe",
              borderRadius: 999,
              fontSize: "0.8rem",
            }}
          >
            💡 Click any node for score breakdown
          </span>
        )}
      </div>

      {/* Plan canvas */}
      <div style={{ marginTop: "1rem" }}>
        {state.kind === "idle" && (
          <div
            style={{
              padding: "2rem",
              border: "1px dashed var(--cf-border)",
              borderRadius: 12,
              textAlign: "center",
              color: "var(--cf-muted)",
            }}
          >
            Click <strong>Show Plan</strong> to decompose + route a prompt.
          </div>
        )}

        {state.kind === "loading" && (
          <div
            style={{
              padding: "2rem",
              border: "1px dashed var(--cf-border)",
              borderRadius: 12,
              textAlign: "center",
              color: "var(--cf-muted)",
            }}
          >
            ⏳ Decomposing + routing…
          </div>
        )}

        {state.kind === "error" && (
          <div
            style={{
              padding: "1rem 1.25rem",
              border: "1px solid #fecaca",
              borderRadius: 8,
              background: "#fef2f2",
              color: "#991b1b",
            }}
          >
            ⚠ {state.message}
          </div>
        )}

        {state.kind === "ready" && (
          <>
            <DAGCanvas plan={state.plan} onNodeClick={onNodeClick} />
            <details style={{ marginTop: 8 }}>
              <summary style={{ cursor: "pointer", color: "#555" }}>Raw plan JSON</summary>
              <pre
                style={{
                  background: "#fafafa",
                  border: "1px solid var(--cf-border)",
                  borderRadius: 8,
                  padding: "0.75rem",
                  marginTop: 8,
                  fontSize: "0.78rem",
                  overflow: "auto",
                  maxHeight: 280,
                }}
              >
                {JSON.stringify(state.plan, null, 2)}
              </pre>
            </details>
          </>
        )}
      </div>
    </div>
  );
}
