"use client";

import { useState } from "react";
import { StreamPanel } from "@/components/StreamPanel";
import { DAGCanvas, type Plan, type PlanNode, type NodeStatus } from "@/components/DAGCanvas";
import { MultiStreamPanel } from "@/components/MultiStreamPanel";
import { FusionRunner } from "@/components/FusionRunner";
import { NodeDetailsPanel, type RoutedNode } from "@/components/NodeDetailsPanel";
import { DocUploader } from "@/components/DocUploader";
import { DocList } from "@/components/DocList";
import { EvalBadge } from "@/components/EvalBadge";
import { BudgetSettings, type BudgetValue } from "@/components/BudgetSettings";
import type { DowngradeEvent, EvalEvent, SSEEvent } from "@/lib/sse";

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
  const [runStarted, setRunStarted] = useState(false);
  const [workspace, setWorkspace] = useState("default");
  const [docRefreshKey, setDocRefreshKey] = useState(0);
  // Phase 7: budget + eval state. The EvalBadge is fed from these; reset
  // at the top of every fresh run so the user always sees the new run's
  // score, not stale data from a prior decomposition.
  const [budget, setBudget] = useState<BudgetValue | null>(null);
  const [evalEnabled, setEvalEnabled] = useState(true);
  const [lastEval, setLastEval] = useState<EvalEvent | null>(null);
  const [lastDowngrade, setLastDowngrade] = useState<DowngradeEvent | null>(null);

  function handleBudgetChange(next: BudgetValue | null) {
    setBudget(next);
  }

  async function showPlan(prompt: string) {
    setPlanState({ kind: "loading" });
    setSelectedNodeId(null);
    setRunStarted(false);
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

      const routed: Record<string, RoutedNode> = {};
      for (const r of data.routed ?? []) {
        routed[r.node_id] = r;
      }

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

  function handleStreamEvent(ev: SSEEvent) {
    if (ev.event !== "node_status") return;
    setPlanState((prev) => {
      if (prev.kind !== "ready") return prev;
      const updated: Plan = {
        ...prev.plan,
        nodes: prev.plan.nodes.map((n) =>
          n.id === ev.data.node_id ? { ...n, status: ev.data.status as NodeStatus } : n,
        ),
      };
      return { ...prev, plan: updated };
    });
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
        Phase 7 — Decompose a prompt, route each node to a model, run the DAG in parallel, then fuse + cite. With RAG upload for doc-grounded answers, a per-run budget cascade, and a faithfulness judge.
      </p>

      <section style={{ marginTop: "1.25rem", marginBottom: "1.5rem" }}>
        <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>📚 Knowledge base (RAG)</h2>
        <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 8 }}>
          <label style={{ fontSize: "0.85rem", color: "#666" }}>workspace:</label>
          <input
            value={workspace}
            onChange={(e) => setWorkspace(e.target.value)}
            style={{
              padding: "0.3rem 0.5rem",
              border: "1px solid var(--cf-border)",
              borderRadius: 6,
              fontSize: "0.85rem",
              fontFamily: "ui-monospace, monospace",
              width: 220,
            }}
          />
        </div>
        <DocUploader
          apiBase="/api/proxy"
          workspace={workspace}
          onUploaded={() => setDocRefreshKey((k) => k + 1)}
        />
        <div style={{ marginTop: 12 }}>
          <DocList apiBase="/api/proxy" workspace={workspace} refreshKey={docRefreshKey} />
        </div>
      </section>

      <section style={{ marginTop: "1.25rem", marginBottom: "1.5rem" }}>
        <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>💰 Budget + Eval</h2>
        <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "stretch" }}>
          <div style={{ flex: "1 1 320px", minWidth: 280 }}>
            <BudgetSettings value={budget} onChange={handleBudgetChange} />
          </div>
          <div
            style={{
              flex: "0 0 auto",
              border: "1px solid var(--cf-border)",
              borderRadius: 12,
              padding: "0.75rem 1rem",
              display: "flex",
              flexDirection: "column",
              justifyContent: "center",
              gap: 6,
              background: "var(--cf-surface, #0a0a0a)",
            }}
          >
            <label
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                fontSize: "0.85rem",
                cursor: "pointer",
              }}
            >
              <input
                type="checkbox"
                checked={evalEnabled}
                onChange={(e) => setEvalEnabled(e.target.checked)}
              />
              <span>Run faithfulness judge after DAG</span>
            </label>
            <span style={{ fontSize: "0.7rem", color: "var(--cf-muted)" }}>
              When on, the orchestrator runs an LLM-as-judge and streams an{" "}
              <code>eval</code> event you can read here.
            </span>
          </div>
        </div>
      </section>

      <section style={{ marginTop: "1.25rem", marginBottom: "1.5rem" }}>
        <PlanSection
          state={planState}
          onShowPlan={showPlan}
          onNodeClick={setSelectedNodeId}
        />
      </section>

      {planState.kind === "ready" && runStarted && (
        <section style={{ marginTop: "1.5rem" }}>
          <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>⚡ Parallel streams</h2>
          <MultiStreamPanel
            plan={planState.plan}
            apiBase="/api/proxy"
            onStreamEvent={handleStreamEvent}
            onDowngrade={(ev) => setLastDowngrade(ev)}
            onEval={(ev) => setLastEval(ev)}
            onClose={() => setRunStarted(false)}
            workspace={workspace}
            budget={budget}
            evalEnabled={evalEnabled}
          />
        </section>
      )}

      {planState.kind === "ready" && runStarted && (
        <section style={{ marginTop: "1.5rem" }}>
          <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>🪪 Fusion + citations</h2>
          <FusionRunner
            plan={planState.plan}
            apiBase="/api/proxy"
            onStreamEvent={handleStreamEvent}
            onDowngrade={(ev) => setLastDowngrade(ev)}
            onEval={(ev) => setLastEval(ev)}
            onJumpToNode={setSelectedNodeId}
            workspace={workspace}
            budget={budget}
            evalEnabled={evalEnabled}
          />
        </section>
      )}

      {(lastEval || lastDowngrade) && (
        <section style={{ marginTop: "1.5rem" }}>
          <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>🛡️ Eval</h2>
          <EvalBadge eval={lastEval} downgrade={lastDowngrade} />
        </section>
      )}

      {planState.kind === "ready" && (
        <section style={{ marginTop: "1.5rem" }}>
          {!runStarted && (
            <button
              onClick={() => {
                // Reset eval/downgrade so the badge reflects the new run only.
                setLastEval(null);
                setLastDowngrade(null);
                setRunStarted(true);
              }}
              style={{
                padding: "0.6rem 1.2rem",
                background: "var(--cf-accent)",
                color: "#fff",
                border: "none",
                borderRadius: 8,
                cursor: "pointer",
                fontWeight: 600,
                fontSize: "0.95rem",
              }}
            >
              ▶ Run Plan (parallel + fused)
            </button>
          )}
        </section>
      )}

      <section style={{ marginTop: "1.5rem" }}>
        <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>📡 Single-model stream (Phase 1)</h2>
        <StreamPanel
          apiBase="/api/proxy"
          defaultModel="mock"
          initialPrompt="Explain CAP theorem in 3 sentences."
        />
      </section>

      <p style={{ marginTop: "1.5rem", fontSize: "0.85rem", color: "#888" }}>
        Phase 7 wires a per-run budget cascade + faithfulness eval. Set a tight budget to see model downgrade in action; toggle the judge off to skip eval.
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
      <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>🧩 Plan + Router</h2>

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
