// Phase 7: per-run budget controls.
//
// Two numeric inputs: max cost (USD) + max carbon (gCO₂). Both are optional;
// empty string means "no limit" (only the budget gate the user set will
// fire). Values are persisted in localStorage so the playground remembers
// the setting across page reloads.
//
// We emit the budget object upward via `onChange`; the playground page feeds
// it into the streamRun() request body.

"use client";

import { useEffect, useState } from "react";

const LS_KEY = "cf:budget:v1";

export type BudgetValue = {
  max_cost_usd?: number;
  max_carbon_g?: number;
};

export function BudgetSettings({
  value,
  onChange,
}: {
  value: BudgetValue | null;
  onChange: (next: BudgetValue | null) => void;
}) {
  // Two raw strings so empty inputs round-trip as "no limit".
  const [maxCost, setMaxCost] = useState("");
  const [maxCarbon, setMaxCarbon] = useState("");

  // Hydrate from props once on mount, and from localStorage on first load.
  useEffect(() => {
    if (value) {
      setMaxCost(
        typeof value.max_cost_usd === "number" ? String(value.max_cost_usd) : "",
      );
      setMaxCarbon(
        typeof value.max_carbon_g === "number" ? String(value.max_carbon_g) : "",
      );
    } else if (typeof window !== "undefined") {
      const raw = localStorage.getItem(LS_KEY);
      if (raw) {
        try {
          const parsed = JSON.parse(raw) as BudgetValue;
          setMaxCost(
            typeof parsed.max_cost_usd === "number" ? String(parsed.max_cost_usd) : "",
          );
          setMaxCarbon(
            typeof parsed.max_carbon_g === "number" ? String(parsed.max_carbon_g) : "",
          );
          onChange(parsed);
        } catch {
          /* ignore corrupt storage */
        }
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Persist + propagate upward when either input changes.
  useEffect(() => {
    const out: BudgetValue = {};
    const costNum = parseFloat(maxCost);
    const carbonNum = parseFloat(maxCarbon);
    if (maxCost.trim() !== "" && Number.isFinite(costNum)) out.max_cost_usd = costNum;
    if (maxCarbon.trim() !== "" && Number.isFinite(carbonNum)) out.max_carbon_g = carbonNum;
    const next = Object.keys(out).length > 0 ? out : null;
    onChange(next);
    if (typeof window !== "undefined") {
      if (next) localStorage.setItem(LS_KEY, JSON.stringify(next));
      else localStorage.removeItem(LS_KEY);
    }
  }, [maxCost, maxCarbon, onChange]);

  return (
    <div
      data-testid="budget-settings"
      style={{
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "0.75rem 1rem",
        display: "flex",
        flexDirection: "column",
        gap: "0.55rem",
        background: "var(--cf-surface, #0a0a0a)",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
        <strong style={{ fontSize: "0.85rem" }}>💰 Budget</strong>
        <span style={{ fontSize: "0.7rem", color: "var(--cf-muted)" }}>
          leave empty for no limit
        </span>
      </div>
      <div style={{ display: "flex", gap: "0.6rem", flexWrap: "wrap" }}>
        <label style={{ display: "flex", flexDirection: "column", fontSize: "0.75rem", color: "var(--cf-muted)" }}>
          <span>max cost (USD)</span>
          <input
            type="number"
            step="0.0001"
            min="0"
            placeholder="e.g. 0.05"
            value={maxCost}
            onChange={(e) => setMaxCost(e.target.value)}
            style={inputStyle}
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", fontSize: "0.75rem", color: "var(--cf-muted)" }}>
          <span>max carbon (gCO₂)</span>
          <input
            type="number"
            step="0.001"
            min="0"
            placeholder="e.g. 1.0"
            value={maxCarbon}
            onChange={(e) => setMaxCarbon(e.target.value)}
            style={inputStyle}
          />
        </label>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  marginTop: "0.15rem",
  padding: "0.35rem 0.55rem",
  border: "1px solid var(--cf-border)",
  borderRadius: 8,
  background: "var(--cf-bg, #000)",
  color: "var(--cf-text)",
  fontSize: "0.85rem",
  width: 140,
};
