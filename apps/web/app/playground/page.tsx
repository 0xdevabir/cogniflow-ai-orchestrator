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

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Tab = "chat" | "dag";

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

// ---------------------------------------------------------------------------
// Shared design tokens (kept consistent with globals.css)
// ---------------------------------------------------------------------------

const ACCENT = "var(--cf-accent, #6366f1)";
const BORDER = "var(--cf-border, #e5e5e5)";
const MUTED = "var(--cf-muted, #666)";
const SURFACE = "#fafafa";
const CARD_SHADOW = "0 1px 2px rgba(0,0,0,0.04)";

const cardStyle: React.CSSProperties = {
  border: `1px solid ${BORDER}`,
  borderRadius: 12,
  padding: "1.25rem",
  background: "#fff",
  boxShadow: CARD_SHADOW,
};

const sectionTitleStyle: React.CSSProperties = {
  fontSize: "1.05rem",
  margin: 0,
  marginBottom: 4,
  display: "flex",
  alignItems: "center",
  gap: 8,
};

const sectionSubtitleStyle: React.CSSProperties = {
  fontSize: "0.85rem",
  color: MUTED,
  margin: 0,
  marginBottom: 12,
};

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function PlaygroundPage() {
  const [tab, setTab] = useState<Tab>("chat");

  return (
    <main
      style={{
        maxWidth: 1200,
        margin: "1.5rem auto",
        padding: "0 1.5rem",
        fontFamily: "-apple-system, BlinkMacSystemFont, system-ui, sans-serif",
      }}
    >
      <Header />

      {/* Hero: tabbed chat + DAG */}
      <section style={{ marginTop: "1.25rem" }}>
        <Tabs value={tab} onChange={setTab} />
        <div style={{ marginTop: 12 }}>
          {tab === "chat" ? <ChatTab /> : <DagTab />}
        </div>
      </section>

      {/* Advanced: visible but reorganized into 3 cards */}
      <section style={{ marginTop: "1.75rem" }}>
        <h2 style={{ fontSize: "1.05rem", margin: "0 0 4px" }}>
          ⚙️ Advanced
        </h2>
        <p style={{ ...sectionSubtitleStyle, marginBottom: 12 }}>
          Optional knobs — leave at defaults if you're just exploring.
        </p>
        <AdvancedPanel />
      </section>

      <p style={{ marginTop: "2rem", fontSize: "0.85rem", color: "#888" }}>
        <a href="/">← Back home</a>
      </p>
    </main>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function Header() {
  return (
    <header>
      <h1 style={{ margin: 0, fontSize: "1.6rem" }}>🎮 Playground</h1>
      <p style={{ color: MUTED, margin: "4px 0 0", fontSize: "0.95rem" }}>
        One prompt → split into sub-tasks → best model per task → fused answer.
      </p>
    </header>
  );
}

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

function Tabs({ value, onChange }: { value: Tab; onChange: (t: Tab) => void }) {
  const items: { id: Tab; label: string; icon: string; hint: string }[] = [
    {
      id: "chat",
      label: "Single Chat",
      icon: "💬",
      hint: "Pick one model, chat directly.",
    },
    {
      id: "dag",
      label: "Multi-Model DAG",
      icon: "🧩",
      hint: "Decompose, route, run, fuse, judge.",
    },
  ];
  return (
    <div
      role="tablist"
      style={{
        display: "grid",
        gridTemplateColumns: "1fr 1fr",
        gap: 8,
      }}
    >
      {items.map((it) => {
        const active = value === it.id;
        return (
          <button
            key={it.id}
            role="tab"
            aria-selected={active}
            onClick={() => onChange(it.id)}
            style={{
              textAlign: "left",
              padding: "0.85rem 1rem",
              borderRadius: 12,
              border: `1px solid ${active ? ACCENT : BORDER}`,
              background: active ? "rgba(99,102,241,0.06)" : "#fff",
              cursor: "pointer",
              transition: "all 0.12s ease",
            }}
          >
            <div
              style={{
                fontWeight: 700,
                fontSize: "0.95rem",
                color: active ? ACCENT : "inherit",
                display: "flex",
                alignItems: "center",
                gap: 8,
              }}
            >
              <span>{it.icon}</span>
              {it.label}
            </div>
            <div style={{ fontSize: "0.78rem", color: MUTED, marginTop: 2 }}>
              {it.hint}
            </div>
          </button>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 1: Single Chat
// ---------------------------------------------------------------------------

function ChatTab() {
  return (
    <div style={cardStyle}>
      <h2 style={sectionTitleStyle}>
        <span>💬</span> Single-model chat
      </h2>
      <p style={sectionSubtitleStyle}>
        The simple path: pick one provider and model, stream the response.
        With no API keys set, the Mock adapter echoes the prompt.
      </p>
      <StreamPanel
        apiBase="/api/proxy"
        defaultModel="mock:echo-v1"
        initialPrompt="Explain CAP theorem in 3 sentences."
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 2: Multi-Model DAG
// ---------------------------------------------------------------------------

function DagTab() {
  const [planState, setPlanState] = useState<PlanState>({ kind: "idle" });
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [runStarted, setRunStarted] = useState(false);
  const [workspace, setWorkspace] = useState("default");
  const [budget, setBudget] = useState<BudgetValue | null>(null);
  const [evalEnabled, setEvalEnabled] = useState(true);
  const [lastEval, setLastEval] = useState<EvalEvent | null>(null);
  const [lastDowngrade, setLastDowngrade] = useState<DowngradeEvent | null>(null);

  // Step indicator
  const step =
    planState.kind === "idle"
      ? 1
      : planState.kind === "loading"
        ? 1
        : !runStarted
          ? 2
          : 3;

  function resetEval() {
    setLastEval(null);
    setLastDowngrade(null);
  }

  async function showPlan(prompt: string) {
    setPlanState({ kind: "loading" });
    setSelectedNodeId(null);
    setRunStarted(false);
    resetEval();
    try {
      const res = await fetch("/api/proxy/v1/plan", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ prompt, workspace }),
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
    planState.kind === "ready" && selectedNodeId
      ? planState.routed[selectedNodeId] ?? null
      : null;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <Stepper step={step} />

      {/* Step 1: prompt → plan */}
      <div style={cardStyle}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
          <span style={stepBadge(1, step)}>1</span>
          <h2 style={{ ...sectionTitleStyle, margin: 0 }}>
            <span>🧩</span> Decompose & route
          </h2>
        </div>
        <p style={sectionSubtitleStyle}>
          The orchestrator breaks your prompt into a DAG of sub-tasks and picks the
          best model for each (by quality, cost, latency, carbon).
        </p>
        <PromptAndPlan
          state={planState}
          onShowPlan={showPlan}
          onNodeClick={setSelectedNodeId}
        />
      </div>

      {/* Step 2: run plan */}
      {planState.kind === "ready" && (
        <div style={cardStyle}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
            <span style={stepBadge(2, step)}>2</span>
            <h2 style={{ ...sectionTitleStyle, margin: 0 }}>
              <span>⚡</span> Run in parallel
            </h2>
          </div>
          <p style={sectionSubtitleStyle}>
            All sub-task nodes stream side-by-side. The orchestrator respects your
            budget and route decisions.
          </p>
          {!runStarted ? (
            <button
              onClick={() => {
                resetEval();
                setRunStarted(true);
              }}
              style={{
                padding: "0.65rem 1.3rem",
                background: ACCENT,
                color: "#fff",
                border: "none",
                borderRadius: 8,
                cursor: "pointer",
                fontWeight: 600,
                fontSize: "0.95rem",
              }}
            >
              ▶ Run Plan
            </button>
          ) : (
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
          )}
        </div>
      )}

      {/* Step 3: fused answer + eval */}
      {planState.kind === "ready" && runStarted && (
        <div style={cardStyle}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
            <span style={stepBadge(3, step)}>3</span>
            <h2 style={{ ...sectionTitleStyle, margin: 0 }}>
              <span>🪪</span> Fuse & judge
            </h2>
          </div>
          <p style={sectionSubtitleStyle}>
            All node outputs are stitched into one coherent answer with citations.
            If eval is enabled, a faithfulness judge rates the result.
          </p>
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
          {(lastEval || lastDowngrade) && (
            <div style={{ marginTop: 14 }}>
              <EvalBadge eval={lastEval} downgrade={lastDowngrade} />
            </div>
          )}
        </div>
      )}

      {selectedNode && (
        <NodeDetailsPanel
          node={selectedNode}
          routed={selectedRouted}
          onClose={() => setSelectedNodeId(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Stepper — visual progress for the 3 DAG steps
// ---------------------------------------------------------------------------

function Stepper({ step }: { step: number }) {
  const items = [
    { n: 1, label: "Plan" },
    { n: 2, label: "Run" },
    { n: 3, label: "Fuse & Judge" },
  ];
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 6,
        padding: "0.5rem 0.75rem",
        border: `1px solid ${BORDER}`,
        borderRadius: 10,
        background: SURFACE,
        fontSize: "0.82rem",
      }}
    >
      <span style={{ color: MUTED, marginRight: 4 }}>Progress:</span>
      {items.map((it, i) => {
        const done = step > it.n;
        const active = step === it.n;
        return (
          <span key={it.n} style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span
              style={{
                width: 20,
                height: 20,
                borderRadius: 999,
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: "0.7rem",
                fontWeight: 700,
                background: done ? ACCENT : active ? ACCENT : "#fff",
                color: done || active ? "#fff" : MUTED,
                border: `1px solid ${done || active ? ACCENT : BORDER}`,
              }}
            >
              {done ? "✓" : it.n}
            </span>
            <span style={{ color: active ? ACCENT : done ? "#444" : MUTED, fontWeight: active ? 600 : 500 }}>
              {it.label}
            </span>
            {i < items.length - 1 && (
              <span style={{ color: BORDER, margin: "0 4px" }}>→</span>
            )}
          </span>
        );
      })}
    </div>
  );
}

function stepBadge(n: number, current: number): React.CSSProperties {
  const done = current > n;
  const active = current === n;
  return {
    width: 22,
    height: 22,
    borderRadius: 999,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: "0.75rem",
    fontWeight: 700,
    background: done || active ? ACCENT : "#fff",
    color: done || active ? "#fff" : MUTED,
    border: `1px solid ${done || active ? ACCENT : BORDER}`,
    flexShrink: 0,
  };
}

// ---------------------------------------------------------------------------
// Prompt + Plan card
// ---------------------------------------------------------------------------

function PromptAndPlan({
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
      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        disabled={state.kind === "loading"}
        rows={3}
        placeholder="Ask anything — multi-part prompts work best."
        style={{
          width: "100%",
          padding: "0.6rem 0.75rem",
          border: `1px solid ${BORDER}`,
          borderRadius: 8,
          fontFamily: "inherit",
          fontSize: "0.95rem",
          resize: "vertical",
          boxSizing: "border-box",
        }}
      />

      <div
        style={{
          display: "flex",
          gap: 8,
          marginTop: 10,
          alignItems: "center",
          flexWrap: "wrap",
        }}
      >
        <button
          onClick={() => onShowPlan(prompt)}
          disabled={state.kind === "loading"}
          style={{
            padding: "0.55rem 1.1rem",
            background: ACCENT,
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
          <Pill color="#92400e" bg="#fef3c7" border="#fcd34d">
            ⚠ Kept as single node (passthrough)
          </Pill>
        )}

        {state.kind === "ready" && !state.passthrough && (
          <>
            <Pill color="#166534" bg="#dcfce7" border="#86efac">
              ✓ {state.plan.nodes.length}-node DAG
            </Pill>
            <Pill color="#3730a3" bg="#eef2ff" border="#c7d2fe">
              💡 Click any node to see routing
            </Pill>
          </>
        )}
      </div>

      <div style={{ marginTop: 14 }}>
        {state.kind === "idle" && <EmptyHint text="Click Show Plan to decompose your prompt." />}
        {state.kind === "loading" && <EmptyHint text="⏳ Decomposing + routing…" />}
        {state.kind === "error" && <ErrorBox message={state.message} />}

        {state.kind === "ready" && (
          <>
            <DAGCanvas plan={state.plan} onNodeClick={onNodeClick} />
            <details style={{ marginTop: 8 }}>
              <summary style={{ cursor: "pointer", color: MUTED, fontSize: "0.85rem" }}>
                Raw plan JSON
              </summary>
              <pre
                style={{
                  background: SURFACE,
                  border: `1px solid ${BORDER}`,
                  borderRadius: 8,
                  padding: "0.75rem",
                  marginTop: 8,
                  fontSize: "0.78rem",
                  overflow: "auto",
                  maxHeight: 240,
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

// ---------------------------------------------------------------------------
// Advanced panel: 3 cards (RAG, Budget, Eval toggle)
// ---------------------------------------------------------------------------

function AdvancedPanel() {
  const [workspace, setWorkspace] = useState("default");
  const [docRefreshKey, setDocRefreshKey] = useState(0);
  const [budget, setBudget] = useState<BudgetValue | null>(null);
  const [evalEnabled, setEvalEnabled] = useState(true);

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))",
        gap: 12,
      }}
    >
      {/* RAG card */}
      <div style={cardStyle}>
        <h3 style={{ ...sectionTitleStyle, marginBottom: 6 }}>
          <span>📚</span> Knowledge base (RAG)
        </h3>
        <p style={sectionSubtitleStyle}>
          Upload PDFs/text. Nodes that need facts will pull chunks from this workspace.
        </p>
        <label style={{ fontSize: "0.8rem", color: MUTED }}>workspace</label>
        <input
          value={workspace}
          onChange={(e) => setWorkspace(e.target.value)}
          style={{
            display: "block",
            width: "100%",
            marginTop: 4,
            marginBottom: 10,
            padding: "0.4rem 0.55rem",
            border: `1px solid ${BORDER}`,
            borderRadius: 6,
            fontFamily: "ui-monospace, monospace",
            fontSize: "0.85rem",
            boxSizing: "border-box",
          }}
        />
        <DocUploader
          apiBase="/api/proxy"
          workspace={workspace}
          onUploaded={() => setDocRefreshKey((k) => k + 1)}
        />
        <div style={{ marginTop: 10 }}>
          <DocList apiBase="/api/proxy" workspace={workspace} refreshKey={docRefreshKey} />
        </div>
      </div>

      {/* Budget card */}
      <div style={cardStyle}>
        <h3 style={{ ...sectionTitleStyle, marginBottom: 6 }}>
          <span>💰</span> Budget cascade
        </h3>
        <p style={sectionSubtitleStyle}>
          Set a max USD / CO₂ per run. If exceeded, the orchestrator auto-downgrades
          some nodes to cheaper models before running.
        </p>
        <BudgetSettings value={budget} onChange={setBudget} />
      </div>

      {/* Eval card */}
      <div style={cardStyle}>
        <h3 style={{ ...sectionTitleStyle, marginBottom: 6 }}>
          <span>🛡️</span> Faithfulness judge
        </h3>
        <p style={sectionSubtitleStyle}>
          After the fused answer is produced, an LLM-as-judge rates faithfulness
          (0–100%) and flags uncited claims / conflicts.
        </p>
        <label
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            padding: "0.6rem 0.75rem",
            border: `1px solid ${BORDER}`,
            borderRadius: 8,
            background: SURFACE,
            cursor: "pointer",
          }}
        >
          <input
            type="checkbox"
            checked={evalEnabled}
            onChange={(e) => setEvalEnabled(e.target.checked)}
          />
          <span style={{ fontSize: "0.9rem" }}>
            Run eval after every run
          </span>
        </label>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tiny primitives
// ---------------------------------------------------------------------------

function Pill({
  children,
  color,
  bg,
  border,
}: {
  children: React.ReactNode;
  color: string;
  bg: string;
  border: string;
}) {
  return (
    <span
      style={{
        padding: "0.3rem 0.7rem",
        background: bg,
        color,
        border: `1px solid ${border}`,
        borderRadius: 999,
        fontSize: "0.78rem",
      }}
    >
      {children}
    </span>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div
      style={{
        padding: "1.5rem",
        border: `1px dashed ${BORDER}`,
        borderRadius: 12,
        textAlign: "center",
        color: MUTED,
        fontSize: "0.9rem",
      }}
    >
      {text}
    </div>
  );
}

function ErrorBox({ message }: { message: string }) {
  return (
    <div
      style={{
        padding: "0.9rem 1.1rem",
        border: "1px solid #fecaca",
        borderRadius: 8,
        background: "#fef2f2",
        color: "#991b1b",
        fontSize: "0.9rem",
      }}
    >
      ⚠ {message}
    </div>
  );
}
