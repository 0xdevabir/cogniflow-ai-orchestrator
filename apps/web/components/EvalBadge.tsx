// Per-response eval badge (Phase 7).
//
// Renders the result of the faithfulness judge plus run-level cost + carbon +
// model-mix + a "downgraded for budget" indicator. The playground page feeds
// this the latest EvalEvent + DowngradeEvent (or null when the server
// hasn't emitted yet).
//
// Visual goal: dense but readable. The faithful score is the headline; the
// cost/carbon pills live below; downgrade is its own row so it's obvious.

import type { DowngradeEvent, EvalEvent } from "@/lib/sse";

export function EvalBadge({
  eval: ev,
  downgrade,
}: {
  eval: EvalEvent | null;
  downgrade: DowngradeEvent | null;
}) {
  if (!ev) {
    return (
      <div
        style={{
          border: "1px dashed var(--cf-border)",
          borderRadius: 12,
          padding: "0.75rem 1rem",
          color: "var(--cf-muted)",
          fontSize: "0.85rem",
        }}
      >
        🛡️ Eval not yet scored…
      </div>
    );
  }

  const faithfulnessColor =
    ev.faithfulness_pct >= 80
      ? "var(--cf-good, #16a34a)"
      : ev.faithfulness_pct >= 50
        ? "var(--cf-warn, #d97706)"
        : "var(--cf-bad, #dc2626)";

  return (
    <div
      data-testid="eval-badge"
      style={{
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "0.85rem 1rem",
        background: "var(--cf-surface, #0a0a0a)",
        color: "var(--cf-text)",
        display: "flex",
        flexDirection: "column",
        gap: "0.6rem",
        fontSize: "0.9rem",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: "0.75rem", flexWrap: "wrap" }}>
        <span
          aria-label="Faithfulness score"
          style={{
            fontSize: "1.4rem",
            fontWeight: 700,
            color: faithfulnessColor,
          }}
        >
          {Math.round(ev.faithfulness_pct)}%
        </span>
        <span style={{ color: "var(--cf-muted)" }}>faithfulness</span>
        {ev.judged_by && (
          <span style={{ color: "var(--cf-muted)", fontSize: "0.75rem" }}>
            judged by {ev.judged_by}
          </span>
        )}
      </div>

      {ev.reasoning && (
        <div style={{ color: "var(--cf-muted)", fontSize: "0.8rem", lineHeight: 1.4 }}>
          {ev.reasoning}
        </div>
      )}

      {(ev.uncited_claims?.length ?? 0) > 0 && (
        <details style={{ fontSize: "0.8rem" }}>
          <summary style={{ cursor: "pointer", color: "var(--cf-warn, #d97706)" }}>
            ⚠ {ev.uncited_claims.length} uncited claim{ev.uncited_claims.length === 1 ? "" : "s"}
          </summary>
          <ul style={{ margin: "0.4rem 0 0 1.1rem", padding: 0, color: "var(--cf-muted)" }}>
            {ev.uncited_claims.slice(0, 5).map((c, i) => (
              <li key={i}>{c}</li>
            ))}
          </ul>
        </details>
      )}

      {(ev.conflicts?.length ?? 0) > 0 && (
        <details style={{ fontSize: "0.8rem" }}>
          <summary style={{ cursor: "pointer", color: "var(--cf-warn, #d97706)" }}>
            ⚠ {ev.conflicts.length} model conflict{ev.conflicts.length === 1 ? "" : "s"}
          </summary>
          <ul style={{ margin: "0.4rem 0 0 1.1rem", padding: 0, color: "var(--cf-muted)" }}>
            {ev.conflicts.slice(0, 5).map((c, i) => (
              <li key={i}>{c}</li>
            ))}
          </ul>
        </details>
      )}

      <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
        {typeof ev.cost_usd === "number" && (
          <Pill label={`$${ev.cost_usd.toFixed(4)}`} title="Estimated cost" />
        )}
        {typeof ev.carbon_g === "number" && (
          <Pill label={`${ev.carbon_g.toFixed(3)} gCO₂`} title="Estimated carbon" />
        )}
        {typeof ev.latency_total_ms === "number" && (
          <Pill label={`${ev.latency_total_ms} ms`} title="End-to-end latency" />
        )}
        {typeof ev.hallucination_flags === "number" && ev.hallucination_flags > 0 && (
          <Pill label={`${ev.hallucination_flags} flagged`} title="Hallucination flags" />
        )}
      </div>

      {ev.model_mix && ev.model_mix.length > 0 && (
        <div style={{ fontSize: "0.75rem", color: "var(--cf-muted)" }}>
          model mix: {ev.model_mix.join(" · ")}
        </div>
      )}

      {downgrade && downgrade.downgraded > 0 && (
        <div
          data-testid="downgrade-banner"
          style={{
            borderRadius: 8,
            padding: "0.55rem 0.75rem",
            background: "rgba(217, 119, 6, 0.15)",
            color: "var(--cf-warn, #d97706)",
            fontSize: "0.8rem",
            display: "flex",
            alignItems: "center",
            gap: "0.5rem",
            flexWrap: "wrap",
          }}
        >
          <strong>⚡ downgraded for budget</strong>
          <span>
            {downgrade.downgraded} node{downgrade.downgraded === 1 ? "" : "s"} switched models
          </span>
          {downgrade.saved_usd > 0 && <span>saved ${downgrade.saved_usd.toFixed(4)}</span>}
          {downgrade.saved_g > 0 && <span>saved {downgrade.saved_g.toFixed(3)} gCO₂</span>}
          {downgrade.unachievable && (
            <span style={{ color: "var(--cf-bad, #dc2626)" }}>
              ⚠ still over budget (no cheaper alternatives)
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function Pill({ label, title }: { label: string; title: string }) {
  return (
    <span
      title={title}
      style={{
        fontSize: "0.75rem",
        background: "var(--cf-chip, #1f1f1f)",
        color: "var(--cf-muted)",
        padding: "0.15rem 0.5rem",
        borderRadius: 999,
        border: "1px solid var(--cf-border)",
      }}
    >
      {label}
    </span>
  );
}
