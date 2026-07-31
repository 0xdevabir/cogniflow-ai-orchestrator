package decomposer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// Deps is the dependency bundle for a Decomposer.
type Deps struct {
	// Registry is the Streamer registry. We pull a "decomposer model" out
	// of it — defaulting to "openai:gpt-4o" if available, else mock.
	Registry providers.Registry
	// Model is the fully-qualified model id used for decomposition.
	// If empty, we use a sensible default per provider.
	Model string
	// Timeout per LLM call. Default: 30s.
	Timeout time.Duration
	// Retries on invalid JSON. Default: 3.
	Retries int
	// MaxTokens per LLM call. Default: 4096.
	MaxTokens int
}

// Decomposer turns a prompt into a Plan.
type Decomposer struct {
	deps Deps
}

// New builds a Decomposer. Panics on a nil Registry because that would
// always lead to a passthrough plan, which is a code smell.
func New(d Deps) *Decomposer {
	if d.Registry == nil {
		panic("decomposer: nil registry")
	}
	if d.Timeout <= 0 {
		d.Timeout = 30 * time.Second
	}
	if d.Retries <= 0 {
		d.Retries = 3
	}
	if d.MaxTokens <= 0 {
		d.MaxTokens = 4096
	}
	return &Decomposer{deps: d}
}

// Deps returns the current dependency bundle. Useful for cloning a
// Decomposer with a different model (see api/plan.go).
func (d *Decomposer) Deps() Deps { return d.deps }

// Model returns the configured model id (may be empty).
func (d *Decomposer) Model() string { return d.deps.Model }

// Decompose calls the LLM, validates the JSON, and either returns a Plan
// or the passthrough fallback. The returned error is non-nil ONLY for
// catastrophic failures (nil pointer, ctx cancelled, etc.) — when we
// fall back to passthrough we return (passthrough, nil).
func (d *Decomposer) Decompose(ctx context.Context, prompt string) (*Plan, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("decomposer: empty prompt")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	model := d.deps.Model
	if model == "" {
		model = "openai:gpt-4o"
	}

	// Build the system prompt with schema hint appended so providers that
	// don't support tool-use still get the format.
	sys := CurrentPrompt()
	if !strings.Contains(sys, "$schema") {
		sys = sys + "\n\n## Schema (return this exact shape)\n```json\n" + string(PlanSchemaJSON) + "\n```"
	}

	var lastErr error
	for attempt := 0; attempt < d.deps.Retries; attempt++ {
		if ctx.Err() != nil {
			return PassthroughPlan(prompt), nil
		}

		// On retries, append the previous error to nudge the LLM.
		attemptSys := sys
		if lastErr != nil {
			attemptSys = sys + "\n\nYour previous output was invalid: " +
				lastErr.Error() + "\nFix the issue and re-emit only valid JSON."
		}

		raw, err := d.callLLM(ctx, model, attemptSys, prompt)
		if err != nil {
			// Network / context errors: pass through, don't retry.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return PassthroughPlan(prompt), nil
			}
			lastErr = err
			continue
		}

		plan, err := d.parseAndValidate(raw)
		if err != nil {
			lastErr = err
			continue
		}

		// Semantic checks that JSON Schema can't express easily.
		if err := semanticCheck(plan); err != nil {
			lastErr = err
			continue
		}

		return plan, nil
	}

	// Exhausted retries — passthrough.
	return PassthroughPlan(prompt), nil
}

// callLLM invokes the configured Streamer and accumulates the full response.
// Uses the existing providers.Streamer contract from Phase 1.
func (d *Decomposer) callLLM(ctx context.Context, model, sys, prompt string) (string, error) {
	streamer, _ := d.deps.Registry.Get(model)
	if streamer == nil {
		return "", fmt.Errorf("no streamer for model %q", model)
	}

	cctx, cancel := context.WithTimeout(ctx, d.deps.Timeout)
	defer cancel()

	var sb strings.Builder
	sink := &collectSink{buf: &sb, finished: false}

	req := providers.Request{
		Prompt:      prompt,
		Model:       model,
		SystemMsg:   sys,
		MaxTokens:   d.deps.MaxTokens,
		Temperature: 0,
		StreamID:    "decomposer",
		NodeID:      "decomposer",
	}

	// Best-effort: many providers reject high max_tokens for mini models,
	// so we cap the request.
	if req.MaxTokens > 8192 {
		req.MaxTokens = 8192
	}

	if err := streamer.Stream(cctx, req, sink); err != nil {
		return "", fmt.Errorf("stream: %w", err)
	}
	if !sink.finished {
		return "", fmt.Errorf("stream ended without Finish chunk")
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("stream produced no text")
	}
	return out, nil
}

// parseAndValidate strips any markdown fences and validates the JSON.
func (d *Decomposer) parseAndValidate(raw string) (*Plan, error) {
	cleaned := stripFences(raw)
	cleaned = extractJSON(cleaned)
	if cleaned == "" {
		return nil, fmt.Errorf("no JSON object found in LLM output")
	}

	// Validate the raw JSON against the schema FIRST so we get a clear
	// error message; if it passes, unmarshal into the typed Plan.
	if err := ValidateJSON([]byte(cleaned)); err != nil {
		return nil, fmt.Errorf("schema validation: %w", err)
	}

	var p Plan
	if err := jsonUnmarshal([]byte(cleaned), &p); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &p, nil
}

// stripFences removes leading/trailing ```json ... ``` fences.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Drop the opening fence line.
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		// Drop the closing fence.
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}

// extractJSON finds the first '{' and the last balanced '}' and returns
// the substring. Helps when the LLM emits "Here you go: {...}".
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	// Walk forward, tracking nesting + string quoting. Whitespace inside
	// braces is allowed (LLMs often emit "{ }" for empty objects).
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				// Trim trailing whitespace before the closing brace so
				// "{ }" becomes "{}".
				out := s[start : i+1]
				out = strings.ReplaceAll(out, "{ }", "{}")
				out = strings.ReplaceAll(out, "{  }", "{}")
				return out
			}
		}
	}
	return ""
}

// semanticCheck applies a few rules JSON Schema can't easily express:
//   - at least one node
//   - all node ids unique
//   - edges reference known nodes
//   - depends_on entries reference known nodes
//   - exactly one synthesizer node with no dependents (or no edges out)
func semanticCheck(p *Plan) error {
	if len(p.Nodes) == 0 {
		return fmt.Errorf("plan must have at least 1 node")
	}
	if len(p.Nodes) > 6 {
		return fmt.Errorf("plan has %d nodes; max 6", len(p.Nodes))
	}
	ids := map[string]bool{}
	for i, n := range p.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node[%d] has empty id", i)
		}
		if ids[n.ID] {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
		if n.Role == "" {
			return fmt.Errorf("node %q has empty role", n.ID)
		}
		if n.Payload == "" {
			return fmt.Errorf("node %q has empty payload", n.ID)
		}
		for _, dep := range n.DependsOn {
			if !ids[dep] && dep != n.ID {
				// dep may refer to a node defined later in the array;
				// we accept forward refs and verify after the loop.
				continue
			}
		}
	}
	// Now all ids are known; validate depends_on + edges.
	for _, n := range p.Nodes {
		for _, dep := range n.DependsOn {
			if dep == n.ID {
				return fmt.Errorf("node %q depends on itself", n.ID)
			}
			if !ids[dep] {
				return fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
		}
	}
	edgeSeen := map[string]bool{}
	for _, e := range p.Edges {
		if !ids[e.From] {
			return fmt.Errorf("edge from unknown node %q", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("edge to unknown node %q", e.To)
		}
		k := e.From + "->" + e.To
		if edgeSeen[k] {
			return fmt.Errorf("duplicate edge %s", k)
		}
		edgeSeen[k] = true
	}
	// Find the synthesizer (or last node with no outgoing edge) and
	// ensure there is exactly one.
	synthCount := 0
	for _, n := range p.Nodes {
		if n.Role == RoleSynthesizer {
			synthCount++
		}
	}
	if synthCount > 1 {
		return fmt.Errorf("plan has %d synthesizer nodes; expected ≤1", synthCount)
	}
	// Note: we don't REQUIRE a synthesizer — the planner is free to omit
	// one. If absent, the final node (topologically) is treated as the sink.
	return nil
}

// collectSink accumulates Chunks into a string builder. Mirrors the Phase 1
// sseSink pattern but writes to memory instead of an http.ResponseWriter.
type collectSink struct {
	buf      *strings.Builder
	finished bool
}

func (c *collectSink) Send(_ context.Context, ch providers.Chunk) error {
	if c.finished {
		return nil
	}
	if ch.Finish {
		c.finished = true
		// We still record text if any (none expected on a finish chunk).
		return nil
	}
	c.buf.WriteString(ch.Text)
	return nil
}
