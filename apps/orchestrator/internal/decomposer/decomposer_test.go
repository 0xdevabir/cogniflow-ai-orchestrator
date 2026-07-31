package decomposer

import (
	"context"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// TestValidate_WorkedExample: the worked example from prompts/decomposer.v1.md
// round-trips through Validate.
func TestValidate_WorkedExample(t *testing.T) {
	p := &Plan{}
	if err := jsonUnmarshal([]byte(workedExampleJSON), p); err != nil {
		t.Fatal(err)
	}
	if err := Validate(p); err != nil {
		t.Fatalf("worked example failed validation: %v", err)
	}
}

// TestValidate_RejectsBadRole: a node with role="hacker" is rejected.
func TestValidate_RejectsBadRole(t *testing.T) {
	raw := []byte(`{
		"version":"plan.v1",
		"nodes":[
			{"id":"n1","role":"hacker","payload":"exploit","depends_on":[],
			 "needs_rag":false,
			 "requires":{"task_class":"reasoning","modality":"text","latency_budget_ms":1000,"max_cost_usd":0.1}
			}
		],
		"edges":[]
	}`)
	if err := ValidateJSON(raw); err == nil {
		t.Fatal("expected validation error for bad role")
	}
}

// TestPassthroughPlanValidates: the fallback single-node plan is valid.
func TestPassthroughPlanValidates(t *testing.T) {
	p := PassthroughPlan("hello world")
	if err := Validate(p); err != nil {
		t.Fatalf("passthrough must validate: %v", err)
	}
	if !isSyntheticSingleNode(p) {
		t.Fatal("expected passthrough flag to be true")
	}
}

// TestPassthroughSemantics: the single-node plan has the synthesizer role
// and an empty edges list.
func TestPassthroughSemantics(t *testing.T) {
	p := PassthroughPlan("anything")
	if len(p.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(p.Nodes))
	}
	if p.Nodes[0].Role != RoleSynthesizer {
		t.Fatalf("expected synthesizer role, got %q", p.Nodes[0].Role)
	}
	if len(p.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(p.Edges))
	}
}

// TestSemanticCheck_MaxSix: too-many-nodes is rejected.
func TestSemanticCheck_MaxSix(t *testing.T) {
	var nodes []Node
	for i := 0; i < 7; i++ {
		nodes = append(nodes, Node{
			ID: "n" + itoa(i), Role: RoleSynthesizer, Payload: "x",
			DependsOn: []string{},
			Requires:  Requirements{TaskClass: ClassReasoning, Modality: ModalityText, LatencyBudgetMS: 1000, MaxCostUSD: 0.1},
		})
	}
	p := &Plan{Version: "plan.v1", Nodes: nodes, Edges: []Edge{}}
	if err := semanticCheck(p); err == nil {
		t.Fatal("expected error for 7 nodes")
	}
}

// TestSemanticCheck_UnknownDep: unknown dep is rejected.
func TestSemanticCheck_UnknownDep(t *testing.T) {
	p := &Plan{Version: "plan.v1", Nodes: []Node{
		{ID: "n1", Role: RoleSynthesizer, Payload: "x", DependsOn: []string{"ghost"},
			Requires: Requirements{TaskClass: ClassReasoning, Modality: ModalityText, LatencyBudgetMS: 1000, MaxCostUSD: 0.1}},
	}, Edges: []Edge{}}
	if err := semanticCheck(p); err == nil {
		t.Fatal("expected error for unknown dep")
	}
}

// TestSemanticCheck_SelfDep: self-dependency is rejected.
func TestSemanticCheck_SelfDep(t *testing.T) {
	p := &Plan{Version: "plan.v1", Nodes: []Node{
		{ID: "n1", Role: RoleSynthesizer, Payload: "x", DependsOn: []string{"n1"},
			Requires: Requirements{TaskClass: ClassReasoning, Modality: ModalityText, LatencyBudgetMS: 1000, MaxCostUSD: 0.1}},
	}, Edges: []Edge{}}
	if err := semanticCheck(p); err == nil {
		t.Fatal("expected error for self-dep")
	}
}

// TestStripFences: removes ```json ... ``` wrappers.
func TestStripFences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"plain text", "plain text"},
		{"```\n{\"b\":2}\n```", `{"b":2}`},
	}
	for _, tc := range cases {
		got := stripFences(tc.in)
		if got != tc.want {
			t.Errorf("stripFences(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestExtractJSON: finds the first balanced top-level object.
func TestExtractJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Here you go: {\"a\":1}", `{"a":1}`},
		{"{ }", `{}`},
		{"no braces at all", ""},
		{"{\"outer\":{\"inner\":1}}", `{"outer":{"inner":1}}`},
		{"{\"with \\\"quote\\\"\":1}", `{"with \"quote\"":1}`},
	}
	for _, tc := range cases {
		got := extractJSON(tc.in)
		if got != tc.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseAndValidate_Fenced: handles markdown-fenced JSON.
func TestParseAndValidate_Fenced(t *testing.T) {
	d := &Decomposer{}
	raw := "```json\n" + workedExampleJSON + "\n```"
	p, err := d.parseAndValidate(raw)
	if err != nil {
		t.Fatalf("parseAndValidate: %v", err)
	}
	if len(p.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(p.Nodes))
	}
}

// TestDecompose_EmptyPrompt: returns an error.
func TestDecompose_EmptyPrompt(t *testing.T) {
	reg := newFakeRegistry("anything", nil)
	d := New(Deps{Registry: reg, Model: "mock:echo-v1", Retries: 1})
	_, err := d.Decompose(context.Background(), "   ")
	if err == nil || !strings.Contains(err.Error(), "empty prompt") {
		t.Fatalf("expected empty-prompt error, got %v", err)
	}
}

// TestDecompose_StreamsAndParses: a fake Streamer emits the worked example
// in chunks; Decompose() should return the parsed plan (not passthrough).
func TestDecompose_StreamsAndParses(t *testing.T) {
	raw := workedExampleJSON
	// Make the LLM emit a markdown-fenced JSON.
	emitted := "```json\n" + raw + "\n```"
	reg := newFakeRegistry("mock", []string{emitted})
	d := New(Deps{Registry: reg, Model: "mock:echo-v1", Retries: 2})
	p, err := d.Decompose(context.Background(), "any prompt")
	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}
	if len(p.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(p.Nodes))
	}
	if p.Nodes[0].ID != "n1" {
		t.Errorf("first id = %q", p.Nodes[0].ID)
	}
}

// TestDecompose_FallbackOnExhaustedRetries: streamer always returns garbage.
// After Retries attempts, Decompose() returns the passthrough plan.
func TestDecompose_FallbackOnExhaustedRetries(t *testing.T) {
	reg := newFakeRegistry("mock", []string{"this is not json", "still not json", "nope"})
	d := New(Deps{Registry: reg, Model: "mock:echo-v1", Retries: 3})
	p, err := d.Decompose(context.Background(), "plan me a sandwich")
	if err != nil {
		t.Fatalf("expected passthrough (nil err), got err: %v", err)
	}
	if !isSyntheticSingleNode(p) {
		t.Fatal("expected single-node passthrough plan")
	}
	if p.Nodes[0].Payload != "plan me a sandwich" {
		t.Errorf("passthrough payload should echo the prompt, got %q", p.Nodes[0].Payload)
	}
}

// TestDecompose_RetrySucceeds: 1st attempt garbage, 2nd valid → succeeds.
func TestDecompose_RetrySucceeds(t *testing.T) {
	raw := workedExampleJSON
	emitted := "```json\n" + raw + "\n```"
	reg := newFakeRegistry("mock", []string{"garbage no json here", emitted})
	d := New(Deps{Registry: reg, Model: "mock:echo-v1", Retries: 3})
	p, err := d.Decompose(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if isSyntheticSingleNode(p) {
		t.Fatal("expected non-passthrough plan after successful retry")
	}
	if len(p.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(p.Nodes))
	}
}

// helpers

// isSyntheticSingleNode returns true if the plan is the single-node
// passthrough plan generated when the decomposer exhausted its retries.
func isSyntheticSingleNode(p *Plan) bool {
	if len(p.Nodes) != 1 {
		return false
	}
	return p.Nodes[0].Role == RoleSynthesizer && len(p.Edges) == 0
}

const workedExampleJSON = `{
  "version": "plan.v1",
  "nodes": [
    {
      "id": "n1",
      "role": "researcher",
      "payload": "Identify the best ramen and izakaya neighborhoods in Tokyo.",
      "depends_on": [],
      "needs_rag": false,
      "requires": { "task_class": "factual", "modality": "text", "latency_budget_ms": 12000, "max_cost_usd": 0.04 }
    },
    {
      "id": "n2",
      "role": "planner",
      "payload": "Build a 3-day itinerary that fits a $500 budget.",
      "depends_on": ["n1"],
      "needs_rag": false,
      "requires": { "task_class": "reasoning", "modality": "text", "latency_budget_ms": 18000, "max_cost_usd": 0.15 }
    },
    {
      "id": "n3",
      "role": "critic",
      "payload": "Compare NVIDIA vs Apple 2024 strategy.",
      "depends_on": [],
      "needs_rag": false,
      "requires": { "task_class": "factual", "modality": "text", "latency_budget_ms": 15000, "max_cost_usd": 0.10 }
    },
    {
      "id": "n4",
      "role": "synthesizer",
      "payload": "Merge itinerary + comparison into one answer.",
      "depends_on": ["n2", "n3"],
      "needs_rag": false,
      "requires": { "task_class": "reasoning", "modality": "text", "latency_budget_ms": 20000, "max_cost_usd": 0.20 }
    }
  ],
  "edges": [
    {"from": "n1", "to": "n2"},
    {"from": "n3", "to": "n4"},
    {"from": "n2", "to": "n4"}
  ]
}`

// itoa avoids importing strconv for test ergonomics.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// fakeRegistry returns a stream sequence for one model id. Each call to
// Stream() consumes one entry from the queue; after exhausting, it replays
// the last entry. This lets us drive the decomposer's retry path.
type fakeRegistry struct {
	name     string
	queue    []string
	index    int
	streamer *fakeStreamer
}

func newFakeRegistry(name string, queue []string) *fakeRegistry {
	r := &fakeRegistry{name: name}
	if len(queue) == 0 {
		queue = []string{"empty"}
	}
	r.queue = queue
	r.streamer = &fakeStreamer{name: name, getResp: func() string {
		// Round-robin; on the last entry, keep replaying it.
		i := r.index
		if i >= len(r.queue) {
			i = len(r.queue) - 1
		}
		r.index++
		return r.queue[i]
	}}
	return r
}

func (r *fakeRegistry) Get(model string) (providers.Streamer, error) {
	return r.streamer, nil
}

func (r *fakeRegistry) List() []string { return []string{r.name} }

// fakeStreamer emits a fixed response when Stream() is called. The response
// is split into a single chunk to mirror real providers.
type fakeStreamer struct {
	name    string
	getResp func() string
}

func (f *fakeStreamer) Name() string { return f.name }

func (f *fakeStreamer) Stream(ctx context.Context, req providers.Request, sink providers.ChunkSink) error {
	resp := f.getResp()
	// Emit in two chunks to exercise the chunk-accumulator.
	half := len(resp) / 2
	if half == 0 {
		half = 1
	}
	if err := sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: f.name, Text: resp[:half],
	}); err != nil {
		return err
	}
	if err := sink.Send(ctx, providers.Chunk{
		V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
		Model: f.name, Text: resp[half:],
	}); err != nil {
		return err
	}
	return sink.Send(ctx, providers.FinishChunk(req, f.name))
}
