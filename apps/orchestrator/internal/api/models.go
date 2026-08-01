package api

import (
	"encoding/json"
	"net/http"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// HandleModels reports the registered providers and a suggested default model
// for each. Used by the web client to populate the model dropdown dynamically,
// so users only see options that will actually respond (instead of every
// supported model in the codebase).
//
// GET /v1/models → 200 application/json
//
// Response shape:
//
//	{
//	  "providers": [
//	    {"name": "mock",   "default_model": "mock:echo-v1", "default_label": "Mock (no API key needed)"},
//	    {"name": "groq",   "default_model": "groq:llama-3.1-70b-versatile", "default_label": "Groq · Llama 3.1 70B (fast)"},
//	    {"name": "hf",     "default_model": "hf:meta-llama/Meta-Llama-3-8B-Instruct", "default_label": "HuggingFace · Llama 3 8B"}
//	  ]
//	}
func (s *Server) HandleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("content-type", "application/json")

	registered := s.Registry.List()
	out := make([]map[string]string, 0, len(registered))
	for _, name := range registered {
		out = append(out, map[string]string{
			"name":           name,
			"default_model":  name + ":" + providers.DefaultModel(name),
			"default_label":  labelFor(name, providers.DefaultModel(name)),
		})
	}

	resp := map[string]any{"providers": out}
	body, _ := json.Marshal(resp)
	_, _ = w.Write(body)
}

// labelFor produces a human-friendly label for the dropdown. Stubs and
// real adapters use the same labels as the frontend's MODEL_OPTIONS so
// the two stay in sync without coupling.
func labelFor(name, model string) string {
	switch name {
	case "mock":
		return "🧪 Mock (no API key needed)"
	case "openai":
		if model == "gpt-4o" {
			return "OpenAI · GPT-4o (vision)"
		}
		return "OpenAI · GPT-4o mini (cheap)"
	case "anthropic":
		if model == "claude-3-haiku-20240307" {
			return "Anthropic · Claude 3 Haiku (cheap)"
		}
		return "Anthropic · Claude 3.5 Sonnet"
	case "groq":
		return "Groq · " + shortGroqLabel(model)
	case "hf":
		return "HuggingFace · " + shortHFLabel(model)
	case "mistral":
		return "Mistral · " + model
	case "ollama":
		return "Ollama · " + model
	}
	return name + " · " + model
}

func shortGroqLabel(model string) string {
	switch model {
	case "llama-3.1-70b-versatile":
		return "Llama 3.1 70B (fast)"
	case "llama-3.1-8b-instant":
		return "Llama 3.1 8B (cheapest)"
	case "mixtral-8x7b-32768":
		return "Mixtral 8x7B (long context)"
	}
	return model
}

func shortHFLabel(model string) string {
	switch model {
	case "meta-llama/Meta-Llama-3-8B-Instruct":
		return "Llama 3 8B Instruct"
	}
	return model
}
