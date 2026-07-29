# Makefile — run from repo root
# Windows-compatible: uses cmd.exe for shell commands

SHELL := cmd.exe
.ONESHELL:

.PHONY: all dev dev-backend dev-agent dev-frontend \
        build build-backend build-agent build-frontend \
        clean tidy lint test db-reset help

# ─────────────────────────────────────────────
# Development
# ─────────────────────────────────────────────

dev: dev-backend dev-frontend

dev-backend:
	cd control-plane && air

dev-agent:
	cd agent && go run ./cmd/agent/... run

dev-frontend:
	cd dashboard && pnpm dev

# ─────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────

build: build-backend build-agent build-frontend

build-backend:
	cd control-plane && go build -o ../bin/control-plane.exe ./cmd/server/...

build-agent:
	cd agent && go build -o ../bin/agent.exe ./cmd/agent/...

build-frontend:
	cd dashboard && pnpm build

# ─────────────────────────────────────────────
# Code quality
# ─────────────────────────────────────────────

tidy:
	cd control-plane && go mod tidy
	cd agent && go mod tidy

lint:
	cd control-plane && go vet ./...
	cd agent && go vet ./...
	cd dashboard && pnpm lint

test:
	cd control-plane && go test ./... -v
	cd agent && go test ./... -v

# ─────────────────────────────────────────────
# Database
# ─────────────────────────────────────────────

db-reset:
	-del /q "control-plane\yourplatform.db" 2>nul
	@echo Database deleted. Will be recreated on next start.

# ─────────────────────────────────────────────
# Utility
# ─────────────────────────────────────────────

clean:
	@if exist bin rmdir /s /q bin
	@if exist control-plane\tmp rmdir /s /q control-plane\tmp
	@if exist agent\tmp rmdir /s /q agent\tmp
	@if exist dashboard\.next rmdir /s /q dashboard\.next

help:
	@echo.
	@echo Available targets:
	@echo   dev              Start all services (backend + frontend)
	@echo   dev-backend      Start control plane with hot reload
	@echo   dev-agent        Build and run agent locally
	@echo   dev-frontend     Start Next.js dev server
	@echo   build            Build all binaries + frontend
	@echo   build-backend    Build control plane binary
	@echo   build-agent      Build agent binary
	@echo   build-frontend   Build Next.js for production
	@echo   tidy             Tidy Go modules
	@echo   lint             Run linters (go vet + eslint)
	@echo   test             Run all tests
	@echo   db-reset         Delete SQLite database
	@echo   clean            Remove build artifacts
	@echo   help             Show this help
	@echo.
