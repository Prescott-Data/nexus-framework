---
icon: material/cloud-upload-outline
---

# Deploying Nexus

This guide covers deploying the Broker and Gateway to a production environment. For local development setup, see [Deploy in Five Minutes](../getting-started/quickstart.md).

## Prerequisites

- PostgreSQL 14 or later
- Docker (for containerised deployment) or Go 1.22+ (for binary deployment)
- TLS termination at your load balancer or reverse proxy

## Generate secrets

Both services require secrets that must be generated before deployment and stored securely.

```bash
openssl rand -base64 32   # ENCRYPTION_KEY  — Broker only
openssl rand -base64 32   # STATE_KEY       — must be identical on Broker and Gateway
openssl rand -hex 32      # BROKER_API_KEY  — shared between Broker and Gateway
```

Store these in your secret manager (AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, Kubernetes Secrets). Do not commit them to source control.

## Broker environment variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | yes | PostgreSQL connection string |
| `ENCRYPTION_KEY` | yes | 32-byte base64 key for AES-GCM token encryption |
| `STATE_KEY` | yes | 32-byte base64 key for OAuth state HMAC signing — must match Gateway |
| `BASE_URL` | yes | Public URL of the Broker, e.g. `https://broker.internal.example.com` |
| `API_KEY` | conditional | Single key the Gateway uses to authenticate with the Broker. Required unless `API_KEYS`, `API_KEY_FILE`, or `API_KEYS_FILE` supplies keys. |
| `API_KEYS` | no | Comma-separated Broker API keys for key rollover. |
| `API_KEY_FILE` | no | Mounted secret file containing one Broker API key; reloaded without process restart. |
| `API_KEYS_FILE` | no | Mounted secret file containing comma- or newline-separated Broker API keys; reloaded without process restart. |
| `API_KEY_RELOAD_INTERVAL` | no | File reload interval for `API_KEY_FILE` and `API_KEYS_FILE` (default: `30s`). |
| `REDIRECT_PATH` | no | OAuth callback path (default: `/auth/callback`) |
| `ALLOWED_CIDRS` | no | Comma-separated CIDRs for IP allowlisting, e.g. `10.0.0.0/8` |
| `ALLOWED_RETURN_DOMAINS` | no | Comma-separated allowed domains for `return_url` validation |

## Gateway environment variables

| Variable | Required | Description |
|---|---|---|
| `STATE_KEY` | yes | Same value as the Broker's `STATE_KEY` |
| `BROKER_BASE_URL` | yes | Internal URL of the Broker, e.g. `http://broker.internal:8080` |
| `BROKER_API_KEY` | yes | The Broker's `API_KEY` |
| `PORT` | no | Port to listen on (default: `8090`) |

## Sidecar environment variables

The Sidecar is optional. Deploy it next to each agent that needs it (same pod or compose network), not as a shared central service, and never expose it publicly: it authenticates callers by network locality alone.

| Variable | Required | Description |
|---|---|---|
| `GATEWAY_BASE_URL` | yes | URL of the Gateway, e.g. `http://gateway:8090` |
| `NEXUS_ROUTES` | yes | Allowlisted upstream routes, e.g. `github=https://api.github.com` |
| `TOKEN_CACHE_TTL` | no | Fallback credential cache lifetime for payloads without an expiry, e.g. `5m` |
| `REQUEST_BODY_LIMIT` | no | Body buffer cap for signing strategies (default: `10MiB`) |
| `PORT` | no | Port to listen on (default: `8070`) |

## Network topology

```
Internet
    │
    ▼
Load Balancer / Reverse Proxy (TLS termination)
    │                   │
    ▼                   ▼
Gateway             Broker
(public or          (internal network only)
 internal)               │
    │                    ▼
    └───────────────► PostgreSQL
```

The Broker handles OAuth callbacks and must be reachable from the public internet at its `BASE_URL/auth/callback`. All other Broker traffic should be internal only.

The Gateway can be internal-only if your agents run inside the same network. Make it public only if agents run outside your network perimeter.

## Database setup

Run the migration before starting the Broker:

```bash
cd nexus-broker
go run ./cmd/migrate
```

This creates the `provider_profiles`, `connections`, `tokens`, and `audit_events` tables.

## Docker Compose (production-ready)

```yaml
services:
  broker:
    image: ghcr.io/prescott-data/nexus-broker:latest
    environment:
      DATABASE_URL: postgres://nexus:${DB_PASSWORD}@postgres:5432/nexus
      ENCRYPTION_KEY: ${ENCRYPTION_KEY}
      STATE_KEY: ${STATE_KEY}
      BASE_URL: https://broker.internal.example.com
      API_KEY: ${BROKER_API_KEY}
      ALLOWED_CIDRS: "10.0.0.0/8"
    networks:
      - internal

  gateway:
    image: ghcr.io/prescott-data/nexus-gateway:latest
    environment:
      STATE_KEY: ${STATE_KEY}
      BROKER_BASE_URL: http://broker:8080
      BROKER_API_KEY: ${BROKER_API_KEY}
      PORT: "8090"
    ports:
      - "8090:8090"
    networks:
      - internal
      - public

  # Optional: co-located proxy for non-Go agents. Keep it on the internal
  # network only; it has no caller authentication of its own.
  sidecar:
    image: ghcr.io/prescott-data/nexus-sidecar:latest
    environment:
      GATEWAY_BASE_URL: http://gateway:8090
      NEXUS_ROUTES: "github=https://api.github.com"
      TOKEN_CACHE_TTL: 5m
    networks:
      - internal

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: nexus
      POSTGRES_USER: nexus
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - internal

networks:
  internal:
  public:

volumes:
  postgres_data:
```

## Key management

`ENCRYPTION_KEY` must remain stable across deployments. Changing it makes every stored token permanently unreadable. Treat it as you would a database master password.

`STATE_KEY` can be rotated, but doing so invalidates all in-flight OAuth flows (pending connections at the moment of rotation). Any user mid-authorization will see an "invalid state" error and need to restart the flow.

Both keys must be identical across all instances of the same service. In Kubernetes, use a single `Secret` object mounted into both the Broker and Gateway pods for `STATE_KEY`.

For Broker API key rotation, prefer `API_KEY_FILE` or `API_KEYS_FILE` backed by a mounted secret. The Broker reloads those files on `API_KEY_RELOAD_INTERVAL`, so updating the secret volume can add or remove accepted keys without restarting the pod.

## Health checks

The Broker exposes `GET /health` and the Gateway exposes `GET /health`. Both return `200 OK` when the service is ready. Configure your load balancer to use these endpoints. The Sidecar also exposes `GET /health` plus Prometheus metrics at `GET /metrics`.

## Upgrading

Run database migrations before starting the new Broker version:

```bash
go run ./cmd/migrate
```

Migrations are additive and backward-compatible within the same minor version. Check the release notes for any breaking schema changes between major versions.
