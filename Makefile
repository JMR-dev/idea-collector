# idea-collect — developer tasks
# Override COMPOSE with `podman compose` or `podman-compose` if you prefer Podman locally.
COMPOSE ?= docker compose
DB_URL  ?= postgres://idea:idea@localhost:5432/idea?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---- Database ----
.PHONY: db-up db-down db-logs
db-up: ## Start Postgres 18 (dev)
	$(COMPOSE) -f compose.dev.yml up -d db
db-down: ## Stop dev Postgres (keeps volume)
	$(COMPOSE) -f compose.dev.yml down
db-logs: ## Tail Postgres logs
	$(COMPOSE) -f compose.dev.yml logs -f db

## ---- Backend ----
.PHONY: backend-run backend-build backend-test admin
backend-run: ## Run the API server (applies migrations)
	cd backend && DATABASE_URL="$(DB_URL)" go run ./cmd/server
backend-build: ## Build server + admin binaries into backend/bin
	cd backend && go build -o bin/server ./cmd/server && go build -o bin/admin ./cmd/admin
backend-test: ## Run Go tests
	cd backend && go test ./...
admin: ## Run the admin CLI: make admin ARGS="user create --project demo --name 'Jane'"
	cd backend && DATABASE_URL="$(DB_URL)" go run ./cmd/admin $(ARGS)

## ---- Frontend ----
.PHONY: frontend-dev frontend-build frontend-install
frontend-install: ## Install frontend deps
	cd frontend && npm install
frontend-dev: ## Run Vite dev server (proxies /api -> :8080)
	cd frontend && npm run dev
frontend-build: ## Production build into frontend/dist
	cd frontend && npm run build

## ---- End-to-end ----
.PHONY: e2e
e2e: db-up ## Start DB then run backend (use frontend-dev in another shell)
	@echo "Postgres up. In another terminal: make frontend-dev"
	$(MAKE) backend-run

## ---- Containers ----
# Use podman locally to mirror production; override with ENGINE=docker.
ENGINE ?= podman
.PHONY: images
images: ## Build the backend + custom Caddy images
	$(ENGINE) build -t localhost/idea-collect-backend:latest -f backend/Containerfile backend
	$(ENGINE) build -t localhost/idea-collect-caddy:latest deploy/caddy
