# Multi-stage build for Dromos OAuth Broker

FROM golang:1.24-alpine AS builder
WORKDIR /app

# Enable modules and install build deps
RUN apk add --no-cache git ca-certificates build-base

# Pre-cache modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source
COPY . .

# Build static binary
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
ARG VERSION=dev
RUN go build -ldflags "-X main.Version=${VERSION}" -o /out/nexus-broker ./cmd/nexus-broker

FROM alpine:3.21 AS runtime
WORKDIR /app
RUN addgroup -S app && adduser -S app -G app \
    && apk add --no-cache ca-certificates

COPY --from=builder /out/nexus-broker /usr/local/bin/nexus-broker

# Port and environment
ENV PORT=8080
EXPOSE 8080

USER app
ENTRYPOINT ["/usr/local/bin/nexus-broker"]

