package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// OpenAIEmbedder calls the OpenAI /v1/embeddings endpoint. The MVP uses
// text-embedding-3-small (1536 dims); other dims are configurable.
type OpenAIEmbedder struct {
	APIKey     string
	ModelName  string
	BaseURL    string
	DimSize    int
	HTTPClient *http.Client
	BatchMax   int
}

// NewOpenAIEmbedder returns an OpenAIEmbedder with sensible defaults.
func NewOpenAIEmbedder(apiKey, model string) *OpenAIEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		APIKey:    apiKey,
		ModelName: model,
		BaseURL:   "https://api.openai.com",
		DimSize:   1536,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		BatchMax:  100,
	}
}

func (e *OpenAIEmbedder) Dim() int { return e.DimSize }
func (e *OpenAIEmbedder) Model() string {
	if e == nil {
		return ""
	}
	return "openai:" + e.ModelName
}

// Embed implements Embedder. Texts are batched at BatchMax per HTTP request
// to stay under OpenAI's per-call input limits.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, errors.New("rag: nil OpenAIEmbedder")
	}
	if e.APIKey == "" {
		return nil, errors.New("rag: OPENAI_API_KEY not set")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	batch := e.BatchMax
	if batch <= 0 {
		batch = 100
	}
	for start := 0; start < len(texts); start += batch {
		end := start + batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

type embeddingReq struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embeddingResp struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingReq{Input: texts, Model: e.ModelName})
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(e.BaseURL, "/v1/embeddings")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("rag: openai embed HTTP %d: %s", res.StatusCode, string(raw))
	}
	var parsed embeddingResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("rag: openai embed parse: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("rag: openai embed: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("rag: openai embed returned %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		out[d.Index] = d.Embedding
	}
	return out, nil
}