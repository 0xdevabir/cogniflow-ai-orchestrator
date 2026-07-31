# CogniFlow — single entrypoint for the whole monorepo.
# `make up` boots infra. Run each app in its own shell with the per-app targets below.

SHELL := /bin/bash

.PHONY: help up down logs ps clean test test-go test-py test-web lint build

help:
	@echo "CogniFlow — orchestrator for multi-model AI"
	@echo ""
	@echo "Infra:"
	@echo "  make up        — start Postgres+pgvector, Redis, Qdrant"
	@echo "  make down      — stop all infra"
	@echo "  make logs      — tail infra logs"
	@echo "  make ps        — list running infra"
	@echo "  make clean     — destroy infra + volumes (DESTRUCTIVE)"
	@echo ""
	@echo "Apps (run in separate shells):"
	@echo "  make dev-web           — Next.js web app on :3000"
	@echo "  make dev-orchestrator  — Go orchestrator on :8080"
	@echo "  make dev-ml-gateway    — FastAPI ml-gateway on :8081"
	@echo ""
	@echo "Quality:"
	@echo "  make test       — go test + pytest + vitest"
	@echo "  make lint       — gofmt+golangci-lint+ruff+eslint"
	@echo "  make build      — produce release binaries"

up:
	docker compose up -d
	@echo ""
	@echo "✓ CogniFlow infra is up."
	@echo "  Postgres+pgvector → localhost:5432  (cogniflow / cogniflow / cogniflow)"
	@echo "  Redis             → localhost:6379"
	@echo "  Qdrant            → localhost:6333 (HTTP) / :6334 (gRPC)"

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

ps:
	docker compose ps

clean:
	@echo "This will DELETE all data volumes. Confirm by pressing Ctrl-C within 5s."
	@sleep 5
	docker compose down -v

dev-web:
	cd apps/web && npm install && npm run dev

dev-orchestrator:
	cd apps/orchestrator && go mod download && go run ./cmd/server

dev-ml-gateway:
	cd apps/ml-gateway && uv pip install -e . && uvicorn app.main:app --reload --port 8081

test: test-go test-py test-web

test-go:
	cd apps/orchestrator && go test ./...

test-py:
	cd apps/ml-gateway && pytest -q

test-web:
	cd apps/web && npm run test --silent

lint:
	cd apps/orchestrator && gofmt -l . && go vet ./...
	cd apps/ml-gateway && ruff check .
	cd apps/web && npm run lint

build:
	cd apps/orchestrator && CGO_ENABLED=0 go build -o bin/orchestrator ./cmd/server
	cd apps/ml-gateway && python -m build
	cd apps/web && npm run build