package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cogniflow/orchestrator/internal/banditlearn"
	"github.com/cogniflow/orchestrator/internal/eval"
	"github.com/cogniflow/orchestrator/internal/meter"
	"github.com/cogniflow/orchestrator/internal/router"
)

// defaultLimit caps how many runs the dashboard returns per page.
const dashboardDefaultLimit = 20
const dashboardMaxLimit = 100

// ---------------------------------------------------------------------------
// GET /v1/dashboard/runs?limit=20
// ---------------------------------------------------------------------------

// HandleDashboardRuns returns recent runs aggregated from eval.jsonl +
// meter.jsonl. It does NOT hit any database; this is the cheap read path
// for the dashboard UI.
func (s *Server) HandleDashboardRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := dashboardDefaultLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
			if limit > dashboardMaxLimit {
				limit = dashboardMaxLimit
			}
		}
	}

	evalPath := envOr("EVAL_LOG", "./data/eval.jsonl")
	evals, err := readEvalLog(evalPath)
	if err != nil {
		writeDashboardJSON(w, map[string]any{"error": err.Error()})
		return
	}

	meterPath := envOr("METER_LOG", "./data/meter.jsonl")
	meterEvents, _ := readMeterLog(meterPath)

	// Aggregate meter events per run for tokens + per-node detail.
	perRunUsage := map[string][]meter.Event{}
	for _, ev := range meterEvents {
		perRunUsage[ev.RunID] = append(perRunUsage[ev.RunID], ev)
	}

	// Newest first.
	sort.Slice(evals, func(i, j int) bool {
		return evals[i].StartedAt.After(evals[j].StartedAt)
	})

	runs := make([]map[string]any, 0, min(len(evals), limit))
	var (
		totalCost   float64
		totalCarbon float64
		totalLat    int
		faithSum    float64
		faithCount  int
	)
	for i, e := range evals {
		if i >= limit {
			break
		}
		usage := perRunUsage[e.RunID]

		// Per-run token totals.
		var tokensIn, tokensOut int
		for _, u := range usage {
			tokensIn += u.TokensIn
			tokensOut += u.TokensOut
		}

		downgraded := 0
		for _, n := range e.PerNode {
			if n.DowngradedFrom != "" {
				downgraded++
			}
		}

		runs = append(runs, map[string]any{
			"run_id":              e.RunID,
			"prompt":              truncateStr(e.Prompt, 240),
			"workspace":           e.Workspace,
			"started_at":          e.StartedAt,
			"finished_at":         e.FinishedAt,
			"cost_usd":            e.CostUSD,
			"carbon_g":            e.CarbonG,
			"latency_total_ms":    e.LatencyTotalMS,
			"latency_p95_ms":      e.LatencyP95MS,
			"faithfulness_pct":    e.FaithfulnessPct,
			"hallucination_flags": e.HallucinationFlags,
			"model_mix":           e.ModelMix,
			"tokens_in":           tokensIn,
			"tokens_out":          tokensOut,
			"judged_by":           e.JudgedBy,
			"downgraded_nodes":    downgraded,
		})

		totalCost += e.CostUSD
		totalCarbon += e.CarbonG
		totalLat += e.LatencyTotalMS
		if e.FaithfulnessPct > 0 {
			faithSum += e.FaithfulnessPct
			faithCount++
		}
	}

	var avgLat, avgFaith float64
	if len(evals) > 0 {
		avgLat = float64(totalLat) / float64(len(evals))
	}
	if faithCount > 0 {
		avgFaith = faithSum / float64(faithCount)
	}

	// Per-model aggregates from meter.jsonl + eval.jsonl so the dashboard
	// can show per-model cost / tokens / latency / faithfulness columns.
	modelAgg := aggregatePerModel(evals, meterEvents)

	writeDashboardJSON(w, map[string]any{
		"runs":          runs,
		"model_agg":     modelAgg,
		"totals": map[string]any{
			"runs":                 len(evals),
			"cost_usd":             round4(totalCost),
			"carbon_g":             round4(totalCarbon),
			"avg_latency_ms":       int(avgLat),
			"avg_faithfulness_pct": round4(avgFaith),
		},
	})
}

// ---------------------------------------------------------------------------
// GET /v1/dashboard/bandit
// ---------------------------------------------------------------------------

// HandleDashboardBandit re-runs the bandit learner over BANDIT_LOG and
// returns per-(task_class, model) stats.
func (s *Server) HandleDashboardBandit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := os.Getenv("BANDIT_LOG")
	if path == "" {
		path = "./data/bandit.jsonl"
	}
	rec, err := banditlearn.LearnFile(path, 1)
	if err != nil {
		// File may not exist yet — return empty rather than 500.
		writeDashboardJSON(w, map[string]any{
			"classes":      []any{},
			"total_events": 0,
			"path":         path,
		})
		return
	}

	type banditModelJSON struct {
		Model    string  `json:"model"`
		Count    int     `json:"count"`
		MeanSat  float64 `json:"mean_satisfaction"`
		MeanLat  int     `json:"mean_latency_ms"`
		MeanCost float64 `json:"mean_cost_usd"`
		Failures int     `json:"failures"`
	}
	type classJSON struct {
		TaskClass             string            `json:"task_class"`
		Winner                string            `json:"winner"`
		RecommendedBenchBoost float64           `json:"recommended_bench_boost"`
		Models                []banditModelJSON `json:"models"`
	}

	out := make([]classJSON, 0, len(rec.Classes))
	for _, c := range rec.Classes {
		cj := classJSON{
			TaskClass:             c.TaskClass,
			Winner:                c.Winner,
			RecommendedBenchBoost: round4(c.RecommendedBenchBoost),
			Models:                make([]banditModelJSON, 0, len(c.Models)),
		}
		for _, m := range c.Models {
			cj.Models = append(cj.Models, banditModelJSON{
				Model:    m.Model,
				Count:    m.Count,
				MeanSat:  round4(m.SatSum / float64(m.Count)),
				MeanLat:  m.LatSumMS / m.Count,
				MeanCost: round4(m.CostSumUSD / float64(m.Count)),
				Failures: m.FailureCount,
			})
		}
		out = append(out, cj)
	}

	writeDashboardJSON(w, map[string]any{
		"classes":         out,
		"total_events":    rec.TotalEvents,
		"generated_at":    rec.GeneratedAt,
		"path":            path,
	})
}

// ---------------------------------------------------------------------------
// GET /v1/dashboard/models
// ---------------------------------------------------------------------------

// HandleDashboardModels returns the merged static reference data: cost
// table + benchmark scores, joined by model id.
func (s *Server) HandleDashboardModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	costs, _ := router.LoadCostTable()
	bench, _ := router.LoadBenchmarks()

	// Collect the union of all known model ids.
	seen := map[string]bool{}
	all := []string{}
	if costs != nil {
		for m := range costs.PerMillionInputUSD {
			if !seen[m] {
				seen[m] = true
				all = append(all, m)
			}
		}
	}
	if bench != nil {
		for _, byClass := range bench.Scores {
			for m := range byClass {
				if !seen[m] {
					seen[m] = true
					all = append(all, m)
				}
			}
		}
	}
	sort.Strings(all)

	rows := make([]map[string]any, 0, len(all))
	for _, m := range all {
		row := map[string]any{"model": m}
		if costs != nil {
			row["cost_in_per_m_usd"] = round4(costs.PerMillionInputUSD[m])
			row["cost_out_per_m_usd"] = round4(costs.PerMillionOutputUSD[m])
			row["latency_p95_ms"] = costs.LatencyP95MS[m]
			row["carbon_g_per_m"] = round4(costs.CarbonGPerMTokens[m])
		}
		if bench != nil {
			scores := map[string]float64{}
			for tc, byClass := range bench.Scores {
				if v, ok := byClass[m]; ok {
					scores[tc] = round4(v)
				}
			}
			row["bench_scores"] = scores
		}
		rows = append(rows, row)
	}

	writeDashboardJSON(w, map[string]any{
		"models": rows,
		"count":  len(rows),
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readEvalLog(path string) ([]eval.LoggedResult, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty log is fine
		}
		return nil, err
	}
	defer f.Close()
	var out []eval.LoggedResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r eval.LoggedResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // skip bad lines
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func readMeterLog(path string) ([]meter.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []meter.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e meter.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func writeDashboardJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	body, _ := json.Marshal(v)
	_, _ = w.Write(body)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func round4(v float64) float64 {
	if v == 0 {
		return 0
	}
	return float64(int64(v*1e4+0.5)) / 1e4
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// aggregatePerModel joins meter.jsonl (per-node usage events) with eval.jsonl
// (per-run faithfulness + model_mix) to produce per-model rows. The eval
// score is bucketed per-model by walking the run's model_mix — it's an
// approximation in the absence of per-node faithfulness, but it's accurate
// enough for a dashboard.
func aggregatePerModel(evals []eval.LoggedResult, meterEvents []meter.Event) []map[string]any {
	type acc struct {
		runs        map[string]struct{}
		tokensIn    int
		tokensOut   int
		cost        float64
		latSumMS    int
		latN        int
		faithSum    float64
		faithN      int
	}
	by := map[string]*acc{}

	get := func(m string) *acc {
		if v, ok := by[m]; ok {
			return v
		}
		by[m] = &acc{runs: map[string]struct{}{}}
		return by[m]
	}

	for _, ev := range meterEvents {
		if ev.Model == "" {
			continue
		}
		a := get(ev.Model)
		a.tokensIn += ev.TokensIn
		a.tokensOut += ev.TokensOut
		a.cost += ev.CostUSD
		a.latSumMS += ev.LatencyMS
		a.latN++
		if ev.RunID != "" {
			a.runs[ev.RunID] = struct{}{}
		}
	}

	for _, e := range evals {
		if e.FaithfulnessPct <= 0 {
			continue
		}
		seen := map[string]struct{}{}
		for _, m := range e.ModelMix {
			if m == "" {
				continue
			}
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			a := get(m)
			a.faithSum += e.FaithfulnessPct
			a.faithN++
			if e.RunID != "" {
				a.runs[e.RunID] = struct{}{}
			}
		}
	}

	out := make([]map[string]any, 0, len(by))
	for m, a := range by {
		var meanLat, meanFaith float64
		if a.latN > 0 {
			meanLat = float64(a.latSumMS) / float64(a.latN)
		}
		if a.faithN > 0 {
			meanFaith = a.faithSum / float64(a.faithN)
		}
		out = append(out, map[string]any{
			"model":                  m,
			"runs":                   len(a.runs),
			"tokens_in":              a.tokensIn,
			"tokens_out":             a.tokensOut,
			"cost_usd":               round4(a.cost),
			"mean_latency_ms":        int(meanLat),
			"mean_faithfulness_pct":  round4(meanFaith),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["model"].(string) < out[j]["model"].(string)
	})
	return out
}

// Ensure time package is referenced even when not used directly above.
var _ = time.Time{}