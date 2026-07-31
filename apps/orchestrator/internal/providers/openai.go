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

// openaiStreamer implements Streamer for the OpenAI Chat Completions API.
//
// Auth:  Authorization: Bearer <apiKey>
// Stream: POST https://api.openai.com/v1/chat/completions with stream=true
//         Response is `data: {json}\n\n` chunks until `data: [DONE]`.
//
// We do NOT pull in the openai-go SDK because (a) we want zero extra deps
// in Phase 1, (b) the SDK churns monthly, (c) we only need streaming.
type openaiStreamer struct {
	key      string
	client   *http.Client
	endpoint string
}

func newOpenAI(key string, timeoutSec int) (Streamer, error) {
	if key == "" {
		return nil, fmt.Errorf("openai: empty key")
	}
	return &openaiStreamer{
		key:      key,
		endpoint: "https://api.openai.com/v1/chat/completions",
		client: &http.Client{
			Timeout: timeoutDuration(timeoutSec),
		},
	}, nil
}

func (o *openaiStreamer) Name() string { return "openai" }

type openaiRequest struct {
	Model       string    `json:"model"`
	Messages    []oaMsg   `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
}

type oaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// OpenAI default model when caller passes a bare prefix.
const openaiDefault = "gpt-4o-mini"

func (o *openaiStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	prefix, modelID := ParseModel(req.Model)
	if prefix != "openai" {
		modelID = openaiDefault
	}
	if modelID == "" {
		modelID = openaiDefault
	}

	msgs := []oaMsg{}
	if req.SystemMsg != "" {
		msgs = append(msgs, oaMsg{Role: "system", Content: req.SystemMsg})
	}
	msgs = append(msgs, oaMsg{Role: "user", Content: req.Prompt})

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	body, _ := json.Marshal(openaiRequest{
		Model:       modelID,
		Messages:    msgs,
		Stream:      true,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.endpoint, bytes.NewReader(body))
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "openai:"+modelID))
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "openai:"+modelID))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain the body to get a useful error.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		// Best-effort: surface as text so it appears in the UI stream.
		_ = sink.Send(ctx, Chunk{
			V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
			Model: "openai:" + modelID,
			Text:  fmt.Sprintf("\n[openai error %d: %s]\n", resp.StatusCode, strings.TrimSpace(string(b))),
			Conf:  0,
		})
		return sink.Send(ctx, FinishChunk(req, "openai:"+modelID))
	}

	scanner := bufio.NewScanner(resp.Body)
	// Allow large SSE payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE: "data: <payload>"; we want everything after "data: ".
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			break
		}

		var oc openaiChunk
		if err := json.Unmarshal([]byte(payload), &oc); err != nil {
			continue // skip malformed
		}
		if oc.Error != nil {
			_ = sink.Send(ctx, Chunk{
				V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
				Model: "openai:" + modelID,
				Text:  fmt.Sprintf("\n[openai error: %s]\n", oc.Error.Message),
			})
			break
		}

		for _, ch := range oc.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			if err := sink.Send(ctx, Chunk{
				V:        "chunk.v1",
				StreamID: req.StreamID,
				NodeID:   req.NodeID,
				Model:    "openai:" + modelID,
				Text:     ch.Delta.Content,
				Conf:     1.0,
			}); err != nil {
				return err
			}
		}
	}

	return sink.Send(ctx, FinishChunk(req, "openai:"+modelID))
}
