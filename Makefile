# idea-collect — developer tasks
# Dev mirrors production: everything runs under rootless Podman.
DB_URL  ?= postgres://idea:idea@localhost:5432/idea?sslmode=disable

# Dev Postgres (rootless Podman). Same image as the production quadlet.
PG_IMAGE     ?= docker.io/library/postgres:18
PG_CONTAINER ?= idea-collect-db
PG_VOLUME    ?= idea-collect-pgdata

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---- Database ----
.PHONY: db-up db-down db-logs
db-up: ## Start Postgres 18 (rootless Podman; idempotent)
	podman run -d --replace --name $(PG_CONTAINER) \
		-e POSTGRES_USER=idea -e POSTGRES_PASSWORD=idea -e POSTGRES_DB=idea \
		-p 5432:5432 \
		-v $(PG_VOLUME):/var/lib/postgresql \
		--health-cmd 'pg_isready -U idea -d idea' \
		--health-interval 5s --health-retries 10 \
		$(PG_IMAGE)
db-down: ## Stop and remove dev Postgres (keeps the data volume)
	-podman rm -f -t 5 $(PG_CONTAINER)
db-logs: ## Tail Postgres logs
	podman logs -f $(PG_CONTAINER)

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
# Rootless Podman mirrors production (the quadlets load these same images).
.PHONY: images
images: ## Build the backend + custom Caddy images
	podman build -t localhost/idea-collect-backend:latest -f backend/Containerfile backend
	podman build -t localhost/idea-collect-caddy:latest deploy/caddy
