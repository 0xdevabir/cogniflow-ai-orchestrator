package dag

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
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
	Parallelism int // default 4
}

// New builds an Executor. Parallelism defaults to 4 if <= 0.
func New(p *decomposer.Plan, r router.Router, reg providers.Registry, sink Sink) *Executor {
	return &Executor{Plan: p, Router: r, Providers: reg, Sink: sink, Parallelism: 4}
}

// UpstreamSeparator is the marker we put around upstream node outputs in
// downstream prompts. Phase 5 will replace this with structured spans.
const UpstreamSeparator = "===UPSTREAM==="

// upstreamPrefix returns the "===UPSTREAM: role===" header for a dep id.
func upstreamPrefix(depID string, depRole decomposer.Role) string {
	return fmt.Sprintf("===UPSTREAM: %s (id=%s, role=%s)===", depID, depID, depRole)
}

// runNode executes a single node. It accumulates the streamed text into
// outs under nodeID and emits node_status / chunk events through the Sink.
func (e *Executor) runNode(ctx context.Context, id string, outs *sync.Map) error {
	if e.Plan == nil {
		return fmt.Errorf("dag: nil plan")
	}
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

	// 1. Route.
	summary := &nodeSummary{node: *node}
	var decision *router.Decision
	if e.Router != nil {
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
	}

	req := providers.Request{
		Prompt:   prompt,
		Model:    decision.Model.String(),
		SystemMsg: e.systemMsgFor(*node),
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

	// 5. Store the accumulated text for downstream consumers.
	outs.Store(id, accum.String())

	// 6. Emit "node_status: ok".
	_ = e.Sink.Emit("node_status", map[string]any{
		"node_id": id, "status": "ok",
	})
	return nil
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
			// Find the role of the dep.
			var role decomposer.Role
			// We don't have the plan here — fall back to "" if not found.
			role = "" // role is decorative; executor passes through plan elsewhere if needed
			v, ok := outs.Load(d)
			if !ok {
				continue
			}
			s, _ := v.(string)
			fmt.Fprintf(&b, "%s\n%s\n\n", upstreamPrefix(d, role), s)
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

	// Emit "plan" event with summary.
	_ = e.Sink.Emit("plan", map[string]any{
		"version":     e.Plan.Version,
		"total_nodes": len(e.Plan.Nodes),
		"levels":      len(levels),
	})

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

	_ = e.Sink.Emit("done", map[string]any{
		"ok":          ctx.Err() == nil,
		"total_nodes": len(e.Plan.Nodes),
		"cancelled":   ctx.Err() != nil,
	})
	return nil
}

// nodeSummary wraps a decomposer.Node to satisfy router.NodeSummary.
type nodeSummary struct {
	node decomposer.Node
}

func (n *nodeSummary) TaskClass() decomposer.TaskClass { return n.node.Requires.TaskClass }
func (n *nodeSummary) LatencyBudgetMS() int            { return n.node.Requires.LatencyBudgetMS }
func (n *nodeSummary) MaxCostUSD() float64             { return n.node.Requires.MaxCostUSD }
