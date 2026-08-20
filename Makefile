# Nexus Framework - Root Makefile

.PHONY: up down restart logs build test clean stamp check-version version

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

# CI drift-guard: fail if any stamped file is out of sync with the VERSION file
check-version:
	@bash scripts/stamp-version.sh
	@git diff --exit-code -- openapi.yaml nexus-broker/openapi.yaml mkdocs.yml \
		|| { echo "❌ Version drift detected. Run 'make stamp' and commit the result."; exit 1; }
	@echo "✅ Version $(VERSION) is in sync across all stamped files."

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

# Run all unit + contract tests (no Docker required)
test:
	@echo "Running tests for all modules..."
	(cd nexus-broker && go test ./...)
	(cd nexus-gateway && go test ./...)
	(cd nexus-bridge && go test ./... -timeout=30s)
	(cd nexus-sidecar && go test ./...)
	(cd nexus-sdk && go test ./...)
	(cd nexus-sdk-python && python3 -m unittest discover -s tests -q)
	(cd nexus-sdk-ts && npm run build)

# Run full E2E dependency tests (Phase 1 + Phase 2 with Docker)
test-e2e:
	@bash scripts/test-e2e.sh

# Clean up build artifacts and temp files
clean:
	rm -rf nexus-broker/bin nexus-gateway/bin
	rm -rf nexus-broker/tmp nexus-gateway/tmp
	docker-compose down -v
