package dag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/fusion"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/rag"
	"github.com/cogniflow/orchestrator/internal/router"
)

// --- topology tests ---

func TestTopoSort_LinearPlan(t *testing.T) {
	nodes := []decomposer.Node{
		{ID: "A", Payload: "x"},
		{ID: "B", DependsOn: []string{"A"}, Payload: "y"},
		{ID: "C", DependsOn: []string{"B"}, Payload: "z"},
	}
	levels, err := TopoSort(nodes, nil)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("len(levels) = %d, want 3; got %v", len(levels), levels)
	}
	if got := levels[0]; len(got) != 1 || got[0] != "A" {
		t.Errorf("levels[0] = %v, want [A]", got)
	}
	if got := levels[1]; len(got) != 1 || got[0] != "B" {
		t.Errorf("levels[1] = %v, want [B]", got)
	}
	if got := levels[2]; len(got) != 1 || got[0] != "C" {
		t.Errorf("levels[2] = %v, want [C]", got)
	}
}

func TestTopoSort_DiamondPlan(t *testing.T) {
	nodes := []decomposer.Node{
		{ID: "A", Payload: "a"},
		{ID: "B", DependsOn: []string{"A"}, Payload: "b"},
		{ID: "C", DependsOn: []string{"A"}, Payload: "c"},
		{ID: "D", DependsOn: []string{"B", "C"}, Payload: "d"},
	}
	levels, err := TopoSort(nodes, nil)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("len(levels) = %d, want 3; got %v", len(levels), levels)
	}
	if got := levels[0]; len(got) != 1 || got[0] != "A" {
		t.Errorf("levels[0] = %v, want [A]", got)
	}
	if got := levels[1]; len(got) != 2 || got[0] != "B" || got[1] != "C" {
		t.Errorf("levels[1] = %v, want [B C]", got)
	}
	if got := levels[2]; len(got) != 1 || got[0] != "D" {
		t.Errorf("levels[2] = %v, want [D]", got)
	}
}

func TestTopoSort_SingleNodePlan(t *testing.T) {
	nodes := []decomposer.Node{{ID: "n1", Payload: "solo"}}
	levels, err := TopoSort(nodes, nil)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(levels) != 1 || len(levels[0]) != 1 || levels[0][0] != "n1" {
		t.Fatalf("got %v, want [[n1]]", levels)
	}
}

func TestTopoSort_EmptyPlan(t *testing.T) {
	levels, err := TopoSort(nil, nil)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(levels) != 0 {
		t.Errorf("got %v, want []", levels)
	}
}

func TestValidate_DetectsCycle(t *testing.T) {
	nodes := []decomposer.Node{
		{ID: "A", DependsOn: []string{"B"}, Payload: "a"},
		{ID: "B", DependsOn: []string{"A"}, Payload: "b"},
	}
	err := Validate(nodes, nil)
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected 'cycle' in error, got %v", err)
	}
}

func TestValidate_DanglingDep(t *testing.T) {
	nodes := []decomposer.Node{
		{ID: "A", DependsOn: []string{"Z"}, Payload: "a"},
	}
	err := Validate(nodes, nil)
	if err == nil {
		t.Fatal("expected error for dangling dep, got nil")
	}
	if !strings.Contains(err.Error(), "unknown dependency") {
		t.Errorf("expected 'unknown dependency' in error, got %v", err)
	}
}

func TestValidate_DanglingEdge(t *testing.T) {
	nodes := []decomposer.Node{{ID: "A", Payload: "a"}}
	edges := []decomposer.Edge{{From: "Z", To: "A"}}
	err := Validate(nodes, edges)
	if err == nil {
		t.Fatal("expected error for dangling edge, got nil")
	}
	if !strings.Contains(err.Error(), "unknown node") {
		t.Errorf("expected 'unknown node' in error, got %v", err)
	}
}

func TestValidate_SelfDep(t *testing.T) {
	nodes := []decomposer.Node{{ID: "A", DependsOn: []string{"A"}, Payload: "a"}}
	err := Validate(nodes, nil)
	if err == nil {
		t.Fatal("expected error for self-dep, got nil")
	}
}

func TestValidate_EmptyNodeID(t *testing.T) {
	nodes := []decomposer.Node{{ID: "", Payload: "a"}}
	err := Validate(nodes, nil)
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}

func TestRoots(t *testing.T) {
	nodes := []decomposer.Node{
		{ID: "A", Payload: "a"},
		{ID: "B", DependsOn: []string{"A"}, Payload: "b"},
		{ID: "C", Payload: "c"},
	}
	roots := Roots(nodes)
	if len(roots) != 2 || roots[0] != "A" || roots[1] != "C" {
		t.Errorf("roots = %v, want [A C]", roots)
	}
}

// --- executor tests ---

// recordingSink captures all emitted events for assertions.
type recordingSink struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Event string
	Data  any
}

func (s *recordingSink) Emit(event string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, recordedEvent{Event: event, Data: data})
	return nil
}

func (s *recordingSink) all() []recordedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedEvent(nil), s.events...)
}

func (s *recordingSink) countByEvent(event string) int {
	n := 0
	for _, e := range s.all() {
		if e.Event == event {
			n++
		}
	}
	return n
}

func (s *recordingSink) nodesByStatus(status string) []string {
	var out []string
	for _, e := range s.all() {
		if e.Event != "node_status" {
			continue
		}
		m, _ := e.Data.(map[string]any)
		if m == nil {
			continue
		}
		if m["status"] == status {
			out = append(out, m["node_id"].(string))
		}
	}
	return out
}

func TestExecutor_RunsAllNodes(t *testing.T) {
	plan := newPlan(t,
		node("n1", "", "first payload"),
		node("n2", "", "second payload"),
		node("n3", "n1", "third depends on n1"),
		node("n4", "n2", "fourth depends on n2"),
	)
	reg := providers.NewRegistry(nil)
	sink := &recordingSink{}
	exec := New(plan, nil, reg, sink)
	exec.Parallelism = 4

	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every node emitted both "running" and "ok".
	ran := sink.nodesByStatus("running")
	ok := sink.nodesByStatus("ok")
	if len(ran) != 4 || len(ok) != 4 {
		t.Errorf("ran=%v ok=%v, want 4 each", ran, ok)
	}
	// Done event was emitted.
	if c := sink.countByEvent("done"); c != 1 {
		t.Errorf("done events = %d, want 1", c)
	}
}

func TestExecutor_RespectsParallelism(t *testing.T) {
	// Two root nodes → level 0 has 2 nodes. With parallelism=2 they should
	// both start; if we use parallelism=1 they would serialize. We just
	// assert that the run completes regardless of parallelism setting.
	for _, par := range []int{1, 2, 4, 8} {
		plan := newPlan(t,
			node("n1", "", "1"),
			node("n2", "", "2"),
			node("n3", "n1", "3"),
			node("n4", "n2", "4"),
		)
		reg := providers.NewRegistry(nil)
		sink := &recordingSink{}
		exec := New(plan, nil, reg, sink)
		exec.Parallelism = par
		if err := exec.Run(context.Background()); err != nil {
			t.Fatalf("par=%d: %v", par, err)
		}
		if got := len(sink.nodesByStatus("ok")); got != 4 {
			t.Errorf("par=%d: ok nodes = %d, want 4", par, got)
		}
	}
}

func TestExecutor_PropagatesUpstreamText(t *testing.T) {
	// n1 produces a known phrase; n2 depends on n1; we capture the prompt
	// n2 received by hooking a custom Streamer for n2. The capture registry
	// captures every prompt it sees; n2's prompt will be the last one to
	// overwrite capturedPrompt. (n1's "ok" emission becomes the upstream
	// text in n2's prompt.)
	plan := newPlan(t,
		node("n1", "", "first task"),
		node("n2", "n1", "second task depends on first"),
	)

	capturedPrompts := []string{}
	reg := &captureRegistry{
		capture: func(p string) { capturedPrompts = append(capturedPrompts, p) },
	}
	sink := &recordingSink{}
	exec := New(plan, nil, reg, sink)
	exec.Parallelism = 4

	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(capturedPrompts) < 2 {
		t.Fatalf("expected 2 captured prompts, got %d", len(capturedPrompts))
	}
	// The prompt containing "second task depends on first" is n2's.
	var n2Prompt string
	for _, p := range capturedPrompts {
		if strings.Contains(p, "second task depends on first") {
			n2Prompt = p
			break
		}
	}
	if n2Prompt == "" {
		t.Fatalf("did not capture n2's prompt among %d captures", len(capturedPrompts))
	}
	if !strings.Contains(n2Prompt, "===UPSTREAM: n1") {
		t.Errorf("n2 prompt missing upstream header; got:\n%s", n2Prompt)
	}
	// The separator constant is the prefix used to build the header.
	// The full header is "===UPSTREAM: n1 (id=n1, role=)===".
	if !strings.Contains(n2Prompt, "===UPSTREAM:") {
		t.Errorf("n2 prompt missing upstream marker; got:\n%s", n2Prompt)
	}
	// And the upstream content (n1's mock output) must be present.
	if !strings.Contains(n2Prompt, "ok") {
		t.Errorf("n2 prompt missing upstream content; got:\n%s", n2Prompt)
	}
}

func TestExecutor_StopsOnCtxCancel(t *testing.T) {
	plan := newPlan(t,
		node("n1", "", "n1 payload"),
		node("n2", "", "n2 payload"),
		node("n3", "n1", "n3 payload"),
		node("n4", "n2", "n4 payload"),
	)
	reg := providers.NewRegistry(nil)
	sink := &recordingSink{}
	exec := New(plan, nil, reg, sink)
	exec.Parallelism = 4

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the run starts but doesn't finish cleanly.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_ = exec.Run(ctx)
	// We don't assert exact counts because timing is non-deterministic; we
	// just assert the run returned without a fatal error and that the done
	// event was emitted with cancelled=true (or at least emitted).
	seen := false
	for _, e := range sink.all() {
		if e.Event != "done" {
			continue
		}
		m, _ := e.Data.(map[string]any)
		if m == nil {
			continue
		}
		if c, ok := m["cancelled"].(bool); ok {
			_ = c
		}
		seen = true
	}
	if !seen {
		t.Error("expected a done event after ctx cancel")
	}
}

func TestExecutor_NilPlanErrors(t *testing.T) {
	sink := &recordingSink{}
	exec := New(nil, nil, providers.NewRegistry(nil), sink)
	if err := exec.Run(context.Background()); err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestExecutor_NilSinkErrors(t *testing.T) {
	plan := newPlan(t, node("n1", "", "x"))
	exec := New(plan, nil, providers.NewRegistry(nil), nil)
	if err := exec.Run(context.Background()); err == nil {
		t.Error("expected error for nil sink")
	}
}

func TestExecutor_UsesRouterDecision(t *testing.T) {
	plan := newPlan(t, node("n1", "", "x"))
	reg := providers.NewRegistry(nil)
	sink := &recordingSink{}
	bench, _ := router.LoadBenchmarks()
	costs, _ := router.LoadCostTable()
	rtr, _ := router.NewWeighted(router.WeightedConfig{Bench: bench, Costs: costs})
	exec := New(plan, rtr, reg, sink)
	exec.Parallelism = 1
	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The running event should include a model field.
	for _, e := range sink.all() {
		if e.Event != "node_status" {
			continue
		}
		m, _ := e.Data.(map[string]any)
		if m == nil || m["status"] != "running" {
			continue
		}
		if _, ok := m["model"].(string); !ok {
			t.Errorf("running event missing model: %v", m)
		}
		if _, ok := m["score"]; !ok {
			t.Errorf("running event missing score: %v", m)
		}
		return
	}
	t.Error("no running event found")
}

// captureRegistry wraps a simple in-memory registry that captures every
// request prompt sent to it.
type captureRegistry struct {
	mu      sync.Mutex
	capture func(prompt string)
}

func (c *captureRegistry) Get(model string) (providers.Streamer, error) {
	return &captureStreamer{capture: func(p string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.capture(p)
	}}, nil
}
func (c *captureRegistry) List() []string { return []string{"mock"} }

type captureStreamer struct{ capture func(string) }

func (c *captureStreamer) Name() string { return "capture" }
func (c *captureStreamer) Stream(ctx context.Context, req providers.Request, sink providers.ChunkSink) error {
	c.capture(req.Prompt)
	// Emit a single short chunk + finish.
	_ = sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: "mock:echo-v1", Text: "ok ",
	})
	return sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: "mock:echo-v1", Finish: true,
	})
}

// --- helpers ---

func node(id, dep string, payload string) decomposer.Node {
	var deps []string
	if dep != "" {
		deps = []string{dep}
	}
	return decomposer.Node{
		ID:        id,
		Role:      decomposer.RoleResearcher,
		Payload:   payload,
		DependsOn: deps,
		Requires: decomposer.Requirements{
			TaskClass:       decomposer.ClassFactual,
			Modality:        decomposer.ModalityText,
			LatencyBudgetMS: 5000,
			MaxCostUSD:      0.05,
		},
	}
}

func newPlan(t *testing.T, nodes ...decomposer.Node) *decomposer.Plan {
	t.Helper()
	var edges []decomposer.Edge
	for _, n := range nodes {
		for _, d := range n.DependsOn {
			edges = append(edges, decomposer.Edge{From: d, To: n.ID})
		}
	}
	return &decomposer.Plan{
		Version: "plan.v1",
		Nodes:   nodes,
		Edges:   edges,
	}
}

// Sanity: errors.As should work for ErrTemporalNotImplemented.
func TestTemporalStubErrors(t *testing.T) {
	var te TemporalExecutor
	err := te.Run(context.Background())
	if !errors.Is(err, ErrTemporalNotImplemented) {
		t.Errorf("err = %v, want ErrTemporalNotImplemented", err)
	}
	if ParseMode("local") != ModeLocal {
		t.Error("ParseMode(local) != ModeLocal")
	}
	if ParseMode("temporal") != ModeTemporal {
		t.Error("ParseMode(temporal) != ModeTemporal")
	}
	if ParseMode("garbage") != ModeLocal {
		t.Error("ParseMode(garbage) should default to ModeLocal")
	}
}

// Sanity: buildPrompt concatenates upstream outputs deterministically.
func TestBuildPrompt(t *testing.T) {
	var m sync.Map
	m.Store("n1", "first content")
	m.Store("n2", "second content")
	n := decomposer.Node{
		ID:        "n3",
		DependsOn: []string{"n1", "n2"},
		Payload:   "do something",
	}
	got := buildPrompt(n, &m)
	if !strings.Contains(got, "===UPSTREAM: n1") || !strings.Contains(got, "===UPSTREAM: n2") {
		t.Errorf("missing upstream headers:\n%s", got)
	}
	// Order must be deterministic: n1 before n2 (sorted).
	i := strings.Index(got, "===UPSTREAM: n1")
	j := strings.Index(got, "===UPSTREAM: n2")
	if i > j {
		t.Errorf("upstream order not deterministic: n1 at %d, n2 at %d", i, j)
	}
	if !strings.HasSuffix(got, "do something") {
		t.Errorf("payload not at end of prompt:\n%s", got)
	}
}

// Ensure fmt is referenced (avoids unused import in some build modes).
var _ = fmt.Sprintf

// --- Phase 5: fusion integration tests ---

func TestExecutor_InvokesFuserAndEmitsManifest(t *testing.T) {
	plan := newPlan(t,
		node("n1", "", "first output"),
		node("n2", "", "second output"),
		node("n3", "n1", "depends on n1"),
	)
	reg := providers.NewRegistry(nil)
	sink := &recordingSink{}
	exec := New(plan, nil, reg, sink)
	exec.Parallelism = 4
	exec.Fuser = stubFuser{text: "FUSED ANSWER with [1] and [2]"}

	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Find a fusion_start event.
	var sawFusionStart, sawFusionChunk, sawManifest bool
	for _, e := range sink.all() {
		switch e.Event {
		case "fusion_start":
			sawFusionStart = true
		case "fusion":
			sawFusionChunk = true
		case "manifest":
			sawManifest = true
		}
	}
	if !sawFusionStart {
		t.Error("missing fusion_start event")
	}
	if !sawFusionChunk {
		t.Error("missing fusion chunk event")
	}
	if !sawManifest {
		t.Error("missing manifest event")
	}
}

func TestExecutor_NoFuserStillWorks(t *testing.T) {
	plan := newPlan(t, node("n1", "", "x"))
	sink := &recordingSink{}
	exec := New(plan, nil, providers.NewRegistry(nil), sink)
	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, e := range sink.all() {
		if e.Event == "fusion_start" || e.Event == "manifest" {
			t.Errorf("unexpected fusion event when Fuser is nil: %v", e)
		}
	}
}

// stubFuser is a test Fuser that emits a single chunk + finish.
type stubFuser struct{ text string }

func (s stubFuser) Fuse(ctx context.Context, fr fusion.FusionRequest, sink providers.ChunkSink) error {
	_ = sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: "fusion", Model: "stub",
		Text: s.text,
	})
	return sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: "fusion", Model: "stub", Finish: true,
	})
}

// --- Phase 6: RAG injection ---

// ragCap captures the SystemMsg passed to the streamer so the test can
// assert on the injected DOC markers.
type ragCap struct {
	mu        sync.Mutex
	systemMsg string
	prompt    string
}

func (r *ragCap) capture(prompt, systemMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompt = prompt
	r.systemMsg = systemMsg
}

func TestExecutor_RAGInjectsDocsIntoSystemMsg(t *testing.T) {
	store := rag.NewMemStore()
	_ = store.UpsertDoc(context.Background(), rag.Document{
		ID: "doc_legal", WorkspaceID: "ws1", Title: "NDA",
	})
	_ = store.UpsertChunks(context.Background(), []rag.Chunk{
		{ID: "c1", DocID: "doc_legal", WorkspaceID: "ws1",
			Text: "the terminating party shall provide thirty days notice", CharStart: 0, CharEnd: 60},
	})
	svc := rag.NewService(store, nil)

	plan := newPlan(t, node("n1", "", "What is the termination clause?"))
	plan.Nodes[0].NeedsRAG = true

	cap := &ragCap{}
	reg := &captureRegistry{capture: func(prompt string) {
		// We need the SystemMsg too; reuse the streamer below.
	}}
	// Replace the underlying streamer with one that captures both.
	reg2 := ragRegistryWithCapture(cap)
	_ = reg

	sink := &recordingSink{}
	exec := New(plan, nil, reg2, sink)
	exec.RAG = svc
	exec.WorkspaceID = "ws1"
	if err := exec.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.systemMsg == "" {
		t.Fatal("expected non-empty system message")
	}
	if !strings.Contains(cap.systemMsg, "===DOC 1") {
		t.Fatalf("expected ===DOC 1 marker, got %q", cap.systemMsg)
	}
	if !strings.Contains(cap.systemMsg, "doc_legal") {
		t.Fatalf("expected doc_legal in system msg, got %q", cap.systemMsg)
	}
}

func TestExecutor_NoRAGLeavesSystemMsgAlone(t *testing.T) {
	plan := newPlan(t, node("n1", "", "no rag"))
	plan.Nodes[0].NeedsRAG = false
	cap := &ragCap{}
	reg := ragRegistryWithCapture(cap)
	exec := New(plan, nil, reg, &recordingSink{})
	exec.RAG = rag.NewService(rag.NewMemStore(), nil)
	if err := exec.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if strings.Contains(cap.systemMsg, "===DOC ") {
		t.Fatalf("expected no DOC marker when NeedsRAG=false, got %q", cap.systemMsg)
	}
}

func TestExecutor_RAGRetrievalFailureIsNonFatal(t *testing.T) {
	plan := newPlan(t, node("n1", "", "what about X?"))
	plan.Nodes[0].NeedsRAG = true
	cap := &ragCap{}
	reg := ragRegistryWithCapture(cap)
	exec := New(plan, nil, reg, &recordingSink{})
	exec.RAG = rag.NewService(rag.NewMemStore(), nil)
	if err := exec.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if strings.Contains(cap.systemMsg, "===DOC ") {
		t.Fatalf("expected no DOC marker when retrieval returns no results, got %q", cap.systemMsg)
	}
}

// ragRegistryWithCapture returns a registry whose streamer captures both
// the prompt and the system message into the given recorder.
func ragRegistryWithCapture(rec *ragCap) providers.Registry {
	return &ragCaptureRegistry{rec: rec}
}

type ragCaptureRegistry struct{ rec *ragCap }

func (r *ragCaptureRegistry) Get(_ string) (providers.Streamer, error) {
	return &ragCaptureStreamer{rec: r.rec}, nil
}
func (r *ragCaptureRegistry) List() []string { return []string{"mock"} }

type ragCaptureStreamer struct{ rec *ragCap }

func (r *ragCaptureStreamer) Name() string { return "mock" }
func (r *ragCaptureStreamer) Stream(ctx context.Context, req providers.Request, sink providers.ChunkSink) error {
	r.rec.capture(req.Prompt, req.SystemMsg)
	_ = sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: req.Model, Text: "ok",
	})
	return sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: req.Model, Finish: true,
	})
}
