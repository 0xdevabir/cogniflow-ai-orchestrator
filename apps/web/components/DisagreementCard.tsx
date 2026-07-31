"use client";

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

export type DisagreementCardProps = {
  verdict: VerdictEvent;
  onJumpToNode?: (nodeId: string) => void;
};

export function DisagreementCard({ verdict, onJumpToNode }: DisagreementCardProps) {
  const aWins = verdict.verdict === "A";
  const bWins = verdict.verdict === "B";
  const tie = verdict.verdict === "tie";

  return (
    <div
      style={{
        background: "#fff",
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "1.25rem",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 12,
        }}
      >
        <strong style={{ fontSize: "0.95rem" }}>⚖️ Disagreement detected</strong>
        <span
          style={{
            fontSize: "0.75rem",
            padding: "2px 8px",
            borderRadius: 999,
            background: "#fff7ed",
            color: "#9a3412",
            fontWeight: 700,
            textTransform: "uppercase",
          }}
        >
          {verdict.verdict}
        </span>
        <span style={{ fontSize: "0.75rem", color: "#666" }}>
          confidence {verdict.confidence.toFixed(2)}
        </span>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 12,
        }}
      >
        <Side
          side="A"
          nodeId={verdict.node_a}
          model={verdict.model_a}
          claim={verdict.claim_a}
          winners={verdict.winners}
          highlight={aWins}
          tie={tie}
          onJump={onJumpToNode}
        />
        <Side
          side="B"
          nodeId={verdict.node_b}
          model={verdict.model_b}
          claim={verdict.claim_b}
          winners={verdict.winners}
          highlight={bWins}
          tie={tie}
          onJump={onJumpToNode}
        />
      </div>

      <div
        style={{
          marginTop: 12,
          padding: "0.7rem 0.9rem",
          background: "#f9fafb",
          border: "1px solid var(--cf-border)",
          borderRadius: 8,
          fontSize: "0.85rem",
          color: "#374151",
        }}
      >
        <strong>⚖ Judge reasoning</strong>
        <p style={{ margin: "0.4rem 0 0 0", fontStyle: "italic" }}>{verdict.reasoning}</p>
      </div>
    </div>
  );
}

function Side({
  side,
  nodeId,
  model,
  claim,
  winners,
  highlight,
  tie,
  onJump,
}: {
  side: "A" | "B";
  nodeId: string;
  model?: string;
  claim?: string;
  winners: string[];
  highlight: boolean;
  tie: boolean;
  onJump?: (id: string) => void;
}) {
  const borderColor = highlight ? "#22c55e" : tie ? "#94a3b8" : "#ef4444";
  const bgColor = highlight ? "#f0fdf4" : tie ? "#fafafa" : "#fef2f2";

  return (
    <div
      style={{
        border: `2px solid ${borderColor}`,
        background: bgColor,
        borderRadius: 8,
        padding: "0.75rem 0.9rem",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          marginBottom: 6,
          fontSize: "0.85rem",
        }}
      >
        <strong>{side === "A" ? "⬅" : "➡"} {side}</strong>
        <span style={{ color: "#666" }}>{nodeId}</span>
        {onJump && (
          <button
            onClick={() => onJump(nodeId)}
            style={{
              marginLeft: "auto",
              background: "transparent",
              border: "1px solid var(--cf-border)",
              borderRadius: 4,
              padding: "2px 6px",
              fontSize: "0.7rem",
              cursor: "pointer",
              color: "#4338ca",
            }}
          >
            jump
          </button>
        )}
      </div>
      {model && (
        <div style={{ fontSize: "0.75rem", color: "#555", marginBottom: 4 }}>
          🧠 <code>{model}</code>
        </div>
      )}
      <p
        style={{
          margin: 0,
          fontSize: "0.85rem",
          color: "#1f2937",
          whiteSpace: "pre-wrap",
        }}
      >
        {claim || <em style={{ color: "#888" }}>(no claim text)</em>}
      </p>
      {winners.length > 0 && (
        <div
          style={{
            marginTop: 6,
            fontSize: "0.7rem",
            color: "#666",
            fontFamily: "ui-monospace, monospace",
          }}
        >
          winners: {winners.join(", ")}
        </div>
      )}
    </div>
  );
}