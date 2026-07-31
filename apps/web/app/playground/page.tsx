"use client";

import { useState } from "react";
import { StreamPanel } from "@/components/StreamPanel";
import { DAGCanvas, type Plan, type PlanNode } from "@/components/DAGCanvas";

type PlanState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ready"; plan: Plan; passthrough: boolean }
  | { kind: "error"; message: string };

export default function PlaygroundPage() {
  const [planState, setPlanState] = useState<PlanState>({ kind: "idle" });

  async function showPlan(prompt: string) {
    setPlanState({ kind: "loading" });
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
      // Hydrate the orchestrator's snake_case into a Plan object.
      const plan: Plan = {
        version: data.plan?.version ?? "plan.v1",
        nodes: (data.plan?.nodes ?? []).map(
          (n: any): PlanNode => ({
            id: n.id,
            role: n.role,
            payload: n.payload,
            status: "pending",
            needs_rag: !!n.needs_rag,
          }),
        ),
        edges: (data.plan?.edges ?? []).map((e: any) => ({ from: e.from, to: e.to })),
      };
      setPlanState({ kind: "ready", plan, passthrough: !!data.passthrough });
    } catch (e: any) {
      setPlanState({ kind: "error", message: e?.message ?? "Unknown error" });
    }
  }

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
        Phase 2 — Decompose a prompt into a DAG, then stream a single-model answer.
      </p>

      <section style={{ marginTop: "1.25rem", marginBottom: "1.5rem" }}>
        <PlanSection state={planState} onShowPlan={showPlan} />
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
        Phase 3 will add per-node <strong>model routing</strong>. Phase 4 will add a
        <strong> Run</strong> button that executes the plan in parallel.
      </p>

      <p>
        <a href="/">← Back home</a>
      </p>
    </main>
  );
}

function PlanSection({
  state,
  onShowPlan,
}: {
  state: PlanState;
  onShowPlan: (prompt: string) => void;
}) {
  const [prompt, setPrompt] = useState(
    "Plan a 3-day foodie trip to Tokyo under $500. Also compare NVIDIA's and Apple's 2024 strategy.",
  );

  return (
    <div>
      <h2 style={{ fontSize: "1.1rem", marginBottom: 8 }}>🧩 Plan (decompose a prompt)</h2>

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

      <div style={{ display: "flex", gap: 8, marginTop: 8, alignItems: "center" }}>
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
          {state.kind === "loading" ? "Decomposing…" : "Show Plan"}
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
            ✓ {state.plan.nodes.length}-node DAG
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
            Click <strong>Show Plan</strong> to decompose a prompt.
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
            ⏳ Decomposing…
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
            <DAGCanvas plan={state.plan} />
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