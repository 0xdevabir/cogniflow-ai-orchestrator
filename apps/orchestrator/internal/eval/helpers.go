package eval

import (
	"context"
	"strings"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// invokeAndAccumulate runs a non-streaming LLM call (the judge doesn't need
// tokens, just the final response) by streaming into an accumulator.
func invokeAndAccumulate(ctx context.Context, s providers.Streamer, req providers.Request) (string, error) {
	var b strings.Builder
	sink := &accSink{buf: &b}
	if err := s.Stream(ctx, req, sink); err != nil {
		return "", err
	}
	return b.String(), nil
}

type accSink struct {
	buf *strings.Builder
}

func (a *accSink) Send(_ context.Context, c providers.Chunk) error {
	if c.Text != "" {
		a.buf.WriteString(c.Text)
	}
	return nil
}