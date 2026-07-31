package providers

import "testing"

func TestRegistry_MockAlwaysPresent(t *testing.T) {
	r := NewRegistry(nil)
	if _, err := r.Get("mock"); err != nil {
		t.Fatalf("mock should always be registered: %v", err)
	}
	if _, err := r.Get("anything"); err != nil {
		t.Fatalf("get on unknown prefix should fall back to mock: %v", err)
	}
	if _, err := r.Get(""); err != nil {
		t.Fatalf("empty model should fall back to mock: %v", err)
	}
}

func TestRegistry_OpenAIKeyWires(t *testing.T) {
	r := NewRegistry(&RegistryConfig{OpenAIKey: "sk-test"})
	names := r.List()
	if !containsString(names, "mock") {
		t.Errorf("registry missing mock: %v", names)
	}
	if !containsString(names, "openai") {
		t.Errorf("registry missing openai when key set: %v", names)
	}
}

func TestRegistry_NoKeysOmitsProviders(t *testing.T) {
	r := NewRegistry(&RegistryConfig{})
	for _, n := range []string{"openai", "anthropic", "mistral", "hf", "ollama"} {
		if containsString(r.List(), n) {
			t.Errorf("registry leaked %q without keys", n)
		}
	}
}

func TestRegistry_GetReturnsMockOnUnknownPrefix(t *testing.T) {
	r := NewRegistry(nil)
	s, err := r.Get("totally-fake-prefix:model-x")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "mock" {
		t.Errorf("expected fallback to mock, got %q", s.Name())
	}
}

func TestRegistry_GetParsesPrefix(t *testing.T) {
	r := NewRegistry(&RegistryConfig{OpenAIKey: "sk-test"})
	s, err := r.Get("openai:gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "openai" {
		t.Errorf("expected openai, got %q", s.Name())
	}
}

func TestDefaultModel(t *testing.T) {
	if DefaultModel("openai") != "gpt-4o-mini" {
		t.Error("default openai model mismatch")
	}
	if DefaultModel("anthropic") != "claude-3-5-sonnet-latest" {
		t.Error("default anthropic model mismatch")
	}
	if DefaultModel("nonsense") == "" {
		t.Error("fallback must return non-empty")
	}
}

func containsString(items []string, target string) bool {
	for _, s := range items {
		if s == target {
			return true
		}
	}
	return false
}
