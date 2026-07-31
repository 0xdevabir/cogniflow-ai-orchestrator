package providers

import (
	"context"
	"hash/fnv"
	"strings"
	"time"
)

// mockStreamer is a deterministic, no-network Streamer used in dev and tests.
//
// It rotates among 4 canned responses based on the prompt hash, splitting
// the response into ~5-token chunks with a 20ms delay each. Always emits a
// final Finish: true chunk.
type mockStreamer struct{}

func newMock() *mockStreamer { return &mockStreamer{} }

func (m *mockStreamer) Name() string { return "mock" }

var mockResponses = []string{
	"Here is a concise summary of the topic: it balances tradeoffs, " +
		"and the key idea is that no single design wins everywhere.",
	"Let me walk through this step by step. First, the foundational " +
		"premise. Second, the practical implications. Third, the tradeoffs.",
	"There are three things to consider. (1) The economic incentive " +
		"structure. (2) The technical constraints. (3) The user experience.",
	"This is genuinely interesting because it sits at the intersection " +
		"of two different fields. The empirical evidence suggests one answer " +
		"while the theoretical literature suggests another.",
}

func splitTokens(s string) []string {
	// Split on whitespace boundaries but keep the chunks short.
	words := strings.Fields(s)
	const chunkSize = 5
	var chunks []string
	for i := 0; i < len(words); i += chunkSize {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
	}
	return chunks
}

func hashPick(s string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32()) % n
}

func (m *mockStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	resp := mockResponses[hashPick(req.Prompt, len(mockResponses))]
	tokens := splitTokens(resp)
	model := "mock:echo-v1"

	for _, tok := range tokens {
		select {
		case <-ctx.Done():
			return sink.Send(ctx, FinishChunk(req, model))
		default:
		}

		err := sink.Send(ctx, Chunk{
			V:        "chunk.v1",
			StreamID: req.StreamID,
			NodeID:   req.NodeID,
			Model:    model,
			Text:     tok + " ",
			Conf:     1.0,
		})
		if err != nil {
			return err
		}

		// Realistic-ish delay between tokens.
		select {
		case <-ctx.Done():
			return sink.Send(ctx, FinishChunk(req, model))
		case <-time.After(20 * time.Millisecond):
		}
	}

	return sink.Send(ctx, FinishChunk(req, model))
}
