# Multi-stage build for Dromos OAuth Gateway (REST server)

FROM golang:1.21-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git ca-certificates build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -o /out/oauth-gateway-rest ./cmd/oha

FROM alpine:3.19 AS runtime
WORKDIR /app
RUN addgroup -S app && adduser -S app -G app \
    && apk add --no-cache ca-certificates

COPY --from=builder /out/oauth-gateway-rest /usr/local/bin/oauth-gateway-rest

# Default envs
ENV PORT=8090
EXPOSE 8090

USER app
ENTRYPOINT ["/usr/local/bin/oauth-gateway-rest"]

