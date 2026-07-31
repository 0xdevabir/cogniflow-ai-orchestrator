"use client";

import type { PlanNode } from "@/components/DAGCanvas";

export type RoutedNode = {
  node_id: string;
  model: string;
  score: number;
  breakdown: Record<string, number>;
  reason: string;
};

type Props = {
  node: PlanNode | null;
  routed: RoutedNode | null;
  onClose: () => void;
};

const breakdownLabels: Record<string, string> = {
  bench: "Benchmark",
  cost: "Cost",
  latency: "Latency",
  cost_est: "Est. cost",
  lat_p95: "p95 latency (ms)",
};

export function NodeDetailsPanel({ node, routed, onClose }: Props) {
  if (!node) return null;

  return (
    <div
      style={{
        position: "fixed",
        right: 16,
        top: 16,
        width: 360,
        maxHeight: "calc(100vh - 32px)",
        overflowY: "auto",
        background: "#fff",
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "1rem 1.1rem",
        boxShadow: "0 10px 30px rgba(0,0,0,0.08)",
        zIndex: 50,
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 8,
        }}
      >
        <div>
          <div style={{ fontSize: "0.75rem", color: "#666", textTransform: "uppercase" }}>
            {node.role}
          </div>
          <h3 style={{ margin: 0, fontSize: "1.1rem" }}>{node.id}</h3>
        </div>
        <button
          onClick={onClose}
          style={{
            background: "transparent",
            border: "none",
            fontSize: "1.2rem",
            cursor: "pointer",
            color: "#666",
          }}
          aria-label="Close"
        >
          ✕
        </button>
      </div>

      <div style={{ marginTop: 8, fontSize: "0.85rem", color: "#333" }}>
        <strong>Payload</strong>
        <p
          style={{
            background: "#fafafa",
            border: "1px solid var(--cf-border)",
            borderRadius: 6,
            padding: "0.5rem 0.65rem",
            marginTop: 4,
            whiteSpace: "pre-wrap",
          }}
        >
          {node.payload || <em style={{ color: "#888" }}>(no payload)</em>}
        </p>
      </div>

      {node.needs_rag && (
        <div
          style={{
            marginTop: 8,
            padding: "0.4rem 0.6rem",
            background: "#eff6ff",
            color: "#1d4ed8",
            border: "1px solid #bfdbfe",
            borderRadius: 6,
            fontSize: "0.8rem",
          }}
        >
          📚 Needs RAG context
        </div>
      )}

      {routed ? (
        <>
          <div
            style={{
              marginTop: 12,
              padding: "0.6rem 0.75rem",
              background: "#f0fdf4",
              border: "1px solid #86efac",
              borderRadius: 8,
            }}
          >
            <div
              style={{ fontSize: "0.7rem", color: "#166534", textTransform: "uppercase" }}
            >
              Routed to
            </div>
            <div
              style={{
                fontFamily: "ui-monospace, monospace",
                fontSize: "0.95rem",
                fontWeight: 600,
                marginTop: 2,
                color: "#166534",
              }}
            >
              🧠 {routed.model}
            </div>
            <div style={{ marginTop: 6, fontSize: "0.85rem", color: "#333" }}>
              Score <strong>{routed.score.toFixed(3)}</strong>
            </div>
            {routed.reason && (
              <div
                style={{
                  marginTop: 4,
                  fontSize: "0.78rem",
                  color: "#52525b",
                  fontStyle: "italic",
                }}
              >
                {routed.reason}
              </div>
            )}
          </div>

          {routed.breakdown && Object.keys(routed.breakdown).length > 0 && (
            <div style={{ marginTop: 12 }}>
              <strong style={{ fontSize: "0.85rem" }}>Score breakdown</strong>
              <table
                style={{
                  width: "100%",
                  marginTop: 4,
                  borderCollapse: "collapse",
                  fontSize: "0.8rem",
                }}
              >
                <tbody>
                  {Object.entries(routed.breakdown).map(([k, v]) => (
                    <tr key={k}>
                      <td style={{ padding: "3px 4px", color: "#555" }}>
                        {breakdownLabels[k] ?? k}
                      </td>
                      <td
                        style={{
                          padding: "3px 4px",
                          textAlign: "right",
                          fontFamily: "ui-monospace, monospace",
                        }}
                      >
                        {typeof v === "number" ? v.toFixed(4) : String(v)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      ) : (
        <div
          style={{
            marginTop: 12,
            padding: "0.6rem 0.75rem",
            background: "#fef3c7",
            color: "#92400e",
            border: "1px solid #fcd34d",
            borderRadius: 8,
            fontSize: "0.8rem",
          }}
        >
          ⚠ Router disabled — server has no router wired.
        </div>
      )}
    </div>
  );
}
