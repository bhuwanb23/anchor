# Makefile — run from repo root
# Windows-compatible with GnuWin32 Make

.PHONY: all dev dev-backend dev-agent dev-frontend \
        build build-backend build-agent build-frontend \
        build-agent-linux-amd64 build-agent-linux-arm64 build-agent-all \
        checksum release \
        clean tidy lint test db-reset

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
# Cross-compilation (Linux releases)
# ─────────────────────────────────────────────

AGENT_VERSION ?= 0.1.0

build-agent-linux-amd64:
	@if not exist release mkdir release
	cd agent && set GOOS=linux&& set GOARCH=amd64&& set CGO_ENABLED=0&& \
		go build -o ../release/yourplatform-agent-linux-amd64 ./cmd/agent/...

build-agent-linux-arm64:
	@if not exist release mkdir release
	cd agent && set GOOS=linux&& set GOARCH=arm64&& set CGO_ENABLED=0&& \
		go build -o ../release/yourplatform-agent-linux-arm64 ./cmd/agent/...

build-agent-all: build-agent-linux-amd64 build-agent-linux-arm64
	@echo "All agent binaries built in release/"

checksum:
	@cd release && for f in yourplatform-agent-linux-*; do \
		if [ ! "$$f" = "*.sha256" ]; then \
			sha256sum "$$f" > "$$f.sha256"; \
		fi; \
	done
	@echo "Checksums generated"

release: build-agent-all checksum
	@echo "Release v$(AGENT_VERSION) ready in release/"

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
	@if exist release rmdir /s /q release
	@if exist control-plane\tmp rmdir /s /q control-plane\tmp
	@if exist agent\tmp rmdir /s /q agent\tmp
	@if exist dashboard\.next rmdir /s /q dashboard\.next
