# Deployment Guide

This guide covers how to configure and deploy the Nexus Framework (Broker and Gateway) in a production environment.

## 1. Configuration Reference

Both services follow the **12-Factor App** methodology and are configured entirely via environment variables.

### Shared Security Keys
These keys must be generated securely (e.g., `openssl rand -base64 32`) and injected into the containers.

| Variable | Service | Description |
| :--- | :--- | :--- |
| `STATE_KEY` | **Both** | 32-byte Base64 key. Used to sign/verify OIDC state. **Must be identical on both services.** |
| `ENCRYPTION_KEY` | **Broker** | 32-byte Base64 key. Encrypts tokens at rest. **Loss = Data Loss.** |
| `API_KEY` | **Broker** | High-entropy string. The "password" for the Gateway to talk to the Broker. |
| `BROKER_API_KEY` | **Gateway** | Must match the `API_KEY` set on the Broker. |

### Nexus Broker Configuration

| Variable | Required | Description |
| :--- | :--- | :--- |
| `PORT` | No | HTTP Listen Port (default `8080`). |
| `DATABASE_URL` | **Yes** | PostgreSQL DSN (e.g., `postgres://user:pass@host:5432/db`). |
| `REDIS_URL` | **Yes** | Redis connection string (e.g., `redis://host:6379`). |
| `BASE_URL` | **Yes** | Public URL of the Broker (e.g., `https://auth.my-org.com`). Used for OAuth redirects. |
| `ALLOWED_CIDRS` | No | Comma-separated list of IPs allowed to call the API (default `0.0.0.0/0`). |

### Nexus Gateway Configuration

| Variable | Required | Description |
| :--- | :--- | :--- |
| `PORT` | No | HTTP REST Port (default `8090`). |
| `GRPC_PORT` | No | gRPC Port (default `9090`). |
| `BROKER_BASE_URL` | **Yes** | Internal URL of the Broker (e.g., `http://nexus-broker:8080`). |
| `ALLOWED_CIDRS` | No | IP Allowlist for incoming agent traffic. |

---

## 2. Docker Compose (Production Example)

This setup runs the full stack: Broker, Gateway, PostgreSQL, and Redis.

```yaml
version: '3.8'

services:
  nexus-broker:
    image: ghcr.io/prescott/nexus-broker:latest
    restart: always
    environment:
      - PORT=8080
      - DATABASE_URL=postgres://nexus:secret@postgres:5432/nexus?sslmode=disable
      - REDIS_URL=redis://redis:6379
      - BASE_URL=https://auth.example.com
      - ENCRYPTION_KEY=${ENCRYPTION_KEY} # Inject via .env or CI/CD
      - STATE_KEY=${STATE_KEY}
      - API_KEY=${INTERNAL_API_KEY}
    depends_on:
      - postgres
      - redis
    ports:
      - "8080:8080" # Expose only if behind a load balancer/proxy

  nexus-gateway:
    image: ghcr.io/prescott/nexus-gateway:latest
    restart: always
    environment:
      - PORT=8090
      - GRPC_PORT=9090
      - BROKER_BASE_URL=http://nexus-broker:8080
      - BROKER_API_KEY=${INTERNAL_API_KEY}
      - STATE_KEY=${STATE_KEY}
    ports:
      - "8090:8090" # Public REST API
      - "9090:9090" # Public gRPC API

  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: nexus
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: nexus
    volumes:
      - pg_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine

volumes:
  pg_data:
```

---

## 3. Kubernetes Deployment Guidelines

When deploying to Kubernetes, follow these best practices:

### Networking & Ingress
1.  **Gateway:** Expose via an **Ingress** (for REST) and a **Service** (Type: LoadBalancer or NodePort for gRPC). Ensure your Ingress controller supports gRPC if you expose it on the same host.
2.  **Broker:** 
    *   **Internal API:** Should NOT be exposed via Ingress. Access it only via internal ClusterIP from the Gateway.
    *   **Public Callback:** You *must* expose the `/auth/callback` endpoint to the public internet so identity providers (Google, etc.) can redirect users back. Use a specific Ingress path rule for this.

### Secret Management
Never hardcode keys in YAML. Use Kubernetes Secrets.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nexus-secrets
type: Opaque
data:
  encryption-key: <base64-encoded-key>
  state-key: <base64-encoded-key>
  api-key: <base64-encoded-key>
  db-url: <base64-encoded-dsn>
```

Inject them into your pods:

```yaml
env:
  - name: ENCRYPTION_KEY
    valueFrom:
      secretKeyRef:
        name: nexus-secrets
        key: encryption-key
```

### Database Migrations
Run migrations as a **Job** or an **InitContainer** before the Broker starts.

```bash
# Example Job command
/app/nexus-broker migrate up
```

---

## 4. Key Generation

Use `openssl` to generate cryptographically secure keys for your environment variables.

```bash
# Generate ENCRYPTION_KEY and STATE_KEY (32 bytes)
openssl rand -base64 32

# Generate API_KEY (Random high-entropy string)
openssl rand -hex 16
```
