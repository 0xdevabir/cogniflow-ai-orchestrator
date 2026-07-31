package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropicStreamer implements Streamer for the Anthropic Messages API.
//
// Auth:  x-api-key: <apiKey>  +  anthropic-version: 2023-06-01
// Stream: POST https://api.anthropic.com/v1/messages with stream=true
//         Response is a sequence of SSE events:
//             message_start, content_block_start, ping,
//             content_block_delta (TextDelta carries the chunk),
//             content_block_stop, message_delta, message_stop
//
// We extract only content_block_delta → delta.text.
type anthropicStreamer struct {
	key      string
	client   *http.Client
	endpoint string
}

func newAnthropic(key string, timeoutSec int) (Streamer, error) {
	if key == "" {
		return nil, fmt.Errorf("anthropic: empty key")
	}
	return &anthropicStreamer{
		key:      key,
		endpoint: "https://api.anthropic.com/v1/messages",
		client: &http.Client{
			Timeout: timeoutDuration(timeoutSec),
		},
	}, nil
}

func (a *anthropicStreamer) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model       string  `json:"model"`
	Messages    []antMsg `json:"messages"`
	MaxTokens   int     `json:"max_tokens"`
	Stream      bool    `json:"stream"`
	System      string  `json:"system,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
}

type antMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Anthropic default model when caller passes a bare prefix.
const anthropicDefault = "claude-3-5-sonnet-latest"

func (a *anthropicStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	prefix, modelID := ParseModel(req.Model)
	if prefix != "anthropic" {
		modelID = anthropicDefault
	}
	if modelID == "" {
		modelID = anthropicDefault
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	body, _ := json.Marshal(anthropicRequest{
		Model:     modelID,
		Messages:  []antMsg{{Role: "user", Content: req.Prompt}},
		MaxTokens: maxTokens,
		Stream:    true,
		System:    req.SystemMsg,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(body))
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "anthropic:"+modelID))
	}
	httpReq.Header.Set("x-api-key", a.key)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "anthropic:"+modelID))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		_ = sink.Send(ctx, Chunk{
			V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
			Model: "anthropic:" + modelID,
			Text:  fmt.Sprintf("\n[anthropic error %d: %s]\n", resp.StatusCode, strings.TrimSpace(string(b))),
		})
		return sink.Send(ctx, FinishChunk(req, "anthropic:"+modelID))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Anthropic SSE: `event: <type>` lines followed by `data: {json}` lines.
	// We only care about content_block_delta events.
	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		case strings.HasPrefix(line, "data: "):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			if currentEvent != "content_block_delta" {
				continue
			}
			var ev struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			if ev.Error != nil {
				_ = sink.Send(ctx, Chunk{
					V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
					Model: "anthropic:" + modelID,
					Text:  fmt.Sprintf("\n[anthropic error: %s]\n", ev.Error.Message),
				})
				break
			}
			if ev.Delta.Text == "" {
				continue
			}
			if err := sink.Send(ctx, Chunk{
				V:        "chunk.v1",
				StreamID: req.StreamID,
				NodeID:   req.NodeID,
				Model:    "anthropic:" + modelID,
				Text:     ev.Delta.Text,
				Conf:     1.0,
			}); err != nil {
				return err
			}
		}
	}

	return sink.Send(ctx, FinishChunk(req, "anthropic:"+modelID))
}
