# Makefile — run from repo root
# On Windows: ensure GnuWin32\bin is in PATH, or run from Git Bash

SHELL := /bin/bash

.PHONY: all dev dev-backend dev-agent dev-frontend \
        build build-backend build-agent build-frontend \
        clean tidy lint test db-reset help

# ─────────────────────────────────────────────
# Development
# ─────────────────────────────────────────────

dev: ## Start all services for development
	@make -j3 dev-backend dev-frontend

dev-backend: ## Start control plane with hot reload
	cd control-plane && air

dev-agent: ## Build agent and run locally (for testing)
	cd agent && go run ./cmd/agent/... run

dev-frontend: ## Start Next.js dev server
	cd dashboard && pnpm dev

# ─────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────

build: build-backend build-agent build-frontend

build-backend: ## Build control plane binary
	cd control-plane && go build -o ../bin/control-plane.exe ./cmd/server/...

build-agent: ## Build agent binary
	cd agent && go build -o ../bin/agent.exe ./cmd/agent/...

build-frontend: ## Build Next.js for production
	cd dashboard && pnpm build

# ─────────────────────────────────────────────
# Code quality
# ─────────────────────────────────────────────

tidy: ## Tidy Go modules
	cd control-plane && go mod tidy
	cd agent && go mod tidy

lint: ## Run linters
	cd control-plane && go vet ./...
	cd agent && go vet ./...
	cd dashboard && pnpm lint

test: ## Run all tests
	cd control-plane && go test ./... -v
	cd agent && go test ./... -v

# ─────────────────────────────────────────────
# Database
# ─────────────────────────────────────────────

db-reset: ## Delete SQLite database (fresh start)
	rm -f control-plane/yourplatform.db
	@echo "Database deleted. Will be recreated on next start."

# ─────────────────────────────────────────────
# Utility
# ─────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf bin/
	rm -rf control-plane/tmp/
	rm -rf agent/tmp/
	rm -rf dashboard/.next/

help: ## Show this help
	@echo ""
	@echo "Available targets:"
	@echo "  dev              Start all services (backend + frontend)"
	@echo "  dev-backend      Start control plane with hot reload"
	@echo "  dev-agent        Build and run agent locally"
	@echo "  dev-frontend     Start Next.js dev server"
	@echo "  build            Build all binaries + frontend"
	@echo "  build-backend    Build control plane binary"
	@echo "  build-agent      Build agent binary"
	@echo "  build-frontend   Build Next.js for production"
	@echo "  tidy             Tidy Go modules"
	@echo "  lint             Run linters (go vet + eslint)"
	@echo "  test             Run all tests"
	@echo "  db-reset         Delete SQLite database"
	@echo "  clean            Remove build artifacts"
	@echo "  help             Show this help"
	@echo ""
