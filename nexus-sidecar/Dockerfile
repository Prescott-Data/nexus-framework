FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY nexus-sidecar/go.mod nexus-sidecar/go.sum ./nexus-sidecar/
COPY nexus-bridge/go.mod nexus-bridge/go.sum ./nexus-bridge/
COPY nexus-sdk/go.mod nexus-sdk/go.sum ./nexus-sdk/

WORKDIR /src/nexus-sidecar
RUN go mod download

WORKDIR /src
COPY nexus-sidecar ./nexus-sidecar
COPY nexus-bridge ./nexus-bridge
COPY nexus-sdk ./nexus-sdk

WORKDIR /src/nexus-sidecar
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION}" -o /bin/nexus-sidecar ./cmd/nexus-sidecar

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/nexus-sidecar /nexus-sidecar
EXPOSE 8070
USER nonroot:nonroot
ENTRYPOINT ["/nexus-sidecar"]
