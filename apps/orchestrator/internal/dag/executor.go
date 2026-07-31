package dag

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cogniflow/orchestrator/internal/budget"
	"github.com/cogniflow/orchestrator/internal/citation"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/eval"
	"github.com/cogniflow/orchestrator/internal/fusion"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/rag"
	"github.com/cogniflow/orchestrator/internal/router"
)

// Sink is the abstraction over the outbound event channel. In production
// it's an SSE-bound writer; in tests it's a recording struct.
type Sink interface {
	// Emit fires an event with a JSON-serializable payload. Errors abort
	// the whole run (the executor returns that error from Run).
	Emit(event string, data any) error
}

// Executor runs a Plan in-process using goroutines + bounded parallelism.
// It honors context cancellation between levels and within levels.
type Executor struct {
	Plan        *decomposer.Plan
	Router      router.Router
	Providers   providers.Registry
	Sink        Sink
	Fuser       fusion.Fuser  // optional; if set, the executor invokes fusion after the synthesizer
	RAG         *rag.Service  // optional; if set, NeedsRAG nodes retrieve + inject docs
	WorkspaceID string        // workspace id used for RAG retrieval
	Estimator   *budget.Estimator  // optional; if set + Budget is non-zero, cascade-downgrades models
	Budget      budget.Budget      // cost + carbon cap for this run
	Judge       *eval.Judge        // optional; if set, runs after the run completes
	Parallelism int                // default 4

	// Populated by Run after a budget cascade.
	overrides   map[string]string // node_id → downgraded model
	overridesMu sync.RWMutex
	// Populated by Run after the cascade completes.
	DowngradeResult *budget.DowngradeResult
}

// New builds an Executor. Parallelism defaults to 4 if <= 0.
func New(p *decomposer.Plan, r router.Router, reg providers.Registry, sink Sink) *Executor {
	return &Executor{Plan: p, Router: r, Providers: reg, Sink: sink, Parallelism: 4, WorkspaceID: "default"}
}

// UpstreamSeparator is the marker we put around upstream node outputs in
// downstream prompts. Phase 5 will replace this with structured spans.
const UpstreamSeparator = "===UPSTREAM==="

// upstreamPrefix returns the "===UPSTREAM: role===" header for a dep id.
func upstreamPrefix(depID string, depRole decomposer.Role) string {
	return fmt.Sprintf("===UPSTREAM: %s (id=%s, role=%s)===", depID, depID, depRole)
}

// nodeOutput is what runNode stores in the shared outs map for downstream
// consumers (buildPrompt + the fusion stage).
type nodeOutput struct {
	Text       string
	Manifest   *citation.Manifest
	Role       decomposer.Role
	Model      string
	DocIDs     []string            // doc ids referenced by this node (Phase 6)
	DocRanges  map[string]DocRange // per-doc char range that was used
	StartedAt  time.Time
	EndedAt    time.Time
	TokensIn   int
	TokensOut  int
}

// DocRange captures the source char range in a single document for one
// retrieval pass. Phase 6 records these so downstream citation spans can
// point back to the exact window.
type DocRange struct {
	CharStart int
	CharEnd   int
}

// runNode executes a single node. It accumulates the streamed text into
// outs under nodeID and emits node_status / chunk events through the Sink.
func (e *Executor) runNode(ctx context.Context, id string, outs *sync.Map) error {
	if e.Plan == nil {
		return fmt.Errorf("dag: nil plan")
	}
	startedAt := time.Now()
	// Find the node.
	var node *decomposer.Node
	for i := range e.Plan.Nodes {
		if e.Plan.Nodes[i].ID == id {
			node = &e.Plan.Nodes[i]
			break
		}
	}
	if node == nil {
		return fmt.Errorf("dag: node %q not in plan", id)
	}

	// 1. Route. The assigned model may have been downgraded by the budget
	// cascade — in that case we honor the override instead of routing again.
	summary := &nodeSummary{node: *node}
	var decision *router.Decision
	if override, ok := e.modelOverride(id); ok {
		decision = &router.Decision{
			Model:  parseModelIDLocal(override),
			Score:  0,
			Reason: "budget override",
		}
	} else if e.Router != nil {
		d, err := e.Router.Route(ctx, summary)
		if err != nil || d == nil {
			// Fall back to the cheap mock if routing fails.
			decision = &router.Decision{
				Model: router.ModelID{Provider: "mock", Model: "echo-v1"},
				Score: 0,
				Reason: fmt.Sprintf("router error: %v", err),
			}
		} else {
			decision = d
		}
	} else {
		decision = &router.Decision{
			Model: router.ModelID{Provider: "mock", Model: "echo-v1"},
			Score: 0,
			Reason: "no router wired",
		}
	}

	// 2. Build the prompt with upstream outputs injected.
	prompt := buildPrompt(*node, outs)

	// 2b. Optional RAG retrieval. If the node is marked NeedsRAG and we have
	// a RAG service wired, retrieve top-k chunks and replace the system
	// message with the injection template. The retrieved sections are also
	// stored so the citation manifest can record DocID/CharStart/CharEnd on
	// every span this node emits.
	var injected []rag.InjectedSection
	systemMsg := e.systemMsgFor(*node)
	if node.NeedsRAG && e.RAG != nil {
		injection, sections, retrievalErr := e.RAG.BuildInjectedSystemPrompt(ctx, e.WorkspaceID, prompt)
		if retrievalErr == nil {
			systemMsg = injection
			injected = sections
		} else if retrievalErr != rag.ErrNoResults {
			// Non-fatal: log and continue without injection.
			_ = e.Sink.Emit("node_status", map[string]any{
				"node_id": id, "status": "running",
				"message": "rag retrieval failed: " + retrievalErr.Error(),
			})
		}
	}

	// 3. Emit "node_status: running".
	_ = e.Sink.Emit("node_status", map[string]any{
		"node_id":    id,
		"status":     "running",
		"model":      decision.Model.String(),
		"score":      decision.Score,
		"breakdown":  decision.Breakdown,
		"reason":     decision.Reason,
		"arm_id":     decision.BanditArmID,
	})

	// 4. Stream.
	streamer, err := e.Providers.Get(decision.Model.String())
	if err != nil {
		_ = e.Sink.Emit("node_status", map[string]any{
			"node_id": id, "status": "error", "message": err.Error(),
		})
		return nil
	}

	accum := &strings.Builder{}
	sink := &sinkAdapter{
		nodeID: id, model: decision.Model.String(),
		sink: e.Sink, accum: accum, finished: false,
		injected: injected,
	}

	req := providers.Request{
		Prompt:   prompt,
		Model:    decision.Model.String(),
		SystemMsg: systemMsg,
		StreamID: id,
		NodeID:   id,
	}

	streamErr := streamer.Stream(ctx, req, sink)
	if streamErr != nil && ctx.Err() == nil {
		_ = e.Sink.Emit("node_status", map[string]any{
			"node_id": id, "status": "error", "message": streamErr.Error(),
		})
		return nil
	}

	// 5. Bundle the accumulated text + a per-node manifest into outs.
	manifest := citation.New()
	// One span per retrieved doc (so the manifest reflects the actual RAG
	// provenance) + a final span for the synthesized answer. If no RAG was
	// injected, the synthesised span still gets the per-node text.
	for _, sec := range injected {
		manifest.Add(citation.Span{
			SubTaskID:  id,
			Model:      decision.Model.String(),
			Text:       sec.Text,
			DocID:      sec.DocID,
			CharStart:  sec.CharStart,
			CharEnd:    sec.CharEnd,
			PromptHash: citation.HashPrompt(prompt),
		})
	}
	manifest.Add(citation.Span{
		SubTaskID:  id,
		Model:      decision.Model.String(),
		Text:       accum.String(),
		PromptHash: citation.HashPrompt(prompt),
	})

	docIDs := make([]string, 0, len(injected))
	docRanges := map[string]DocRange{}
	for _, sec := range injected {
		docIDs = append(docIDs, sec.DocID)
		docRanges[sec.DocID] = DocRange{CharStart: sec.CharStart, CharEnd: sec.CharEnd}
	}
	endedAt := time.Now()
	tokensIn, tokensOut := estimateTokens(prompt, accum.String())
	outs.Store(id, nodeOutput{
		Text:      accum.String(),
		Manifest:  manifest,
		Role:      node.Role,
		Model:     decision.Model.String(),
		DocIDs:    docIDs,
		DocRanges: docRanges,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
	})

	// 6. Emit "node_status: ok".
	_ = e.Sink.Emit("node_status", map[string]any{
		"node_id":     id,
		"status":      "ok",
		"model":       decision.Model.String(),
		"tokens_in":   tokensIn,
		"tokens_out":  tokensOut,
		"duration_ms": int(endedAt.Sub(startedAt) / time.Millisecond),
	})
	return nil
}

// modelOverride returns the budget-cascade override for this node id, if any.
func (e *Executor) modelOverride(id string) (string, bool) {
	e.overridesMu.RLock()
	defer e.overridesMu.RUnlock()
	m, ok := e.overrides[id]
	return m, ok
}

// parseModelIDLocal parses a "provider:model" string into a router.ModelID.
// Kept local so the dag package doesn't depend on the router's unexported
// helper.
func parseModelIDLocal(s string) router.ModelID {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return router.ModelID{Provider: s[:i], Model: s[i+1:]}
		}
	}
	return router.ModelID{Provider: "", Model: s}
}

// estimateTokens is a crude char/4 approximation. The real provider surfaces
// exact counts in its final chunk; Phase 8 reads those.
func estimateTokens(prompt, output string) (int, int) {
	return len(prompt) / 4, len(output) / 4
}

// buildPrompt concatenates upstream outputs in dependency order (sorted by
// id for determinism) and prepends them as ===UPSTREAM=== blocks.
func buildPrompt(n decomposer.Node, outs *sync.Map) string {
	var b strings.Builder
	if len(n.DependsOn) > 0 {
		b.WriteString("# Upstream context\n")
		// Sort deps for deterministic prompt ordering.
		deps := append([]string(nil), n.DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			v, ok := outs.Load(d)
			if !ok {
				continue
			}
			out, _ := v.(nodeOutput)
			fmt.Fprintf(&b, "%s\n%s\n\n", upstreamPrefix(d, out.Role), out.Text)
		}
		b.WriteString("---\n")
	}
	b.WriteString(n.Payload)
	return b.String()
}

// systemMsgFor returns a system message appropriate for the node role.
// Phase 5 will tune these per role.
func (e *Executor) systemMsgFor(n decomposer.Node) string {
	switch n.Role {
	case decomposer.RoleSynthesizer:
		return "You are the synthesizer. Combine the upstream sub-task outputs into a single coherent answer. Cite upstream sources by id when you reference them."
	case decomposer.RoleCoder:
		return "You are an expert software engineer. Produce only the requested code, with brief comments where helpful."
	case decomposer.RoleResearcher:
		return "You are a research analyst. Produce a factual, well-structured report. When uncertain, say so explicitly."
	default:
		return ""
	}
}

// sinkAdapter is a per-node providers.ChunkSink that pushes chunks through
// the executor Sink and accumulates text for downstream consumption.
type sinkAdapter struct {
	nodeID   string
	model    string
	sink     Sink
	accum    *strings.Builder
	finished bool
	injected []rag.InjectedSection // doc provenance for this node (Phase 6)
}

func (s *sinkAdapter) Send(ctx context.Context, c providers.Chunk) error {
	if s.finished {
		return nil
	}
	if c.Text != "" {
		s.accum.WriteString(c.Text)
	}
	if c.Finish {
		s.finished = true
	}
	c.NodeID = s.nodeID
	c.Model = s.model
	if c.V == "" {
		c.V = "chunk.v1"
	}
	if c.StreamID == "" {
		c.StreamID = s.nodeID
	}
	// Attach doc provenance when RAG was injected and the upstream chunk
	// doesn't already carry explicit cites.
	if len(c.Cite) == 0 && len(s.injected) > 0 {
		refs := make([]providers.SpanRef, 0, len(s.injected))
		for _, sec := range s.injected {
			refs = append(refs, providers.SpanRef{
				SubTaskID: s.nodeID,
				Model:     s.model,
				DocID:     sec.DocID,
				CharStart: sec.CharStart,
				CharEnd:   sec.CharEnd,
			})
		}
		c.Cite = refs
	}
	return s.sink.Emit("chunk", c)
}

// Run executes the plan to completion. Returns error only on catastrophic
// failures (e.g. nil plan, cycle, no sink); per-node errors are emitted as
// node_status events.
func (e *Executor) Run(ctx context.Context) error {
	if e.Plan == nil {
		return fmt.Errorf("dag: nil plan")
	}
	if e.Sink == nil {
		return fmt.Errorf("dag: nil sink")
	}
	if len(e.Plan.Nodes) == 0 {
		_ = e.Sink.Emit("done", map[string]any{"ok": true, "total_nodes": 0})
		return nil
	}
	if err := Validate(e.Plan.Nodes, e.Plan.Edges); err != nil {
		return err
	}

	levels, err := TopoSort(e.Plan.Nodes, e.Plan.Edges)
	if err != nil {
		return err
	}

	par := e.Parallelism
	if par <= 0 {
		par = 4
	}
	sem := make(chan struct{}, par)
	outs := &sync.Map{}

	// Phase 7: project the plan's cost + carbon before running; if a budget
	// was supplied, cascade-downgrade models until we fit. The cascade uses
	// the router's first pick per node as the baseline.
	if e.Estimator != nil && (e.Budget.MaxCostUSD > 0 || e.Budget.MaxCarbonG > 0) {
		initial := e.projectInitialModels(ctx)
		if len(initial) > 0 {
			res := e.Estimator.PlanDowngrade(initial, e.Budget, e.Router)
			e.DowngradeResult = &res
			if res.Downgraded > 0 {
				e.overridesMu.Lock()
				e.overrides = res.New
				e.overridesMu.Unlock()
				// Emit a "downgrade" event so the UI can show the cascade.
				_ = e.Sink.Emit("downgrade", map[string]any{
					"original":      res.Original,
					"new":           res.New,
					"saved_usd":     res.SavedUSD,
					"saved_g":       res.SavedG,
					"final_cost_usd": res.FinalCost,
					"final_carbon_g": res.FinalCarbon,
					"downgraded":    res.Downgraded,
					"unachievable":  res.Unachievable,
				})
			}
		}
	}

	// Emit "plan" event with summary.
	_ = e.Sink.Emit("plan", map[string]any{
		"version":     e.Plan.Version,
		"total_nodes": len(e.Plan.Nodes),
		"levels":      len(levels),
	})

	startedAt := time.Now()
	var wg sync.WaitGroup
	for li, level := range levels {
		if ctx.Err() != nil {
			break
		}
		for _, id := range level {
			select {
			case <-ctx.Done():
				_ = e.Sink.Emit("node_status", map[string]any{
					"node_id": id, "status": "error", "message": "context cancelled",
				})
				continue
			case sem <- struct{}{}:
			}
			wg.Add(1)
			go func(nodeID string, levelIdx int) {
				defer wg.Done()
				defer func() { <-sem }()
				_ = e.runNode(ctx, nodeID, outs)
				_ = levelIdx
			}(id, li)
		}
		wg.Wait()
	}

	// Phase 5: invoke fusion after the DAG (if a fuser is wired) to emit
	// the final coherent answer + a CitationManifest as SSE events.
	if e.Fuser != nil && ctx.Err() == nil {
		e.runFusion(ctx, outs)
	}

	// Phase 7: score the run with the eval judge (if wired).
	if e.Judge != nil && ctx.Err() == nil {
		e.runEval(ctx, outs, startedAt)
	}

	_ = e.Sink.Emit("done", map[string]any{
		"ok":          ctx.Err() == nil,
		"total_nodes": len(e.Plan.Nodes),
		"cancelled":   ctx.Err() != nil,
	})
	return nil
}

// projectInitialModels invokes the router for every node to learn the
// baseline model assignment for the budget cascade. Failures fall back to
// the cheap mock.
func (e *Executor) projectInitialModels(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, n := range e.Plan.Nodes {
		summary := &nodeSummary{node: n}
		if e.Router != nil {
			d, err := e.Router.Route(ctx, summary)
			if err == nil && d != nil {
				out[n.ID] = d.Model.String()
				continue
			}
		}
		out[n.ID] = "mock:echo-v1"
	}
	return out
}

// runEval invokes the judge and emits the result as an "eval" SSE event.
func (e *Executor) runEval(ctx context.Context, outs *sync.Map, startedAt time.Time) {
	endedAt := time.Now()
	streams := map[string]string{}
	usage := []eval.UsageEvent{}
	manifest := citation.New()
	for _, n := range e.Plan.Nodes {
		v, ok := outs.Load(n.ID)
		if !ok {
			continue
		}
		out, _ := v.(nodeOutput)
		streams[n.ID] = out.Text
		usage = append(usage, eval.UsageEvent{
			NodeID:     n.ID,
			Model:      out.Model,
			TokensIn:   out.TokensIn,
			TokensOut:  out.TokensOut,
			DurationMS: int(out.EndedAt.Sub(out.StartedAt) / time.Millisecond),
			StartedAt:  out.StartedAt,
			EndedAt:    out.EndedAt,
		})
		for _, s := range out.Manifest.Spans {
			manifest.Add(s)
		}
	}
	dr := &eval.DowngradeRecord{}
	if e.DowngradeResult != nil {
		dr.Original = e.DowngradeResult.Original
		dr.New = e.DowngradeResult.New
		dr.SavedUSD = e.DowngradeResult.SavedUSD
		dr.SavedG = e.DowngradeResult.SavedG
		dr.Unachievable = e.DowngradeResult.Unachievable
	}
	res, err := e.Judge.Score(ctx, eval.RunRecord{
		Plan:      e.Plan,
		Manifest:  manifest,
		Streams:   streams,
		Usage:     usage,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Downgrade: dr,
	})
	if err != nil || res == nil {
		return
	}
	_ = e.Sink.Emit("eval", res)
}

// runFusion packages the per-node outputs into a FusionRequest, invokes the
// fuser, and emits the merged chunks as a "fusion" SSE stream. The final
// CitationManifest is emitted as a "manifest" event.
//
// We re-stream the fusion output (NOT the synthesizer's raw output) so the
// UI gets a coherent final-answer stream that's distinct from the per-node
// streams.
func (e *Executor) runFusion(ctx context.Context, outs *sync.Map) {
	streams := map[string]*fusion.NodeStream{}
	merged := citation.New()
	// Walk plan nodes in deps-first order so the citation order is deterministic.
	for _, n := range e.Plan.Nodes {
		v, ok := outs.Load(n.ID)
		if !ok {
			continue
		}
		out, _ := v.(nodeOutput)
		// Append each node's spans to the merged manifest, assigning new IDs
		// so the merged manifest is independent of the per-node manifests.
		for _, s := range out.Manifest.Spans {
			s.ID = "" // force fresh ID
			merged.Add(s)
		}
		stream := &fusion.NodeStream{
			NodeID:   n.ID,
			Role:     out.Role,
			Text:     out.Text,
			Manifest: out.Manifest,
		}
		streams[n.ID] = stream
	}

	// Find the synthesizer node (the last one — the one with no dependents).
	var synthNode decomposer.Node
	for _, n := range e.Plan.Nodes {
		if len(dependentsOf(e.Plan.Nodes, n.ID)) == 0 {
			synthNode = n
			break
		}
	}
	if synthNode.ID == "" {
		synthNode = e.Plan.Nodes[len(e.Plan.Nodes)-1]
	}

	// Emit "fusion_start" so the UI can switch to the answer view.
	_ = e.Sink.Emit("fusion_start", map[string]any{
		"synth_node": synthNode.ID,
		"streams":    len(streams),
	})

	// Emit chunks from the fuser. The fuser is responsible for sizing its
	// final Finish chunk.
	fusionSink := &fusionSinkAdapter{
		nodeOutput: &nodeOutput{Manifest: merged},
		sink:       e.Sink,
	}
	_ = e.Fuser.Fuse(ctx, fusion.FusionRequest{
		Plan:      e.Plan,
		Streams:   streams,
		SynthNode: synthNode,
	}, fusionSink)

	// Emit the unified manifest.
	_ = e.Sink.Emit("manifest", map[string]any{
		"v":     merged.V,
		"spans": merged.Spans,
	})
}

// dependentsOf returns the ids of nodes that depend on `id`.
func dependentsOf(nodes []decomposer.Node, id string) []string {
	var out []string
	for _, n := range nodes {
		for _, d := range n.DependsOn {
			if d == id {
				out = append(out, n.ID)
			}
		}
	}
	return out
}

// fusionSinkAdapter wraps the executor's Sink to emit each fusion chunk as
// "fusion" SSE events (so the UI can pick them up separately from per-node
// streams).
type fusionSinkAdapter struct {
	nodeOutput *nodeOutput
	sink       Sink
	finished   bool
}

func (f *fusionSinkAdapter) Send(ctx context.Context, c providers.Chunk) error {
	if f.finished {
		return nil
	}
	if c.Finish {
		f.finished = true
	}
	c.NodeID = "" // fusion is its own stream
	if c.StreamID == "" {
		c.StreamID = "fusion"
	}
	// Tag with cited span ids if the synthesizer chunk carried any.
	// (Phase 5 keeps [n] markers in the text; UI parses them.)
	return f.sink.Emit("fusion", c)
}

// nodeSummary wraps a decomposer.Node to satisfy router.NodeSummary.
type nodeSummary struct {
	node decomposer.Node
}

func (n *nodeSummary) TaskClass() decomposer.TaskClass { return n.node.Requires.TaskClass }
func (n *nodeSummary) LatencyBudgetMS() int            { return n.node.Requires.LatencyBudgetMS }
func (n *nodeSummary) MaxCostUSD() float64             { return n.node.Requires.MaxCostUSD }
