// Command orchestrator is the CogniFlow orchestrator entrypoint.
//
// Phase 0: prints a ready banner and binds /healthz.
// Phase 1: wires the Streamer registry + /v1/chat SSE handler.
// Phase 2: wires the Decomposer + /v1/plan JSON endpoint.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cogniflow/orchestrator/internal/api"
	"github.com/cogniflow/orchestrator/internal/decomposer"
	"github.com/cogniflow/orchestrator/internal/providers"
)

func main() {
	addr := os.Getenv("ORCH_PORT")
	if addr == "" {
		addr = ":8080"
	}

	// Build the model provider registry from env.
	reg := providers.NewRegistry(&providers.RegistryConfig{
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		AnthropicKey: os.Getenv("ANTHROPIC_API_KEY"),
		MistralKey:   os.Getenv("MISTRAL_API_KEY"),
		HFKey:        os.Getenv("HF_API_KEY"),
		OllamaURL:    os.Getenv("OLLAMA_BASE_URL"),
		HTTPTimeout:  60,
	})

	log.Printf("🧠 cogniflow — registered providers: %v", reg.List())

	// Build the decomposer. The model is configurable via env.
	decompModel := os.Getenv("DECOMP_MODEL")
	if decompModel == "" {
		decompModel = "openai:gpt-4o-mini"
	}
	decomp := decomposer.New(decomposer.Deps{
		Registry: reg,
		Model:    decompModel,
		Timeout:  45 * time.Second,
		Retries:  3,
		MaxTokens: 4096,
	})

	srv := &api.Server{
		Registry:   reg,
		Decomposer: decomp,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.HandleHealthz)
	mux.HandleFunc("/v1/chat", srv.HandleChat)
	mux.HandleFunc("/v1/plan", srv.HandlePlan)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("🧠 cogniflow-orchestrator listening on %s (decomp model: %s)", addr, decompModel)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
