package fusion

import (
	"context"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/citation"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

// recordingSink collects the full output text + counts.
type recordingSink struct {
	text   strings.Builder
	chunks int
	finish bool
}

func (r *recordingSink) Send(ctx context.Context, c providers.Chunk) error {
	r.chunks++
	if c.Text != "" {
		r.text.WriteString(c.Text)
	}
	if c.Finish {
		r.finish = true
	}
	return nil
}

func (r *recordingSink) String() string { return r.text.String() }

func TestHeuristicFusion_ConcatenatesWithCites(t *testing.T) {
	reg := providers.NewRegistry(nil)
	f := New(Config{Mode: ModeHeuristic}, reg)
	fr := FusionRequest{
		Prompt: "compare two companies",
		Streams: map[string]*NodeStream{
			"n1": {NodeID: "n1", Role: decomposer.RoleResearcher, Text: "Apple revenue grew 5%"},
			"n2": {NodeID: "n2", Role: decomposer.RoleResearcher, Text: "NVIDIA revenue grew 120%"},
		},
	}
	sink := &recordingSink{}
	if err := f.Fuse(context.Background(), fr, sink); err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	out := sink.String()
	if !strings.Contains(out, "Apple revenue grew 5%") {
		t.Errorf("missing stream n1 text: %s", out)
	}
	if !strings.Contains(out, "NVIDIA revenue grew 120%") {
		t.Errorf("missing stream n2 text: %s", out)
	}
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "[2]") {
		t.Errorf("missing [1]/[2] cite markers: %s", out)
	}
	if !sink.finish {
		t.Error("did not emit Finish chunk")
	}
}

func TestModelDriven_BuildsPromptWithSpans(t *testing.T) {
	// Build a registry whose streamer returns a canned response.
	reg := providers.NewRegistryForTest([]string{"merged answer with [1] and [2]"})
	f := New(Config{Mode: ModeModel, SynthModel: "mock:echo-v1"}, reg)
	fr := FusionRequest{
		Prompt: "user prompt",
		Streams: map[string]*NodeStream{
			"n1": {NodeID: "n1", Role: decomposer.RoleResearcher, Text: "claim one"},
			"n2": {NodeID: "n2", Role: decomposer.RoleResearcher, Text: "claim two"},
		},
	}
	sink := &recordingSink{}
	if err := f.Fuse(context.Background(), fr, sink); err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if !strings.Contains(sink.String(), "[1]") {
		t.Errorf("synthesizer output missing markers: %s", sink.String())
	}
}

func TestFuser_DefaultHeuristicForSingle(t *testing.T) {
	reg := providers.NewRegistry(nil)
	f := New(Config{Mode: ModeAuto}, reg)
	fr := FusionRequest{
		Streams: map[string]*NodeStream{
			"n1": {NodeID: "n1", Role: decomposer.RoleSynthesizer, Text: "single stream"},
		},
	}
	sink := &recordingSink{}
	if err := f.Fuse(context.Background(), fr, sink); err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if !strings.Contains(sink.String(), "single stream") {
		t.Errorf("missing stream text")
	}
}

func TestFuser_DefaultModelForMulti(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{"multi [1] [2]"})
	f := New(Config{Mode: ModeAuto}, reg)
	fr := FusionRequest{
		Streams: map[string]*NodeStream{
			"n1": {NodeID: "n1", Role: decomposer.RoleResearcher, Text: "alpha"},
			"n2": {NodeID: "n2", Role: decomposer.RoleResearcher, Text: "beta"},
		},
	}
	sink := &recordingSink{}
	if err := f.Fuse(context.Background(), fr, sink); err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if !strings.Contains(sink.String(), "[1]") {
		t.Errorf("expected model-driven output with markers")
	}
}

func TestFuser_EmptyStreamsEmitsFinish(t *testing.T) {
	reg := providers.NewRegistry(nil)
	f := New(Config{Mode: ModeAuto}, reg)
	fr := FusionRequest{
		Plan:    &decomposer.Plan{Version: "plan.v1"},
		Streams: map[string]*NodeStream{},
	}
	sink := &recordingSink{}
	if err := f.Fuse(context.Background(), fr, sink); err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if !sink.finish {
		t.Error("expected finish chunk")
	}
}

func TestFuser_NilPlanAndEmptyStreamsErrors(t *testing.T) {
	reg := providers.NewRegistry(nil)
	f := New(Config{Mode: ModeHeuristic}, reg)
	err := f.Fuse(context.Background(), FusionRequest{}, &recordingSink{})
	if err == nil {
		t.Error("expected error for nil plan + empty streams")
	}
}

func TestBuildSynthPrompt_IncludesAll(t *testing.T) {
	fr := FusionRequest{
		Prompt: "user's question",
		Streams: map[string]*NodeStream{
			"n1": {NodeID: "n1", Role: decomposer.RoleResearcher, Text: "alpha alpha"},
			"n2": {NodeID: "n2", Role: decomposer.RoleResearcher, Text: "beta beta"},
		},
	}
	got := buildSynthPrompt(fr)
	if !strings.Contains(got, "user's question") {
		t.Errorf("prompt missing user prompt")
	}
	if !strings.Contains(got, "alpha alpha") || !strings.Contains(got, "beta beta") {
		t.Errorf("prompt missing stream texts")
	}
	if !strings.Contains(got, "SPAN TABLE") {
		t.Errorf("prompt missing SPAN TABLE")
	}
	if !strings.Contains(got, "[1]") || !strings.Contains(got, "[2]") {
		t.Errorf("prompt missing span numbers")
	}
}

func TestShortQuote_Truncates(t *testing.T) {
	in := strings.Repeat("x", 300)
	got := shortQuote(in, 50)
	if len(got) > 60 { // 50 + ellipsis
		t.Errorf("shortQuote too long: %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("shortQuote missing ellipsis: %q", got)
	}
}

func TestCitationManifest_Roundtrip(t *testing.T) {
	m := citation.New()
	m.Add(citation.Span{SubTaskID: "n1", Model: "m1", Text: "alpha"})
	m.Add(citation.Span{SubTaskID: "n2", Model: "m2", Text: "beta"})
	if len(m.Spans) != 2 {
		t.Errorf("len(Spans) = %d, want 2", len(m.Spans))
	}
}

// --- Judge tests ---

func TestJudge_VerdictShape(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{
		"```json\n{\"verdict\":\"A\",\"confidence\":0.82,\"reasoning\":\"A cites the 10-K filing\",\"winners\":[\"sp_001\"]}\n```",
	})
	j := &Judge{Registry: reg, Model: "mock:echo-v1"}
	v, err := j.Compare(context.Background(),
		Claim{Text: "A: rev grew 5%", SpanID: "sp_001", NodeID: "n3", Model: "m1"},
		Claim{Text: "B: rev fell 3%", SpanID: "sp_002", NodeID: "n4", Model: "m2"},
	)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if v.Pick != "A" {
		t.Errorf("Pick = %q, want A", v.Pick)
	}
	if v.Confidence != 0.82 {
		t.Errorf("Confidence = %v, want 0.82", v.Confidence)
	}
	if len(v.Winners) != 1 || v.Winners[0] != "sp_001" {
		t.Errorf("Winners = %v, want [sp_001]", v.Winners)
	}
}

func TestJudge_VerdictPlainJSON(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{
		`{"verdict":"tie","confidence":0.5,"reasoning":"both equally supported","winners":[]}`,
	})
	j := &Judge{Registry: reg, Model: "mock:echo-v1"}
	v, err := j.Compare(context.Background(),
		Claim{Text: "x", SpanID: "sp_001", NodeID: "n1", Model: "m"},
		Claim{Text: "y", SpanID: "sp_002", NodeID: "n2", Model: "m"},
	)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if v.Pick != "tie" {
		t.Errorf("Pick = %q, want tie", v.Pick)
	}
}

func TestJudge_MalformedVerdictErrors(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{"not json at all"})
	j := &Judge{Registry: reg, Model: "mock:echo-v1"}
	_, err := j.Compare(context.Background(),
		Claim{Text: "x", SpanID: "sp_001", NodeID: "n1", Model: "m"},
		Claim{Text: "y", SpanID: "sp_002", NodeID: "n2", Model: "m"},
	)
	if err == nil {
		t.Error("expected error for malformed verdict")
	}
}

func TestJudge_InvalidPickErrors(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{
		`{"verdict":"C","confidence":0.5,"reasoning":"","winners":[]}`,
	})
	j := &Judge{Registry: reg, Model: "mock:echo-v1"}
	_, err := j.Compare(context.Background(),
		Claim{Text: "x", SpanID: "sp_001", NodeID: "n1", Model: "m"},
		Claim{Text: "y", SpanID: "sp_002", NodeID: "n2", Model: "m"},
	)
	if err == nil {
		t.Error("expected error for invalid pick")
	}
}

func TestJudge_BadConfidenceErrors(t *testing.T) {
	reg := providers.NewRegistryForTest([]string{
		`{"verdict":"A","confidence":2.5,"reasoning":"","winners":[]}`,
	})
	j := &Judge{Registry: reg, Model: "mock:echo-v1"}
	_, err := j.Compare(context.Background(),
		Claim{Text: "x", SpanID: "sp_001", NodeID: "n1", Model: "m"},
		Claim{Text: "y", SpanID: "sp_002", NodeID: "n2", Model: "m"},
	)
	if err == nil {
		t.Error("expected error for out-of-range confidence")
	}
}

// --- Jaccard similarity tests ---

func TestJaccard_IdenticalIsOne(t *testing.T) {
	s := "the apple revenue grew five percent last year"
	if got := Jaccard(s, s); got != 1.0 {
		t.Errorf("identical strings: Jaccard = %v, want 1.0", got)
	}
}

func TestJaccard_DisjointIsZero(t *testing.T) {
	a := "apple banana cherry"
	b := "delta echo foxtrot"
	if got := Jaccard(a, b); got != 0.0 {
		t.Errorf("disjoint strings: Jaccard = %v, want 0.0", got)
	}
}

func TestJaccard_Overlap(t *testing.T) {
	a := "the quick brown fox jumps"
	b := "the lazy brown fox sleeps"
	got := Jaccard(a, b)
	// shared: the, brown, fox (3); union = {the, quick, brown, fox, jumps, lazy, sleeps} (7)
	// 3/7 ≈ 0.43
	if got < 0.4 || got > 0.5 {
		t.Errorf("Jaccard = %v, want ~0.43", got)
	}
}

func TestJaccard_EmptyIsZero(t *testing.T) {
	if got := Jaccard("", ""); got != 0 {
		t.Errorf("empty/empty: %v, want 0", got)
	}
	if got := Jaccard("", "alpha"); got != 0 {
		t.Errorf("empty/non-empty: %v, want 0", got)
	}
}

func TestStripFences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"```\n{\"a\":1}\n```", "{\"a\":1}"},
		{"{\"a\":1}", "{\"a\":1}"},
		{"  plain  ", "plain"},
	}
	for _, c := range cases {
		if got := stripFences(c.in); got != c.want {
			t.Errorf("stripFences(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}