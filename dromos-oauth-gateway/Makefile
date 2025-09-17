# Makefile for dromos-oauth-gateway

.PHONY: run-grpc run-rest build-grpc build-rest generate tidy test help

# Defaults (override on CLI or env)
BROKER_BASE_URL ?= http://localhost:8080
STATE_KEY       ?= $(shell openssl rand -base64 32)
PORT_GRPC       ?= 9090
PORT_HTTP       ?= 8090

help:
	@echo "make build-grpc     # Build gRPC + HTTP gateway binary"
	@echo "make run-grpc       # Run gRPC + HTTP gateway"
	@echo "make build-rest     # Build REST-only binary"
	@echo "make run-rest       # Run REST-only server"
	@echo "make generate       # Regenerate protobuf/gateway code (buf)"
	@echo "make tidy           # Go module tidy"
	@echo "make test           # Run tests"

build-grpc:
	go build -o bin/oauth-gateway-grpc ./cmd/oha-grpc

run-grpc: build-grpc
	PORT_GRPC=$(PORT_GRPC) PORT_HTTP=$(PORT_HTTP) BROKER_BASE_URL=$(BROKER_BASE_URL) STATE_KEY=$(STATE_KEY) ./bin/oauth-gateway-grpc

build-rest:
	go build -o bin/oauth-gateway-rest ./cmd/oha

run-rest: build-rest
	BROKER_BASE_URL=$(BROKER_BASE_URL) STATE_KEY=$(STATE_KEY) ./bin/oauth-gateway-rest

generate:
	buf generate

tidy:
	go mod tidy

test:
	go test ./...


#touching to test pipeline
