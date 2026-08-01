// Typed fetchers for the orchestrator's dashboard endpoints.
//
// Three GETs:
//   /v1/dashboard/runs    → recent eval-log rows + totals
//   /v1/dashboard/bandit  → per-(task_class, model) bandit stats
//   /v1/dashboard/models  → static reference data (cost + benchmarks)
//
// All endpoints are cheap JSON reads (no DB) and are proxied by Next.js via
// /api/proxy/:path*.

export type DashboardRun = {
  run_id: string;
  prompt: string;
  workspace: string;
  started_at: string;
  finished_at: string;
  cost_usd: number;
  carbon_g: number;
  latency_total_ms: number;
  latency_p95_ms: number;
  faithfulness_pct: number;
  hallucination_flags: number;
  model_mix: string[];
  tokens_in: number;
  tokens_out: number;
  judged_by?: string;
  downgraded_nodes: number;
};

export type DashboardTotals = {
  runs: number;
  cost_usd: number;
  carbon_g: number;
  avg_latency_ms: number;
  avg_faithfulness_pct: number;
};

/** Server-aggregated per-model stats from the runs endpoint. */
export type ModelAggregate = {
  model: string;
  runs: number;
  tokens_in: number;
  tokens_out: number;
  cost_usd: number;
  mean_latency_ms: number;
  mean_faithfulness_pct: number;
};

export type DashboardRuns = {
  runs: DashboardRun[];
  model_agg?: ModelAggregate[];
  totals: DashboardTotals;
};

export type BanditModelStat = {
  model: string;
  count: number;
  mean_satisfaction: number;
  mean_latency_ms: number;
  mean_cost_usd: number;
  failures: number;
};

export type BanditClass = {
  task_class: string;
  winner: string;
  recommended_bench_boost: number;
  models: BanditModelStat[];
};

export type DashboardBandit = {
  classes: BanditClass[];
  total_events: number;
  generated_at?: string;
  path?: string;
};

export type DashboardModelRow = {
  model: string;
  cost_in_per_m_usd?: number;
  cost_out_per_m_usd?: number;
  latency_p95_ms?: number;
  carbon_g_per_m?: number;
  bench_scores?: Record<string, number>;
};

export type DashboardModels = {
  models: DashboardModelRow[];
  count: number;
};

async function getJSON<T>(path: string, apiBase = "/api/proxy"): Promise<T> {
  const res = await fetch(`${apiBase}${path}`, {
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status} fetching ${path}: ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export function fetchDashboardRuns(limit = 20, apiBase = "/api/proxy") {
  return getJSON<DashboardRuns>(
    `/v1/dashboard/runs?limit=${limit}`,
    apiBase,
  );
}

export function fetchDashboardBandit(apiBase = "/api/proxy") {
  return getJSON<DashboardBandit>("/v1/dashboard/bandit", apiBase);
}

export function fetchDashboardModels(apiBase = "/api/proxy") {
  return getJSON<DashboardModels>("/v1/dashboard/models", apiBase);
}

/** Friendly "x minutes ago" string. */

/** Friendly "x minutes ago" string. */
export function timeAgo(iso: string): string {
  if (!iso) return "";
  const t = new Date(iso).getTime();
  if (isNaN(t)) return "";
  const sec = Math.max(0, (Date.now() - t) / 1000);
  if (sec < 60) return `${Math.round(sec)}s ago`;
  const min = sec / 60;
  if (min < 60) return `${Math.round(min)}m ago`;
  const hr = min / 60;
  if (hr < 24) return `${Math.round(hr)}h ago`;
  const day = hr / 24;
  return `${Math.round(day)}d ago`;
}

/** Shorten model id "openai:gpt-4o-mini" → "gpt-4o-mini". */
export function shortModel(m: string): string {
  if (!m) return "";
  const ix = m.indexOf(":");
  return ix >= 0 ? m.slice(ix + 1) : m;
}

/** Provider prefix from model id. */
export function providerOf(m: string): string {
  if (!m) return "";
  const ix = m.indexOf(":");
  return ix >= 0 ? m.slice(0, ix) : "unknown";
}

/** Color per provider — small static palette so pills stay consistent. */
export function providerColor(p: string): string {
  switch (p) {
    case "openai":
      return "#10a37f";
    case "anthropic":
      return "#cc785c";
    case "mistral":
      return "#ff7000";
    case "groq":
      return "#f55036";
    case "hf":
      return "#ffd21e";
    case "ollama":
      return "#1f1f1f";
    case "mock":
      return "#9ca3af";
    default:
      return "#6b7280";
  }
}

/** Color a faithfulness bar: green for high, yellow mid, red low. */
export function faithfulnessColor(pct: number): string {
  if (pct >= 85) return "#16a34a";
  if (pct >= 60) return "#eab308";
  return "#dc2626";
}
