// Package eval runs LLM-as-judge faithfulness scoring on final answers.
//
// The Judge type scores a RunRecord (a complete DAG execution) and returns a
// Result containing:
//   - FaithfulnessPct (0..1)
//   - HallucinationFlags (# of unsourced claims)
//   - CostUSD + CarbonG (sum of per-node projections)
//   - LatencyTotalMS / LatencyP95MS
//   - ModelMix (distinct models invoked)
//   - PerNode (per-node model + cost + latency + tokens)
//   - UncitedClaims (text the judge couldn't verify)
//
// The judge is a cheap LLM that sees the final answer + cited spans (with
// doc snippets) and verifies each cited claim against its source. We use
// JSON-constrained decoding via a prompt template. Phase 8 swaps the
// template for an OpenAI tool-call.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cogniflow/orchestrator/internal/citation"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/router"
)

// UsageEvent captures one node's resource usage.
type UsageEvent struct {
	NodeID     string `json:"node_id"`
	Model      string `json:"model"`
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	DurationMS int    `json:"duration_ms"`
	StartedAt  time.Time
	EndedAt    time.Time
}

// DowngradeRecord is a passthrough of budget.DowngradeResult, inlined so the
// eval package doesn't import budget (and the other way around).
type DowngradeRecord struct {
	Original    map[string]string `json:"original"`
	New         map[string]string `json:"new"`
	SavedUSD    float64           `json:"saved_usd"`
	SavedG      float64           `json:"saved_g"`
	Unachievable bool             `json:"unachievable"`
}

// RunRecord is everything the judge needs to score one execution.
type RunRecord struct {
	Plan         *decomposer.Plan
	Manifest     *citation.Manifest
	Streams      map[string]string // node_id → final text
	Usage        []UsageEvent
	StartedAt    time.Time
	EndedAt      time.Time
	Downgrade    *DowngradeRecord
}

// NodeEval is the per-node metrics breakdown.
type NodeEval struct {
	Model         string  `json:"model"`
	CostUSD       float64 `json:"cost_usd"`
	CarbonG       float64 `json:"carbon_g"`
	LatencyMS     int     `json:"latency_ms"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	DowngradedFrom string `json:"downgraded_from,omitempty"`
}

// Result is the per-run evaluation summary.
type Result struct {
	FaithfulnessPct    float64               `json:"faithfulness_pct"`
	HallucinationFlags int                   `json:"hallucination_flags"`
	CostUSD            float64               `json:"cost_usd"`
	CarbonG            float64               `json:"carbon_g"`
	LatencyTotalMS     int                   `json:"latency_total_ms"`
	LatencyP95MS       int                   `json:"latency_p95_ms"`
	ModelMix           []string              `json:"model_mix"`
	PerNode            map[string]NodeEval   `json:"per_node"`
	UncitedClaims      []string              `json:"uncited_claims,omitempty"`
	JudgedBy           string                `json:"judged_by,omitempty"`
}

// Judge scores RunRecords.
type Judge struct {
	Registry providers.Registry
	Model    string            // "openai:gpt-4o-mini" by default
	Costs    *router.CostTable
}

// New builds a Judge. If model is empty, defaults to gpt-4o-mini.
func New(reg providers.Registry, costs *router.CostTable, model string) *Judge {
	if model == "" {
		model = "openai:gpt-4o-mini"
	}
	return &Judge{Registry: reg, Model: model, Costs: costs}
}

// judgeOutput is the JSON the judge LLM is expected to return.
type judgeOutput struct {
	FaithfulnessPct  float64  `json:"faithfulness_pct"`
	UncitedClaims    []string `json:"uncited_claims"`
	Conflicts        []string `json:"conflicts"`
	Reasoning        string   `json:"reasoning"`
}

// Score computes a Result without invoking an LLM (heuristic only). Used by
// tests and as a fallback when the judge model is unavailable.
func (j *Judge) Score(ctx context.Context, rec RunRecord) (*Result, error) {
	if rec.StartedAt.IsZero() || rec.EndedAt.IsZero() {
		rec.EndedAt = time.Now()
	}
	res := &Result{
		PerNode: map[string]NodeEval{},
		ModelMix: []string{},
	}
	// 1. Per-node roll-up.
	seen := map[string]bool{}
	for _, u := range rec.Usage {
		ne := NodeEval{
			Model:     u.Model,
			LatencyMS: u.DurationMS,
			TokensIn:  u.TokensIn,
			TokensOut: u.TokensOut,
		}
		if j.Costs != nil {
			pi := j.Costs.PerMillionInputUSD[u.Model]
			po := j.Costs.PerMillionOutputUSD[u.Model]
			cg := j.Costs.CarbonGPerMTokens[u.Model]
			ne.CostUSD = round4(float64(u.TokensIn)*pi/1e6 + float64(u.TokensOut)*po/1e6)
			ne.CarbonG = round4(float64(u.TokensIn+u.TokensOut) * cg / 1e6)
		}
		if rec.Downgrade != nil {
			if orig, ok := rec.Downgrade.Original[u.NodeID]; ok && orig != u.Model {
				ne.DowngradedFrom = orig
			}
		}
		res.PerNode[u.NodeID] = ne
		res.CostUSD += ne.CostUSD
		res.CarbonG += ne.CarbonG
		if !seen[u.Model] {
			seen[u.Model] = true
			res.ModelMix = append(res.ModelMix, u.Model)
		}
	}
	sort.Strings(res.ModelMix)

	// 2. Latency totals.
	res.LatencyTotalMS = int(rec.EndedAt.Sub(rec.StartedAt) / time.Millisecond)
	if len(rec.Usage) > 0 {
		// p95 across per-node durations.
		ds := make([]int, 0, len(rec.Usage))
		for _, u := range rec.Usage {
			ds = append(ds, u.DurationMS)
		}
		sort.Ints(ds)
		idx := int(float64(len(ds)) * 0.95)
		if idx >= len(ds) {
			idx = len(ds) - 1
		}
		if idx < 0 {
			idx = 0
		}
		res.LatencyP95MS = ds[idx]
	}

	// 3. Faithfulness. Try the judge LLM; fall back to a lexical heuristic.
	finalText := pickSynthText(rec)
	if j.Registry != nil {
		if jo, err := j.invokeJudge(ctx, finalText, rec); err == nil {
			res.FaithfulnessPct = jo.FaithfulnessPct
			res.UncitedClaims = jo.UncitedClaims
			res.HallucinationFlags = len(jo.UncitedClaims)
			res.JudgedBy = j.Model
			return res, nil
		}
	}
	// Fallback heuristic: count "unsourced" claims as sentences that don't
	// appear in any cited span.
	res.FaithfulnessPct = heuristicFaithfulness(finalText, rec.Manifest)
	res.HallucinationFlags = 0
	res.JudgedBy = "heuristic"
	return res, nil
}

func pickSynthText(rec RunRecord) string {
	if len(rec.Streams) == 0 {
		return ""
	}
	// Prefer the node with no dependents (synth node).
	if rec.Plan != nil {
		// collect dependents
		deps := map[string]int{}
		for _, n := range rec.Plan.Nodes {
			if len(n.DependsOn) > 0 {
				deps[n.ID] = len(n.DependsOn)
			}
		}
		// pick one with deps == 0
		for _, n := range rec.Plan.Nodes {
			if deps[n.ID] == 0 {
				if t, ok := rec.Streams[n.ID]; ok {
					return t
				}
			}
		}
	}
	// Fallback: any value.
	for _, v := range rec.Streams {
		return v
	}
	return ""
}

func (j *Judge) invokeJudge(ctx context.Context, finalText string, rec RunRecord) (*judgeOutput, error) {
	if j.Registry == nil {
		return nil, fmt.Errorf("eval: no registry")
	}
	s, err := j.Registry.Get(j.Model)
	if err != nil {
		return nil, err
	}
	prompt := buildJudgePrompt(finalText, rec)
	body, err := invokeAndAccumulate(ctx, s, providers.Request{
		Prompt:    prompt,
		Model:     j.Model,
		StreamID:  "judge",
		NodeID:    "judge",
		SystemMsg: judgeSystemMsg,
	})
	if err != nil {
		return nil, err
	}
	var out judgeOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("eval: judge json: %w", err)
	}
	return &out, nil
}

const judgeSystemMsg = `You are CogniFaith, a strict citation-faithfulness evaluator.
You will be given a final answer and a list of cited spans. For each sentence in the answer, determine whether it is supported by the cited text.
Return STRICT JSON with the schema: {"faithfulness_pct": 0..1, "uncited_claims": [...], "conflicts": [...], "reasoning": "..."}.
Do not wrap the JSON in markdown fences.`

func buildJudgePrompt(finalText string, rec RunRecord) string {
	var b strings.Builder
	b.WriteString("# Final answer\n")
	b.WriteString(finalText)
	b.WriteString("\n\n# Cited spans\n")
	if rec.Manifest != nil {
		for i, s := range rec.Manifest.Spans {
			fmt.Fprintf(&b, "[%d] model=%s doc=%s text=%q\n", i+1, s.Model, s.DocID, truncate(s.Text, 240))
		}
	}
	b.WriteString("\nReturn JSON only.\n")
	return b.String()
}

// heuristicFaithfulness returns a 0..1 score: 1.0 if the answer is empty or
// every sentence is supported by some cited span, otherwise proportional.
//
// A sentence is "supported" if some span text contains the sentence (the
// answer is quoting/paraphrasing the source) OR the sentence contains a
// significant prefix of the span (≥40% of the span's tokens appear in the
// sentence, case-insensitive). The lenient rule is "the sentence is
// substantively in the source"; the strict rule is "the source covers the
// sentence". We combine both: support if either is true.
func heuristicFaithfulness(text string, m *citation.Manifest) float64 {
	if text == "" {
		return 1.0
	}
	if m == nil || len(m.Spans) == 0 {
		return 0.5
	}
	hits, total := 0, 0
	for _, sent := range splitSentences(text) {
		if sent == "" {
			continue
		}
		total++
		if sentenceSupported(sent, m.Spans) {
			hits++
		}
	}
	if total == 0 {
		return 1.0
	}
	return float64(hits) / float64(total)
}

func sentenceSupported(sent string, spans []citation.Span) bool {
	sentTokens := tokenizeWords(sent)
	if len(sentTokens) == 0 {
		return false
	}
	for _, s := range spans {
		// Rule 1: span contains the entire sentence (answer quoting source).
		if containsCI(s.Text, sent) {
			return true
		}
		// Rule 2: ≥40% of the span's words appear in the sentence (answer
		// summarising the source).
		spanTokens := tokenizeWords(s.Text)
		if len(spanTokens) == 0 {
			continue
		}
		overlap := 0
		for _, w := range spanTokens {
			if containsCI(sent, w) {
				overlap++
			}
		}
		if float64(overlap)/float64(len(spanTokens)) >= 0.4 {
			return true
		}
	}
	return false
}

func tokenizeWords(s string) []string {
	var out []string
	cur := make([]byte, 0, 16)
	flush := func() {
		if len(cur) >= 3 { // ignore tiny tokens
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			cur = append(cur, c+32)
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			cur = append(cur, c)
		default:
			flush()
		}
	}
	flush()
	return out
}

func splitSentences(s string) []string {
	out := []string{}
	cur := strings.Builder{}
	for _, r := range s {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func containsCI(haystack, needle string) bool {
	if len(needle) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func round4(v float64) float64 {
	return float64(int64(v*1e4+0.5)) / 1e4
}
