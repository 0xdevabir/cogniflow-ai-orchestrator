package fusion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// Claim is one verifiable assertion attributed to a particular node/model.
type Claim struct {
	Text    string
	SpanID  string
	NodeID  string
	Model   string
}

// Verdict is the judge's pick.
type Verdict struct {
	Pick       string   `json:"verdict"`
	Confidence float64  `json:"confidence"`
	Reasoning  string   `json:"reasoning"`
	Winners    []string `json:"winners"`
}

// Judge calls the LLM to compare two claims.
type Judge struct {
	Registry providers.Registry
	Model    string // default openai:gpt-4o-mini
}

// Compare asks the judge LLM to pick between two claims. Returns a verdict
// or an error if the LLM response could not be parsed.
func (j *Judge) Compare(ctx context.Context, a, b Claim) (*Verdict, error) {
	if j.Registry == nil {
		return nil, fmt.Errorf("judge: nil registry")
	}
	if j.Model == "" {
		j.Model = "openai:gpt-4o-mini"
	}
	streamer, err := j.Registry.Get(j.Model)
	if err != nil {
		return nil, err
	}

	prompt := buildJudgePrompt(a, b)
	req := providers.Request{
		Prompt:    prompt,
		Model:     j.Model,
		SystemMsg: "You are the CogniFlow Judge. Return only the JSON object specified in the prompt.",
		StreamID:  "judge",
		NodeID:    "judge",
	}

	// Collect the streamed response.
	var buf strings.Builder
	sink := &collectSink{buf: &buf}
	if err := streamer.Stream(ctx, req, sink); err != nil {
		return nil, err
	}
	return parseVerdict(buf.String())
}

// buildJudgePrompt fills the judge template with claim A and B context.
func buildJudgePrompt(a, b Claim) string {
	tmpl := JudgePrompt()
	var sb strings.Builder
	sb.WriteString(tmpl)
	sb.WriteString("\n\n## Runtime context\n\n")
	fmt.Fprintf(&sb, "### Claim A (node=%s, model=%s, span=%s)\n%s\n\n",
		a.NodeID, a.Model, a.SpanID, a.Text)
	fmt.Fprintf(&sb, "### Claim B (node=%s, model=%s, span=%s)\n%s\n\n",
		b.NodeID, b.Model, b.SpanID, b.Text)
	sb.WriteString("Return only the JSON object. No prose.\n")
	return sb.String()
}

func parseVerdict(raw string) (*Verdict, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown code fences if present.
	raw = stripFences(raw)
	// Find the first {...} block.
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var v Verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("judge: malformed verdict: %w (raw=%q)", err, truncate(raw, 200))
	}
	switch v.Pick {
	case "A", "B", "tie":
	default:
		return nil, fmt.Errorf("judge: invalid pick %q", v.Pick)
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return nil, fmt.Errorf("judge: confidence out of range: %v", v.Confidence)
	}
	return &v, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Drop first line (```` or ```json) and trailing fence.
		if nl := strings.Index(s, "\n"); nl >= 0 {
			s = s[nl+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// collectSink accumulates all chunks into a single string for parsing.
type collectSink struct {
	buf *strings.Builder
}

func (c *collectSink) Send(ctx context.Context, ch providers.Chunk) error {
	if ch.Text != "" {
		c.buf.WriteString(ch.Text)
	}
	return nil
}

// DisagreementThreshold is the Jaccard similarity above which two claims
// are considered "overlapping" and routed to the judge.
const DisagreementThreshold = 0.55

// Jaccard returns the Jaccard similarity between two strings after a
// simple lowercased word-token set. Empty strings return 0.
func Jaccard(a, b string) float64 {
	at := tokenize(a)
	bt := tokenize(b)
	if len(at) == 0 && len(bt) == 0 {
		return 0
	}
	setA := map[string]bool{}
	for _, t := range at {
		setA[t] = true
	}
	setB := map[string]bool{}
	for _, t := range bt {
		setB[t] = true
	}
	inter := 0
	for k := range setA {
		if setB[k] {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	// Strip basic punctuation.
	repl := strings.NewReplacer(".", " ", ",", " ", ";", " ", ":", " ", "!", " ", "?", " ", "(", " ", ")", " ")
	s = repl.Replace(s)
	fields := strings.Fields(s)
	// Drop very short tokens.
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) >= 3 {
			out = append(out, f)
		}
	}
	return out
}
