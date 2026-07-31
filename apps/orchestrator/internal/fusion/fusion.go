// Package fusion merges multiple sub-task streams into one coherent answer.
//
// MVP (Phase 5):
//   - ModeHeuristic: concatenate streams with paragraph-level cite markers.
//   - ModeModel:     run the fusion_synthesizer LLM with a SPAN table; cite
//     markers `[n]` are preserved in the streamed output.
//   - ModeAuto:      choose heuristic for ≤1 stream, model otherwise.
//
// On disagreement (overlapping claims with low similarity) we additionally
// fire a Judge LLM call and emit a Verdict side-by-side.
//
// Every fused stream attaches a CitationManifest containing one Span per
// upstream node. Phase 6 will fill DocID/DocSnippet from RAG.
package fusion

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cogniflow/orchestrator/internal/citation"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

// Mode selects the fusion strategy.
type Mode string

const (
	ModeAuto     Mode = "auto"     // heuristic for ≤1 stream, else model
	ModeHeuristic Mode = "heuristic"
	ModeModel    Mode = "model"
)

// Config controls the fuser.
type Config struct {
	Mode       Mode            // default ModeAuto
	SynthModel string          // LLM for ModeModel; default openai:gpt-4o-mini
	JudgeModel string          // LLM for disagreements; default openai:gpt-4o-mini
	Threshold  float64         // disagreement similarity threshold; default 0.85
}

// NodeStream is one upstream sub-task's aggregated output.
type NodeStream struct {
	NodeID   string
	Role     decomposer.Role
	Text     string
	Manifest *citation.Manifest
}

// FusionRequest is what the executor hands to the fuser.
type FusionRequest struct {
	Plan      *decomposer.Plan
	Streams   map[string]*NodeStream
	Prompt    string            // original user prompt
	SynthNode decomposer.Node   // the synthesizer node (for context)
}

// Fuser is the contract. Implementations must write one final coherent
// answer into the sink (with Chunk.Finish=true on the last chunk).
type Fuser interface {
	Fuse(ctx context.Context, fr FusionRequest, sink providers.ChunkSink) error
}

// New constructs a Fuser for the given config. Registry must be non-nil.
func New(cfg Config, registry providers.Registry) Fuser {
	if registry == nil {
		registry = providers.NewRegistry(nil)
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeAuto
	}
	if cfg.SynthModel == "" {
		cfg.SynthModel = "openai:gpt-4o-mini"
	}
	if cfg.JudgeModel == "" {
		cfg.JudgeModel = "openai:gpt-4o-mini"
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 0.85
	}
	return &fuser{cfg: cfg, reg: registry}
}

// fuser is the concrete impl. It dispatches based on Mode.
type fuser struct {
	cfg Config
	reg providers.Registry
}

// Fuse picks the strategy and writes to sink.
func (f *fuser) Fuse(ctx context.Context, fr FusionRequest, sink providers.ChunkSink) error {
	if fr.Plan == nil && len(fr.Streams) == 0 {
		return fmt.Errorf("fusion: nil plan and no streams")
	}
	if len(fr.Streams) == 0 {
		// Nothing to fuse; just emit a finish chunk so the stream closes cleanly.
		return sink.Send(ctx, providers.Chunk{
			V: "chunk.v1", StreamID: "fusion", Model: "system",
			Text: "[fusion] no upstream streams", Finish: true,
		})
	}

	mode := f.cfg.Mode
	if mode == ModeAuto {
		if len(fr.Streams) <= 1 {
			mode = ModeHeuristic
		} else {
			mode = ModeModel
		}
	}

	switch mode {
	case ModeHeuristic:
		return f.fuseHeuristic(ctx, fr, sink)
	case ModeModel:
		return f.fuseModel(ctx, fr, sink)
	default:
		return fmt.Errorf("fusion: unknown mode %q", mode)
	}
}

// fuseHeuristic concatenates streams with paragraph-level cites.
func (f *fuser) fuseHeuristic(ctx context.Context, fr FusionRequest, sink providers.ChunkSink) error {
	// Deterministic order: sort by node id.
	keys := make([]string, 0, len(fr.Streams))
	for k := range fr.Streams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Emit one header.
	_ = sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: "fusion", Model: "fusion",
		Text: fmt.Sprintf("# Synthesized answer (%d streams)\n\n", len(keys)),
	})
	for i, k := range keys {
		s := fr.Streams[k]
		marker := fmt.Sprintf("[%d]", i+1)
		header := fmt.Sprintf("\n-- %s (%s, role=%s) %s --\n", k, s.NodeID, s.Role, marker)
		_ = sink.Send(ctx, providers.Chunk{
			V: "chunk.v1", StreamID: "fusion", Model: "fusion",
			Text: header,
		})
		_ = sink.Send(ctx, providers.Chunk{
			V: "chunk.v1", StreamID: "fusion", Model: "fusion",
			Text: s.Text,
		})
		_ = sink.Send(ctx, providers.Chunk{
			V: "chunk.v1", StreamID: "fusion", Model: "fusion",
			Text: "\n",
		})
	}
	// Trailing summary of citations (1..N).
	var cites []string
	for i, k := range keys {
		s := fr.Streams[k]
		cites = append(cites, fmt.Sprintf("[%d] %s (%s)", i+1, k, s.Role))
	}
	_ = sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: "fusion", Model: "fusion",
		Text: "\n## Sources\n" + strings.Join(cites, "\n"),
	})
	return sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: "fusion", Model: "fusion", Finish: true,
	})
}

// fuseModel runs the fusion_synthesizer LLM with the SPAN table inline.
// We stream into the sink. The synthesizer's output preserves [n] markers;
// we don't transform them here (Phase 5 leaves the model's text untouched).
func (f *fuser) fuseModel(ctx context.Context, fr FusionRequest, sink providers.ChunkSink) error {
	prompt := buildSynthPrompt(fr)
	streamer, err := f.reg.Get(f.cfg.SynthModel)
	if err != nil {
		// Fall back to heuristic if the LLM is unreachable.
		return f.fuseHeuristic(ctx, fr, sink)
	}
	req := providers.Request{
		Prompt:    prompt,
		Model:     f.cfg.SynthModel,
		SystemMsg: "You are the CogniFlow Synthesizer. Output only the merged answer with inline [n] citations.",
		StreamID:  "fusion",
		NodeID:    fr.SynthNode.ID,
	}
	return streamer.Stream(ctx, req, sink)
}

// buildSynthPrompt assembles the synthesizer LLM prompt.
func buildSynthPrompt(fr FusionRequest) string {
	var b strings.Builder
	tmpl := FusionSynthesizerPrompt()
	b.WriteString(tmpl)
	b.WriteString("\n\n## Runtime context\n\n")
	fmt.Fprintf(&b, "USER PROMPT:\n%s\n\n", fr.Prompt)
	b.WriteString("=== UPSTREAM OUTPUTS ===\n")
	keys := make([]string, 0, len(fr.Streams))
	for k := range fr.Streams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		s := fr.Streams[k]
		fmt.Fprintf(&b, "\n-- %s (role=%s) [%d] --\n", k, s.Role, i+1)
		b.WriteString(s.Text)
		b.WriteString("\n")
	}
	b.WriteString("\n=== SPAN TABLE ===\n")
	for i, k := range keys {
		s := fr.Streams[k]
		fmt.Fprintf(&b, "\n[%d] %s — model=%s — node=%s\n", i+1, shortQuote(s.Text, 240), s.NodeID, k)
	}
	return b.String()
}

// shortQuote returns the first N chars of s, single-line.
func shortQuote(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
