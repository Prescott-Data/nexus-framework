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

# Run all tests (Broker + Gateway + Bridge + SDK)
test:
	@echo "Running tests for all modules..."
	(cd nexus-broker && go test ./...)
	(cd nexus-gateway && go test ./...)
	(cd nexus-bridge && go test ./...)
	(cd nexus-sdk && go test ./...)

# Clean up build artifacts and temp files
clean:
	rm -rf nexus-broker/bin nexus-gateway/bin
	rm -rf nexus-broker/tmp nexus-gateway/tmp
	docker-compose down -v
