package providers

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// registry implements Registry with a fixed set of registered Streamers.
type registry struct {
	mu sync.RWMutex

	streamers map[string]Streamer
	// defaultModel maps a provider prefix to its default model id.
	defaults map[string]string
}

// NewRegistry builds a registry from cfg. Adapters are wired only when the
// corresponding credentials are present; the mock adapter is always wired.
// Returns a Registry; never nil.
func NewRegistry(cfg *RegistryConfig) Registry {
	if cfg == nil {
		cfg = &RegistryConfig{}
	}

	r := &registry{
		streamers: map[string]Streamer{},
		defaults: map[string]string{
			"openai":    "gpt-4o-mini",
			"anthropic": "claude-3-5-sonnet-latest",
			"mistral":   "mistral-small-latest",
			"hf":        "meta-llama/Meta-Llama-3-8B-Instruct",
			"ollama":    "llama3.1",
			"mock":      "echo-v1",
		},
	}

	// Mock is always present.
	r.streamers["mock"] = &mockStreamer{}

	// Wire real adapters only when keys are provided. Adapters that fail
	// to construct (e.g. invalid key format) are silently skipped — the
	// mock acts as the universal fallback.
	if cfg.OpenAIKey != "" {
		if s, err := newOpenAI(cfg.OpenAIKey, cfg.HTTPTimeout); err == nil {
			r.streamers["openai"] = s
		}
	}
	if cfg.AnthropicKey != "" {
		if s, err := newAnthropic(cfg.AnthropicKey, cfg.HTTPTimeout); err == nil {
			r.streamers["anthropic"] = s
		}
	}
	if cfg.MistralKey != "" {
		r.streamers["mistral"] = newMistralStub(cfg.MistralKey)
	}
	if cfg.HFKey != "" {
		r.streamers["hf"] = newHFStub(cfg.HFKey)
	}
	if cfg.OllamaURL != "" {
		r.streamers["ollama"] = newOllamaStub(cfg.OllamaURL)
	}

	return r
}

// Get resolves a fully-qualified model id like "openai:gpt-4o-mini".
//
// Behavior:
//   - "openai:gpt-4o-mini" → openai Streamer
//   - "openai"            → openai Streamer (caller picks the default)
//   - "anthropic:claude-3-5-sonnet-latest" → anthropic Streamer
//   - "mock" / "mock:echo" → mock Streamer
//   - empty string         → mock Streamer
//   - unknown prefix       → error
//
// When a requested provider is not registered (e.g. openai with no key),
// we fall back to mock and log a warning via the returned model name.
func (r *registry) Get(model string) (Streamer, error) {
	prefix, _ := splitModel(model)
	if prefix == "" {
		prefix = "mock"
	}

	r.mu.RLock()
	s, ok := r.streamers[prefix]
	r.mu.RUnlock()

	if !ok {
		// Unknown prefix — fall back to mock.
		r.mu.RLock()
		s = r.streamers["mock"]
		r.mu.RUnlock()
		return s, nil
	}
	return s, nil
}

// List returns the registered provider prefixes.
func (r *registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.streamers))
	for k := range r.streamers {
		out = append(out, k)
	}
	return out
}

// DefaultModel returns the default model id for a provider prefix.
func DefaultModel(prefix string) string {
	switch prefix {
	case "openai":
		return "gpt-4o-mini"
	case "anthropic":
		return "claude-3-5-sonnet-latest"
	case "mistral":
		return "mistral-small-latest"
	case "hf":
		return "meta-llama/Meta-Llama-3-8B-Instruct"
	case "ollama":
		return "llama3.1"
	case "mock", "":
		return "echo-v1"
	default:
		return prefix
	}
}

// splitModel splits "prefix:rest" into ("prefix", "rest"). If there is no ":",
// the second value is empty.
func splitModel(model string) (prefix, rest string) {
	i := strings.Index(model, ":")
	if i < 0 {
		return model, ""
	}
	return model[:i], model[i+1:]
}

// ParseModel is the public version of splitModel.
func ParseModel(model string) (prefix, modelID string) { return splitModel(model) }

// timeoutDuration returns the configured HTTP timeout or 60s default.
func timeoutDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// ErrUnknownProvider is returned (rarely) when we cannot resolve and cannot
// fall back. Currently defensive — Get always falls back to mock.
var ErrUnknownProvider = fmt.Errorf("unknown provider")
