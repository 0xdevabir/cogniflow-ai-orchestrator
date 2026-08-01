"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  fetchDashboardRuns,
  fetchDashboardBandit,
  fetchDashboardModels,
  timeAgo,
  shortModel,
  providerOf,
  providerColor,
  faithfulnessColor,
  type DashboardRuns,
  type DashboardBandit,
  type DashboardModels,
  type DashboardRun,
  type ModelAggregate,
} from "@/lib/dashboard";

type LoadState<T> =
  | { kind: "loading" }
  | { kind: "ok"; data: T }
  | { kind: "error"; message: string };

export default function DashboardPage() {
  const [runs, setRuns] = useState<LoadState<DashboardRuns>>({ kind: "loading" });
  const [bandit, setBandit] = useState<LoadState<DashboardBandit>>({ kind: "loading" });
  const [models, setModels] = useState<LoadState<DashboardModels>>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    fetchDashboardRuns(50)
      .then((data) => {
        if (!cancelled) setRuns({ kind: "ok", data });
      })
      .catch((e) => {
        if (!cancelled)
          setRuns({
            kind: "error",
            message: e?.message ?? "failed to load runs",
          });
      });
    fetchDashboardBandit()
      .then((data) => {
        if (!cancelled) setBandit({ kind: "ok", data });
      })
      .catch((e) => {
        if (!cancelled)
          setBandit({
            kind: "error",
            message: e?.message ?? "failed to load bandit",
          });
      });
    fetchDashboardModels()
      .then((data) => {
        if (!cancelled) setModels({ kind: "ok", data });
      })
      .catch((e) => {
        if (!cancelled)
          setModels({
            kind: "error",
            message: e?.message ?? "failed to load models",
          });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const totals = runs.kind === "ok" ? runs.data.totals : null;
  const runList = runs.kind === "ok" ? runs.data.runs : [];
  const modelAgg = runs.kind === "ok" ? runs.data.model_agg ?? [] : [];

  const overallError =
    runs.kind === "error" && bandit.kind === "error" && models.kind === "error"
      ? runs.kind === "error"
        ? runs.message
        : ""
      : "";

  return (
    <main
      style={{
        maxWidth: 1200,
        margin: "2rem auto",
        padding: "0 1.5rem",
        fontFamily: "system-ui, -apple-system, sans-serif",
      }}
    >
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", flexWrap: "wrap", gap: "0.5rem" }}>
        <h1 style={{ margin: 0 }}>📊 Eval Dashboard</h1>
        <div style={{ display: "flex", gap: "1rem", alignItems: "center" }}>
          <Link href="/playground">← Playground</Link>
          <Link href="/">Home</Link>
        </div>
      </div>
      <p style={{ color: "#666", marginTop: "0.25rem" }}>
        Historical runs, model performance, bandit winners, and reference data — all from local JSONL logs.
      </p>

      {overallError ? <ErrorBanner message={overallError} /> : null}

      {/* Header strip */}
      <section
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
          gap: "0.75rem",
          marginTop: "1.5rem",
        }}
      >
        <StatCard label="Total runs" value={totals?.runs ?? "—"} loading={runs.kind === "loading"} />
        <StatCard
          label="Total cost"
          value={totals ? `$${totals.cost_usd.toFixed(4)}` : "—"}
          loading={runs.kind === "loading"}
        />
        <StatCard
          label="Total carbon"
          value={totals ? `${totals.carbon_g.toFixed(4)} gCO₂` : "—"}
          loading={runs.kind === "loading"}
        />
        <StatCard
          label="Avg latency"
          value={totals ? `${totals.avg_latency_ms.toLocaleString()} ms` : "—"}
          loading={runs.kind === "loading"}
        />
        <StatCard
          label="Avg faithfulness"
          value={totals ? `${totals.avg_faithfulness_pct.toFixed(1)}%` : "—"}
          loading={runs.kind === "loading"}
          accent={totals ? faithfulnessColor(totals.avg_faithfulness_pct) : undefined}
        />
      </section>

      {/* Recent runs table */}
      <Section title="Recent runs" hint={runs.kind === "loading" ? "Loading…" : ""}>
        {runs.kind === "error" ? (
          <ErrorBanner message={runs.message} />
        ) : runList.length === 0 ? (
          <EmptyHint
            title="No runs yet"
            body="Kick off a few prompts in the Playground — every multi-model run gets recorded here automatically."
          />
        ) : (
          <RecentRunsTable runs={runList} />
        )}
      </Section>

      {/* Per-model aggregates */}
      <Section title="Per-model performance" hint={runs.kind === "loading" ? "Loading…" : ""}>
        {modelAgg.length === 0 ? (
          <EmptyHint
            title="No model usage yet"
            body="Once you run a prompt, per-model cost, tokens, latency, and faithfulness appear here."
          />
        ) : (
          <ModelAggTable rows={modelAgg} />
        )}
      </Section>

      {/* Bandit winners */}
      <Section title="Bandit winners" hint={bandit.kind === "loading" ? "Loading…" : ""}>
        <BanditView state={bandit} />
      </Section>

      {/* Reference data (cost + benchmarks) */}
      <Section title="Reference data" hint={models.kind === "loading" ? "Loading…" : ""}>
        <ModelsView state={models} />
      </Section>
    </main>
  );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StatCard({
  label,
  value,
  loading,
  accent,
}: {
  label: string;
  value: string | number;
  loading?: boolean;
  accent?: string;
}) {
  return (
    <div
      style={{
        padding: "1rem 1.1rem",
        border: "1px solid #e5e5e5",
        borderRadius: 12,
        background: "#fafafa",
        minHeight: 86,
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
      }}
    >
      <div style={{ fontSize: "0.8rem", color: "#666", textTransform: "uppercase", letterSpacing: 0.5 }}>
        {label}
      </div>
      <div
        style={{
          fontSize: "1.5rem",
          fontWeight: 600,
          marginTop: 4,
          color: accent ?? "#111",
          opacity: loading ? 0.4 : 1,
        }}
      >
        {value}
      </div>
    </div>
  );
}

function Section({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <section style={{ marginTop: "2rem" }}>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between" }}>
        <h2 style={{ margin: 0, fontSize: "1.25rem" }}>{title}</h2>
        {hint ? <span style={{ color: "#999", fontSize: "0.85rem" }}>{hint}</span> : null}
      </div>
      <div style={{ marginTop: "0.75rem" }}>{children}</div>
    </section>
  );
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div
      style={{
        marginTop: "1rem",
        padding: "0.75rem 1rem",
        borderRadius: 8,
        background: "#fef2f2",
        border: "1px solid #fecaca",
        color: "#991b1b",
      }}
    >
      <strong>Couldn&apos;t reach the dashboard endpoint.</strong>
      <div style={{ marginTop: 4, fontSize: "0.9rem" }}>{message}</div>
      <div style={{ marginTop: 4, fontSize: "0.85rem", color: "#7f1d1d" }}>
        Is the orchestrator running? Try <code>make dev-orchestrator</code> in the repo root.
      </div>
    </div>
  );
}

function EmptyHint({ title, body }: { title: string; body: string }) {
  return (
    <div
      style={{
        padding: "1.5rem",
        border: "1px dashed #d4d4d8",
        borderRadius: 12,
        background: "#fafafa",
        color: "#52525b",
      }}
    >
      <div style={{ fontWeight: 600, color: "#27272a" }}>{title}</div>
      <div style={{ marginTop: 4 }}>{body}</div>
    </div>
  );
}

function RecentRunsTable({ runs }: { runs: DashboardRun[] }) {
  return (
    <div style={{ overflowX: "auto", border: "1px solid #e5e5e5", borderRadius: 12 }}>
      <table
        style={{
          width: "100%",
          borderCollapse: "collapse",
          fontSize: "0.9rem",
          background: "#fff",
        }}
      >
        <thead>
          <tr style={{ background: "#fafafa", textAlign: "left" }}>
            <Th>Prompt</Th>
            <Th>When</Th>
            <Th>Faithfulness</Th>
            <Th>Cost</Th>
            <Th>Latency</Th>
            <Th>Tokens</Th>
            <Th>Model mix</Th>
            <Th>Notes</Th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.run_id} style={{ borderTop: "1px solid #f1f1f1", verticalAlign: "top" }}>
              <Td>
                <div
                  title={r.prompt}
                  style={{
                    maxWidth: 360,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {r.prompt || <em style={{ color: "#9ca3af" }}>(empty)</em>}
                </div>
                <div style={{ fontSize: "0.75rem", color: "#9ca3af" }}>{r.run_id}</div>
              </Td>
              <Td>
                <div>{timeAgo(r.started_at)}</div>
                <div style={{ fontSize: "0.75rem", color: "#9ca3af" }}>{r.workspace || "default"}</div>
              </Td>
              <Td>
                <FaithfulnessBar pct={r.faithfulness_pct} />
                {r.hallucination_flags > 0 ? (
                  <div style={{ fontSize: "0.75rem", color: "#dc2626" }}>
                    ⚠ {r.hallucination_flags} flag{r.hallucination_flags === 1 ? "" : "s"}
                  </div>
                ) : null}
              </Td>
              <Td>
                ${r.cost_usd.toFixed(4)}
                <div style={{ fontSize: "0.75rem", color: "#9ca3af" }}>{r.carbon_g.toFixed(3)} gCO₂</div>
              </Td>
              <Td>
                {r.latency_total_ms.toLocaleString()} ms
                <div style={{ fontSize: "0.75rem", color: "#9ca3af" }}>
                  p95 {r.latency_p95_ms.toLocaleString()} ms
                </div>
              </Td>
              <Td>
                {(r.tokens_in + r.tokens_out).toLocaleString()}
                <div style={{ fontSize: "0.75rem", color: "#9ca3af" }}>
                  in {r.tokens_in.toLocaleString()} · out {r.tokens_out.toLocaleString()}
                </div>
              </Td>
              <Td>
                <ModelMixPills mix={r.model_mix || []} />
              </Td>
              <Td>
                {r.downgraded_nodes > 0 ? (
                  <span style={{ color: "#b45309" }}>↘ {r.downgraded_nodes} downgraded</span>
                ) : (
                  <span style={{ color: "#9ca3af" }}>—</span>
                )}
              </Td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th
      style={{
        padding: "0.6rem 0.75rem",
        fontSize: "0.75rem",
        textTransform: "uppercase",
        letterSpacing: 0.5,
        color: "#6b7280",
        fontWeight: 600,
      }}
    >
      {children}
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return <td style={{ padding: "0.6rem 0.75rem" }}>{children}</td>;
}

function FaithfulnessBar({ pct }: { pct: number }) {
  const width = Math.max(0, Math.min(100, pct));
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
      <div
        style={{
          width: 80,
          height: 8,
          background: "#f3f4f6",
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            width: `${width}%`,
            height: "100%",
            background: faithfulnessColor(pct),
          }}
        />
      </div>
      <span style={{ fontVariantNumeric: "tabular-nums" }}>{pct.toFixed(0)}%</span>
    </div>
  );
}

function ModelMixPills({ mix }: { mix: string[] }) {
  if (!mix.length) return <span style={{ color: "#9ca3af" }}>—</span>;
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
      {mix.map((m, i) => {
        const prov = providerOf(m);
        return (
          <span
            key={`${m}-${i}`}
            title={m}
            style={{
              padding: "2px 8px",
              borderRadius: 999,
              background: providerColor(prov),
              color: prov === "hf" || prov === "ollama" ? "#111" : "#fff",
              fontSize: "0.72rem",
              fontWeight: 500,
              whiteSpace: "nowrap",
            }}
          >
            {shortModel(m)}
          </span>
        );
      })}
    </div>
  );
}

function ModelAggTable({ rows }: { rows: ModelAggregate[] }) {
  return (
    <div style={{ overflowX: "auto", border: "1px solid #e5e5e5", borderRadius: 12 }}>
      <table
        style={{
          width: "100%",
          borderCollapse: "collapse",
          fontSize: "0.9rem",
          background: "#fff",
        }}
      >
        <thead>
          <tr style={{ background: "#fafafa", textAlign: "left" }}>
            <Th>Model</Th>
            <Th>Runs</Th>
            <Th>Mean cost</Th>
            <Th>Mean latency</Th>
            <Th>Tokens in</Th>
            <Th>Tokens out</Th>
            <Th>Mean faithfulness</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const prov = providerOf(r.model);
            return (
              <tr key={r.model} style={{ borderTop: "1px solid #f1f1f1" }}>
                <Td>
                  <span
                    style={{
                      padding: "2px 8px",
                      borderRadius: 999,
                      background: providerColor(prov),
                      color: prov === "hf" || prov === "ollama" ? "#111" : "#fff",
                      fontSize: "0.72rem",
                      fontWeight: 500,
                      marginRight: 6,
                    }}
                  >
                    {prov}
                  </span>
                  <span style={{ fontWeight: 500 }}>{shortModel(r.model)}</span>
                </Td>
                <Td>{r.runs}</Td>
                <Td>${r.cost_usd.toFixed(4)}</Td>
                <Td>{r.mean_latency_ms ? `${r.mean_latency_ms.toLocaleString()} ms` : "—"}</Td>
                <Td>{r.tokens_in.toLocaleString()}</Td>
                <Td>{r.tokens_out.toLocaleString()}</Td>
                <Td>
                  {r.mean_faithfulness_pct > 0 ? (
                    <FaithfulnessBar pct={r.mean_faithfulness_pct} />
                  ) : (
                    <span style={{ color: "#9ca3af" }}>—</span>
                  )}
                </Td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function BanditView({ state }: { state: LoadState<DashboardBandit> }) {
  if (state.kind === "loading") {
    return <div style={{ color: "#9ca3af" }}>Loading bandit data…</div>;
  }
  if (state.kind === "error") {
    return <ErrorBanner message={state.message} />;
  }
  if (!state.data.classes || state.data.classes.length === 0) {
    return (
      <EmptyHint
        title="No bandit feedback yet"
        body="Once enough runs accumulate, each task class picks a winning model and a recommended bench-score boost."
      />
    );
  }

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
        gap: "0.75rem",
      }}
    >
      {state.data.classes.map((c) => (
        <div
          key={c.task_class}
          style={{
            padding: "0.9rem 1rem",
            border: "1px solid #e5e5e5",
            borderRadius: 12,
            background: "#fff",
          }}
        >
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
            <span style={{ fontWeight: 600, fontSize: "0.95rem" }}>{c.task_class}</span>
            <span style={{ fontSize: "0.75rem", color: "#6b7280" }}>{c.models.length} models</span>
          </div>
          {c.winner ? (
            <div style={{ marginTop: 6, fontSize: "0.85rem" }}>
              🏆 <strong>{shortModel(c.winner)}</strong>
              {c.recommended_bench_boost ? (
                <span style={{ color: "#6b7280", marginLeft: 6 }}>
                  (+{(c.recommended_bench_boost * 100).toFixed(1)}% boost)
                </span>
              ) : null}
            </div>
          ) : (
            <div style={{ marginTop: 6, fontSize: "0.85rem", color: "#9ca3af" }}>no winner yet</div>
          )}
          <ul
            style={{
              margin: "0.5rem 0 0",
              padding: 0,
              listStyle: "none",
              fontSize: "0.8rem",
              color: "#374151",
            }}
          >
            {c.models.map((m) => (
              <li
                key={m.model}
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  padding: "2px 0",
                  borderTop: "1px solid #f3f4f6",
                }}
              >
                <span>{shortModel(m.model)}</span>
                <span style={{ color: "#6b7280" }}>
                  n={m.count} · sat {(m.mean_satisfaction * 100).toFixed(0)}% · $
                  {m.mean_cost_usd.toFixed(4)}
                  {m.failures > 0 ? ` · ⚠${m.failures}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function ModelsView({ state }: { state: LoadState<DashboardModels> }) {
  if (state.kind === "loading") {
    return <div style={{ color: "#9ca3af" }}>Loading reference data…</div>;
  }
  if (state.kind === "error") {
    return <ErrorBanner message={state.message} />;
  }
  if (!state.data.models || state.data.models.length === 0) {
    return (
      <EmptyHint
        title="No reference data loaded"
        body="Make sure cost_table.json and benchmarks.json are present in apps/orchestrator/internal/router/data/."
      />
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
      {state.data.models.map((m) => {
        const prov = providerOf(m.model);
        return (
          <details
            key={m.model}
            style={{
              border: "1px solid #e5e5e5",
              borderRadius: 10,
              background: "#fff",
              padding: "0.5rem 0.9rem",
            }}
          >
            <summary
              style={{
                cursor: "pointer",
                fontWeight: 500,
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                gap: 8,
              }}
            >
              <span>
                <span
                  style={{
                    padding: "2px 8px",
                    borderRadius: 999,
                    background: providerColor(prov),
                    color: prov === "hf" || prov === "ollama" ? "#111" : "#fff",
                    fontSize: "0.7rem",
                    fontWeight: 500,
                    marginRight: 6,
                  }}
                >
                  {prov}
                </span>
                {shortModel(m.model)}
              </span>
              <span style={{ color: "#6b7280", fontSize: "0.8rem" }}>
                {typeof m.cost_in_per_m_usd === "number"
                  ? `$${m.cost_in_per_m_usd}/M in`
                  : ""}
                {typeof m.latency_p95_ms === "number" ? ` · ${m.latency_p95_ms}ms p95` : ""}
              </span>
            </summary>
            <div
              style={{
                marginTop: "0.5rem",
                display: "grid",
                gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
                gap: "0.4rem",
                fontSize: "0.85rem",
                color: "#374151",
              }}
            >
              <RefRow label="Cost in /M" value={fmt(m.cost_in_per_m_usd, "$")} />
              <RefRow label="Cost out /M" value={fmt(m.cost_out_per_m_usd, "$")} />
              <RefRow label="Latency p95" value={fmt(m.latency_p95_ms, "", "ms")} />
              <RefRow label="Carbon /M tok" value={fmt(m.carbon_g_per_m, "", "g")} />
              {m.bench_scores && Object.keys(m.bench_scores).length > 0 ? (
                <div style={{ gridColumn: "1 / -1", marginTop: 4 }}>
                  <div
                    style={{
                      fontSize: "0.72rem",
                      textTransform: "uppercase",
                      color: "#6b7280",
                      letterSpacing: 0.5,
                      marginBottom: 4,
                    }}
                  >
                    Bench scores (per task class)
                  </div>
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                    {Object.entries(m.bench_scores)
                      .sort()
                      .map(([tc, sc]) => (
                        <span
                          key={tc}
                          style={{
                            padding: "2px 8px",
                            borderRadius: 999,
                            background: "#f3f4f6",
                            fontSize: "0.78rem",
                          }}
                          title={`${tc}: ${(sc * 100).toFixed(1)}%`}
                        >
                          {tc}: {(sc * 100).toFixed(0)}%
                        </span>
                      ))}
                  </div>
                </div>
              ) : null}
            </div>
          </details>
        );
      })}
    </div>
  );
}

function RefRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <div
        style={{
          fontSize: "0.7rem",
          textTransform: "uppercase",
          color: "#6b7280",
          letterSpacing: 0.5,
        }}
      >
        {label}
      </div>
      <div style={{ fontWeight: 500 }}>{value}</div>
    </div>
  );
}

function fmt(v: number | undefined, prefix = "", suffix = ""): React.ReactNode {
  if (v === undefined || v === null) return <span style={{ color: "#9ca3af" }}>—</span>;
  return (
    <>
      {prefix}
      {v.toFixed(4)}
      {suffix}
    </>
  );
}
