// Command orchestrator is the CogniFlow orchestrator entrypoint.
//
// Phase 0: prints a ready banner and binds /healthz.
// Phase 1: wires the Streamer registry + /v1/chat SSE handler.
// Phase 2: wires the Decomposer + /v1/plan JSON endpoint.
// Phase 3: wires the Router (weighted heuristic + bandit logger) and
// attaches per-node routing decisions to /v1/plan.
// Phase 8: wires OpenTelemetry traces (OTLP/stdout/none via OTEL_EXPORTER).
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
	"github.com/cogniflow/orchestrator/internal/entity"
	"github.com/cogniflow/orchestrator/internal/eval"
	"github.com/cogniflow/orchestrator/internal/meter"
	"github.com/cogniflow/orchestrator/internal/obs"
	"github.com/cogniflow/orchestrator/internal/providers"
	"github.com/cogniflow/orchestrator/internal/rag"
	"github.com/cogniflow/orchestrator/internal/router"
)

func main() {
	addr := os.Getenv("ORCH_PORT")
	if addr == "" {
		addr = ":8080"
	}

	// Phase 8: OpenTelemetry. Default = stdout (great for dev); set
	// OTEL_EXPORTER=otlp to ship to a real collector, OTEL_EXPORTER=none
	// to disable. The shutdown hook flushes spans on SIGTERM.
	otelShutdown, err := obs.Init(context.Background(), "cogniflow-orchestrator", os.Getenv("OTEL_EXPORTER"))
	if err != nil {
		log.Printf("obs: init failed: %v (telemetry disabled)", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := otelShutdown(ctx); err != nil {
				log.Printf("obs: shutdown: %v", err)
			}
		}()
	}

	// Build the model provider registry from env.
	reg := providers.NewRegistry(&providers.RegistryConfig{
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		AnthropicKey: os.Getenv("ANTHROPIC_API_KEY"),
		MistralKey:   os.Getenv("MISTRAL_API_KEY"),
		HFKey:        os.Getenv("HF_API_KEY"),
		OllamaURL:    os.Getenv("OLLAMA_BASE_URL"),
		GroqKey:      os.Getenv("GROQ_API_KEY"),
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

	// Build the router. The bandit log path is configurable via
	// BANDIT_LOG (default: ./data/bandit.jsonl). If the path can't be
	// opened, we log a warning and continue without logging.
	var rtr router.Router
	if bench, err := router.LoadBenchmarks(); err != nil {
		log.Printf("router: failed to load benchmarks: %v (router disabled)", err)
	} else if costs, err := router.LoadCostTable(); err != nil {
		log.Printf("router: failed to load cost table: %v (router disabled)", err)
	} else {
		var logger router.FeedbackLogger
		logPath := os.Getenv("BANDIT_LOG")
		if logPath == "" {
			logPath = "./data/bandit.jsonl"
		}
		if l, err := router.NewJSONLFeedbackLogger(logPath); err != nil {
			log.Printf("router: bandit log disabled (%v)", err)
		} else {
			logger = l
			log.Printf("router: bandit log → %s", logPath)
		}
		r, err := router.NewWeighted(router.WeightedConfig{
			Bench:        bench,
			Costs:        costs,
			EstPromptTok: 500,
			EstOutTok:    1000,
			Logger:       logger,
		})
		if err != nil {
			log.Printf("router: failed to build router: %v (router disabled)", err)
		} else {
			rtr = r
			// Phase 8: optionally apply the bandit-learn recommendation.
			// Set ROUTER_RECOMMENDATION=./data/recommendation.json to
			// enable. The file is rewritten by `cmd/bandit-learn` from
			// the JSONL log.
			if recPath := os.Getenv("ROUTER_RECOMMENDATION"); recPath != "" {
				routerWeights, err := router.LoadRecommendation(recPath, nil)
				if err != nil {
					log.Printf("router: recommendation load failed: %v", err)
				} else if len(routerWeights) > 0 {
					r.SetWeights(routerWeights)
					log.Printf("router: applied recommendation from %s (%d classes)", recPath, len(routerWeights))
				}
			}
		}
	}

	srv := &api.Server{
		Registry:    reg,
		Decomposer:  decomp,
		Router:      rtr,
		EntityStore: entity.NoopStore{},
	}

	// Phase 8: per-usage-event JSONL log. Defaults to ./data/meter.jsonl
	// when METER_LOG is unset so the dashboard always has data to show.
	meterLogPath := os.Getenv("METER_LOG")
	if meterLogPath == "" {
		meterLogPath = "./data/meter.jsonl"
	}
	if m, err := meter.NewJSONLMeterer(meterLogPath); err == nil {
		srv.Meter = m
		log.Printf("📊 meter log → %s", meterLogPath)
		defer m.Close()
	} else {
		log.Printf("meter: failed to open log %s: %v (using noop)", meterLogPath, err)
		srv.Meter = meter.NoopMeterer{}
	}

	// Phase 7+: per-run eval-result JSONL log so the /dashboard endpoint can
	// show historical runs. Defaults to ./data/eval.jsonl when EVAL_LOG unset.
	evalLogPath := os.Getenv("EVAL_LOG")
	if evalLogPath == "" {
		evalLogPath = "./data/eval.jsonl"
	}
	if e, err := eval.NewJSONLEvalLogger(evalLogPath); err == nil {
		srv.EvalLog = e
		log.Printf("📊 eval log → %s", evalLogPath)
		defer e.Close()
	} else {
		log.Printf("eval: failed to open log %s: %v (eval persistence disabled)", evalLogPath, err)
	}

	// Phase 7: load the cost table so the budget cascade + eval can price runs.
	if costs, err := router.LoadCostTable(); err == nil {
		srv.CostTable = costs
	} else {
		log.Printf("budget: failed to load cost table: %v (cascade disabled)", err)
	}

	// Build the RAG service. The default store is the in-memory store so
	// the demo works without Postgres; ORCH_DATABASE_URL switches to pgvector
	// in Phase 8. The OpenAI embedder requires OPENAI_API_KEY; without it we
	// fall back to a lexical-only retriever (good enough for the playground).
	var ragSvc *rag.Service
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		emb := rag.NewOpenAIEmbedder(openaiKey, "text-embedding-3-small")
		ragSvc = rag.NewService(rag.NewMemStore(), emb)
	} else {
		log.Printf("rag: OPENAI_API_KEY not set → using lexical-only retriever")
		ragSvc = rag.NewService(rag.NewMemStore(), nil)
	}
	srv.RAG = ragSvc

	// Build the meter. Order of preference:
	// Build the meter. The JSONL default was wired above so the dashboard
	// always has data; here we only override when Stripe is explicitly
	// configured (STRIPE_API_KEY + STRIPE_METER_ID).
	if sm := meter.StripeMetererFromEnv(); sm != nil {
		if _, ok := sm.(meter.NoopMeterer); !ok {
			log.Printf("meter: Stripe enabled (overriding JSONL default)")
			srv.Meter = sm
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.HandleHealthz)
	mux.HandleFunc("/v1/chat", srv.HandleChat)
	mux.HandleFunc("/v1/plan", srv.HandlePlan)
	mux.HandleFunc("/v1/run", srv.HandleRun)
	mux.HandleFunc("/v1/models", srv.HandleModels)
	mux.HandleFunc("/v1/dashboard/runs", srv.HandleDashboardRuns)
	mux.HandleFunc("/v1/dashboard/bandit", srv.HandleDashboardBandit)
	mux.HandleFunc("/v1/dashboard/models", srv.HandleDashboardModels)
	mux.HandleFunc("/v1/docs", srv.HandleDocsRoute)
	mux.HandleFunc("/v1/docs/", srv.HandleDocsRoute)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           obs.HTTPMiddleware(mux),
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

