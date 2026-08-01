package providers

import (
	"context"
	"strings"
	"time"
)

// mockStreamer is a deterministic, no-network Streamer used in dev and tests.
//
// It echoes the user's prompt back in 4-5 word chunks with a small delay, so
// the dev experience is predictable: "you typed X, you got X back". This also
// makes it obvious when real providers fall back to mock (the response is
// literally your own input). Always emits a final Finish: true chunk.
type mockStreamer struct{}

func newMock() *mockStreamer { return &mockStreamer{} }

func (m *mockStreamer) Name() string { return "mock" }

func splitTokens(s string) []string {
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
	if len(chunks) == 0 {
		chunks = []string{"[empty prompt]"}
	}
	return chunks
}

func (m *mockStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	// Echo the prompt back so it's obvious what the mock did. Prefix with
	// a banner so the response is unmistakably mock output.
	prefix := "[mock echo] "
	tokens := append([]string{}, prefix)
	tokens = append(tokens, splitTokens(req.Prompt)...)
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

