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

// groqStreamer implements Streamer for the Groq Chat Completions API.
//
// Groq's API is OpenAI-compatible: same request/response shape and same
// SSE wire format (`data: {json}\n\n` chunks ending with `data: [DONE]`).
// The only differences from OpenAI are the base URL and that Groq is
// typically used with open-source models (Llama 3.1, Mixtral, etc.).
//
// Auth:  Authorization: Bearer <apiKey>
// Stream: POST https://api.groq.com/openai/v1/chat/completions with stream=true
type groqStreamer struct {
	key      string
	client   *http.Client
	endpoint string
}

func newGroq(key string, timeoutSec int) (Streamer, error) {
	if key == "" {
		return nil, fmt.Errorf("groq: empty key")
	}
	return &groqStreamer{
		key:      key,
		endpoint: "https://api.groq.com/openai/v1/chat/completions",
		client: &http.Client{
			Timeout: timeoutDuration(timeoutSec),
		},
	}, nil
}

func (g *groqStreamer) Name() string { return "groq" }

type groqRequest struct {
	Model       string  `json:"model"`
	Messages    []gqMsg `json:"messages"`
	Stream      bool    `json:"stream"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
}

type gqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqChunk struct {
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

// Groq default model when caller passes a bare prefix.
const groqDefault = "llama-3.1-70b-versatile"

func (g *groqStreamer) Stream(ctx context.Context, req Request, sink ChunkSink) error {
	prefix, modelID := ParseModel(req.Model)
	if prefix != "groq" {
		modelID = groqDefault
	}
	if modelID == "" {
		modelID = groqDefault
	}

	msgs := []gqMsg{}
	if req.SystemMsg != "" {
		msgs = append(msgs, gqMsg{Role: "system", Content: req.SystemMsg})
	}
	msgs = append(msgs, gqMsg{Role: "user", Content: req.Prompt})

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	body, _ := json.Marshal(groqRequest{
		Model:       modelID,
		Messages:    msgs,
		Stream:      true,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", g.endpoint, bytes.NewReader(body))
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "groq:"+modelID))
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return sink.Send(ctx, FinishChunk(req, "groq:"+modelID))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		_ = sink.Send(ctx, Chunk{
			V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
			Model: "groq:" + modelID,
			Text:  fmt.Sprintf("\n[groq error %d: %s]\n", resp.StatusCode, strings.TrimSpace(string(b))),
			Conf:  0,
		})
		return sink.Send(ctx, FinishChunk(req, "groq:"+modelID))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			break
		}

		var gc groqChunk
		if err := json.Unmarshal([]byte(payload), &gc); err != nil {
			continue
		}
		if gc.Error != nil {
			_ = sink.Send(ctx, Chunk{
				V: "chunk.v1", StreamID: req.StreamID, NodeID: req.NodeID,
				Model: "groq:" + modelID,
				Text:  fmt.Sprintf("\n[groq error: %s]\n", gc.Error.Message),
			})
			break
		}

		for _, ch := range gc.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			if err := sink.Send(ctx, Chunk{
				V:        "chunk.v1",
				StreamID: req.StreamID,
				NodeID:   req.NodeID,
				Model:    "groq:" + modelID,
				Text:     ch.Delta.Content,
				Conf:     1.0,
			}); err != nil {
				return err
			}
		}
	}

	return sink.Send(ctx, FinishChunk(req, "groq:"+modelID))
}
