# Fireman build and development commands. Image and binary variable names are
# part of the public build contract and must not be renamed.

BIN ?= fireman
BACKEND_IMAGE ?= fireman
WEB_IMAGE ?= fireman-web
WEB_API_PROXY_TARGET ?= http://backend:8080

GO ?= go
NPM ?= npm
UV ?= uv
DOCKER ?= docker
COMPOSE ?= $(DOCKER) compose

PROJECT_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
WEB_DIR := $(PROJECT_ROOT)/web
PROVIDER_DIR := $(PROJECT_ROOT)/sidecars/market-provider
COMPOSE_FILE := $(PROJECT_ROOT)/docker/docker-compose.yml

.PHONY: help \
	build test lint backend-check \
	web-install web-lint web-test web-build web-check \
	market-provider-install market-provider-test market-provider-start \
	build-backend-image build-web-image build-market-provider-image build-images \
	dev docker-up docker-down integration-test ci

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-32s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# -----------------------------------------------------------------------------
# Go backend
# -----------------------------------------------------------------------------

# GO_PKGS is the list of importable Fireman Go packages. We deliberately avoid
# the bare `./...` form so that any stray *.go files vendored beneath
# web/node_modules cannot hijack `go test` / `go vet`.
GO_PKGS := ./cmd/... ./internal/... ./migrations/...

# Source roots that gofmt and go vet must inspect. Same exclusion logic as
# above — keep this list in sync with the Go source layout if directories are
# added.
GO_SRC_DIRS := cmd internal migrations

build: ## Build the Go binary at bin/$(BIN).
	@mkdir -p $(PROJECT_ROOT)/bin
	$(GO) build -o $(PROJECT_ROOT)/bin/$(BIN) ./cmd/fireman

test: ## Run Go unit and integration tests.
	$(GO) test $(GO_PKGS)

lint: ## Run Go lint (gofmt + go vet).
	@unformatted="$$(gofmt -l $(GO_SRC_DIRS) 2>/dev/null || true)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: the following files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) vet $(GO_PKGS)

backend-check: build test lint ## build + test + lint.

# -----------------------------------------------------------------------------
# Web (Next.js)
# -----------------------------------------------------------------------------

web-install: ## Install web dependencies via npm ci.
	$(PROJECT_ROOT)/scripts/web-install.sh

web-lint: ## Lint the web project.
	cd $(WEB_DIR) && $(NPM) run lint

web-test: ## Run web tests via Vitest.
	cd $(WEB_DIR) && $(NPM) test -- --run

web-build: ## Build the Next.js production bundle.
	rm -rf $(WEB_DIR)/.next
	cd $(WEB_DIR) && $(NPM) run build

web-check: web-install web-lint web-test web-build ## Web install + lint + test + build.

# -----------------------------------------------------------------------------
# Market provider (Python sidecar)
# -----------------------------------------------------------------------------

market-provider-install: ## Resolve the sidecar venv from uv.lock.
	cd $(PROVIDER_DIR) && $(UV) sync --frozen

market-provider-test: ## Run sidecar pytest suite.
	cd $(PROVIDER_DIR) && $(UV) run pytest

market-provider-start: ## Start the sidecar on :18081.
	cd $(PROVIDER_DIR) && $(UV) run uvicorn fireman_market_provider.app:app --host 0.0.0.0 --port 18081

# -----------------------------------------------------------------------------
# Container images
# -----------------------------------------------------------------------------

build-backend-image: ## Build $(BACKEND_IMAGE) image.
	$(DOCKER) build -t $(BACKEND_IMAGE) -f $(PROJECT_ROOT)/Dockerfile $(PROJECT_ROOT)

build-web-image: ## Build $(WEB_IMAGE) image.
	$(DOCKER) build -t $(WEB_IMAGE) --build-arg API_PROXY_TARGET=$(WEB_API_PROXY_TARGET) -f $(WEB_DIR)/Dockerfile $(WEB_DIR)

build-market-provider-image: ## Build $(MARKET_PROVIDER_IMAGE) image.
	$(DOCKER) build -t $(MARKET_PROVIDER_IMAGE) -f $(PROVIDER_DIR)/Dockerfile $(PROVIDER_DIR)

build-images: build-backend-image build-web-image build-market-provider-image ## Build all three images.

# -----------------------------------------------------------------------------
# Lifecycle / orchestration
# -----------------------------------------------------------------------------

dev: ## Run backend, web and market-provider locally via scripts/dev.sh.
	$(PROJECT_ROOT)/scripts/dev.sh

docker-up: ## docker compose up -d --build using docker/docker-compose.yml.
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --build

docker-down: ## Stop the docker compose stack.
	$(COMPOSE) -f $(COMPOSE_FILE) down

integration-test: ## Backend integration tests using temp SQLite + fixtures.
	$(GO) test -tags=integration $(GO_PKGS)

ci: backend-check web-check market-provider-install market-provider-test integration-test ## Full CI pipeline.
