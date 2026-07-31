package providers

import (
	"context"
	"strings"
	"sync"
)

// scriptedStreamer returns each entry from `responses` for successive calls,
// wrapping in a single text chunk followed by a Finish chunk. Once exhausted
// it returns the last response (handy for tests that don't care about count).
type scriptedStreamer struct {
	mu       sync.Mutex
	idx      int
	responses []string
}

func (s *scriptedStreamer) Name() string { return "scripted" }

func (s *scriptedStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	s.mu.Lock()
	idx := s.idx
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	text := s.responses[idx]
	s.idx++
	s.mu.Unlock()

	if err := sink.Send(ctx, Chunk{
		V:        "chunk.v1",
		StreamID: req.StreamID,
		NodeID:   req.NodeID,
		Model:    req.Model,
		Text:     text,
		Conf:     1.0,
	}); err != nil {
		return err
	}
	return sink.Send(ctx, FinishChunk(req, req.Model))
}

// NewRegistryForTest returns a registry whose mock provider is replaced with
// a scripted streamer that emits the given canned responses in order. Used
// only from tests; do not call from production code.
//
// If responses is empty the registry is the same as NewRegistry(nil).
func NewRegistryForTest(responses []string) Registry {
	r := NewRegistry(nil).(*registry)
	if len(responses) > 0 {
		r.streamers["mock"] = &scriptedStreamer{responses: responses}
	}
	return r
}

// RegisterStreamer installs a custom Streamer under a model prefix (e.g.
// "mock" or "judge") so tests can inject scripted behavior. The test
// registry returned by NewRegistryForTest is *registry (not the interface),
// so callers can mutate it directly. This helper is provided for symmetry
// with that mutation pattern.
func (r *registry) RegisterStreamer(prefix string, s Streamer) {
	r.streamers[prefix] = s
}

// Ensure unused imports don't fire on stub streamers.
var _ = strings.TrimSpace
