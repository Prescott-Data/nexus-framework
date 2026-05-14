# Nexus Framework - Root Makefile

.PHONY: up down restart logs build test clean

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
