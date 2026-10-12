# Nexus Framework - Root Makefile

.PHONY: up down restart logs build test clean stamp check-version version cli

# Single source of truth for the framework version. Every build and spec
# derives from this file so a bump propagates from one place.
VERSION := $(shell cat VERSION | tr -d '[:space:]')
export VERSION

# Print the current version
version:
	@echo $(VERSION)

# Propagate the VERSION file into OpenAPI specs and the docs site
stamp:
	@bash scripts/stamp-version.sh

# CI drift-guard: stamped files, changelog top section and git tags must all agree with VERSION
check-version:
	@bash scripts/check-version.sh

# Default: Start the entire stack in detached mode
up:
	docker-compose up -d --build

# Stop and remove containers
down:
	docker-compose down

# Restart the stack
restart: down up

# Tail logs
logs:
	docker-compose logs -f

# Build images without starting
build:
	docker-compose build

# Build the nexus CLI into nexus-cli/nexus, stamped with VERSION
cli:
	(cd nexus-cli && go build -ldflags "-X github.com/Prescott-Data/nexus-framework/nexus-cli/internal/cli.Version=$(VERSION)" -o nexus ./cmd/nexus)

# Run all unit + contract tests (no Docker required)
test:
	@echo "Running tests for all modules..."
	(cd nexus-broker && go test ./...)
	(cd nexus-gateway && go test ./...)
	(cd nexus-bridge && go test ./... -timeout=30s)
	(cd nexus-sidecar && go test ./...)
	(cd nexus-sdk && go test ./...)
	(cd nexus-cli && go test ./...)
	(cd nexus-sdk-python && python3 -m unittest discover -s tests -q)
	(cd nexus-sdk-ts && npm run build)

# Run full E2E dependency tests (Phase 1 + Phase 2 with Docker)
test-e2e:
	@bash scripts/test-e2e.sh

# Run tests that need a real PostgreSQL instance. These are excluded from
# `make test` behind the `integration` build tag because they cannot run
# without a database. Point NEXUS_TEST_DATABASE_URL at a database that has had
# the broker migrations applied.
#
#   make test-integration NEXUS_TEST_DATABASE_URL="postgres://user@host:5432/nexus?sslmode=disable"
test-integration:
	@test -n "$(NEXUS_TEST_DATABASE_URL)" || { \
		echo "❌ NEXUS_TEST_DATABASE_URL is not set."; \
		echo "   Example: make test-integration NEXUS_TEST_DATABASE_URL=\"postgres://nexus@localhost:5432/nexus?sslmode=disable\""; \
		exit 1; \
	}
	(cd nexus-broker && NEXUS_TEST_DATABASE_URL="$(NEXUS_TEST_DATABASE_URL)" go test -tags=integration ./... -count=1)

# Clean up build artifacts and temp files
clean:
	rm -rf nexus-broker/bin nexus-gateway/bin
	rm -rf nexus-broker/tmp nexus-gateway/tmp
	docker-compose down -v
