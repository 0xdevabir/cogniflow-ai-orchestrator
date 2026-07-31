// Per-response eval badge (Phase 7+).
//
// Shows: faithfulness %, hallucination flags, total cost, latency, model mix,
// and a "downgraded for budget" indicator if applicable.
export function EvalBadge() {
  return (
    <div
      style={{
        border: "1px dashed var(--cf-border)",
        borderRadius: 12,
        padding: "1rem 2rem",
        textAlign: "center",
        color: "var(--cf-muted)",
        display: "inline-block",
      }}
    >
      🛡️ Eval badge (Phase 7)
    </div>
  );
}
