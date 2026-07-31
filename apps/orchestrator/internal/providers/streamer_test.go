package providers

import (
	"context"
	"testing"
)

// collectorSink collects chunks into a slice for inspection in tests.
type collectorSink struct {
	chunks []Chunk
}

func (c *collectorSink) Send(ctx context.Context, ch Chunk) error {
	c.chunks = append(c.chunks, ch)
	return nil
}

func TestMockStreamer_EmitsFinish(t *testing.T) {
	// Mock uses a 20ms-per-chunk delay; keep the test fast by using
	// a short prompt that routes to the smallest mock response. The
	// response length is bounded so the test finishes in ~300ms.
	sink := &collectorSink{}
	ctx := context.Background()
	req := Request{Prompt: "explain CAP theorem in 3 sentences", StreamID: "default", NodeID: "default"}

	if err := newMock().Stream(ctx, req, sink); err != nil {
		t.Fatalf("mock stream failed: %v", err)
	}

	if len(sink.chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	last := sink.chunks[len(sink.chunks)-1]
	if !last.Finish {
		t.Fatal("last chunk must have Finish: true")
	}
	if last.V != "chunk.v1" {
		t.Fatalf("chunk.v1 version mismatch: %q", last.V)
	}
	if last.Model == "" {
		t.Fatal("finish chunk must carry Model")
	}
}

func TestMockStreamer_RespectsCtx(t *testing.T) {
	sink := &collectorSink{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_ = newMock().Stream(ctx, Request{Prompt: "x", StreamID: "default"}, sink)

	// Even when cancelled, the streamer must emit a Finish chunk (release contract).
	finished := false
	for _, c := range sink.chunks {
		if c.Finish {
			finished = true
		}
	}
	if !finished {
		t.Fatal("mock must emit Finish chunk even on ctx cancellation")
	}
}

func TestStreamer_ChunkJSONShape(t *testing.T) {
	c := Chunk{
		V: "chunk.v1", StreamID: "n1", NodeID: "n1", Model: "openai:gpt-4o-mini",
		Text: "hello", Conf: 0.9, Cite: []SpanRef{{SubTaskID: "n1", Model: "openai:gpt-4o-mini", CharStart: 10, CharEnd: 15}},
	}
	b, err := MarshalSSE(c)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, key := range []string{`"v":"chunk.v1"`, `"stream_id":"n1"`, `"node_id":"n1"`, `"model":"openai:gpt-4o-mini"`, `"text":"hello"`, `"conf":0.9`, `"cite":[`, `"sub_task_id":"n1"`, `"char_start":10`, `"char_end":15`} {
		if !contains(got, key) {
			t.Errorf("chunk JSON missing key %q in %s", key, got)
		}
	}
}

func TestParseModel(t *testing.T) {
	cases := []struct {
		in            string
		wantPrefix    string
		wantModelID   string
	}{
		{"openai:gpt-4o-mini", "openai", "gpt-4o-mini"},
		{"anthropic:claude-3-5-sonnet-latest", "anthropic", "claude-3-5-sonnet-latest"},
		{"mock", "mock", ""},
		{"", "", ""},
		{"strange:foo:bar", "strange", "foo:bar"},
	}
	for _, tc := range cases {
		gotP, gotM := ParseModel(tc.in)
		if gotP != tc.wantPrefix || gotM != tc.wantModelID {
			t.Errorf("ParseModel(%q) = (%q,%q), want (%q,%q)", tc.in, gotP, gotM, tc.wantPrefix, tc.wantModelID)
		}
	}
}

func TestFinishChunk(t *testing.T) {
	c := FinishChunk(Request{StreamID: "n1", NodeID: "n1", Model: "openai:gpt-4o-mini"}, "openai:gpt-4o-mini")
	if c.V != "chunk.v1" || !c.Finish || c.StreamID != "n1" || c.NodeID != "n1" || c.Model != "openai:gpt-4o-mini" {
		t.Fatalf("FinishChunk malformed: %+v", c)
	}
}

// helper
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}