# Nexus Broker Service

The **Nexus Broker** is the internal "System of Record" for the Nexus Framework. It manages identity provider configurations, orchestrates OAuth 2.0 handshakes, and securely stores credentials using AES-256-GCM encryption.

**⚠️ Warning:** This service handles sensitive credentials (Refresh Tokens, API Keys). It should be deployed in a private network zone and never exposed directly to the public internet, except for the specific `/auth/callback` endpoint.

## Documentation

- **[Architecture & Design](../docs/services/broker.md)**: Detailed breakdown of the Broker's role and security model.
- **[Provider Registration](../docs/PROVIDER_REGISTRATION_GUIDE.md)**: `curl` templates for adding Google, GitHub, and custom API providers.
- **[Deployment Guide](../docs/deployment.md)**: Production configuration and key management.

## Quick Start (Local)

### 1. Prerequisites
- PostgreSQL 13+
- Redis 6+
- Go 1.21+

### 2. Run Dependencies
```bash
docker-compose up -d postgres redis
```

### 3. Configure Environment
Create a `.env` file or export these variables:

```bash
export DATABASE_URL="postgres://nexus:secret@localhost:5432/nexus?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export BASE_URL="http://localhost:8080"
export PORT="8080"

# Generate these using `openssl rand -base64 32`
export ENCRYPTION_KEY="<32-byte-base64-string>"
export STATE_KEY="<32-byte-base64-string>"

# Internal Admin Key
export API_KEY="nexus-admin-key"
```

### 4. Run the Broker
```bash
go run ./cmd/nexus-broker
```

## API Overview

The Broker API is protected by the `X-API-Key` header.

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/providers` | GET/POST | Manage provider profiles. |
| `/auth/consent-spec` | POST | Generate an authorization URL. |
| `/auth/callback` | GET | Public endpoint for OAuth redirects. |
| `/connections/{id}/token` | GET | Retrieve encrypted credentials. |
| `/connections/{id}/refresh`| POST | Force a token refresh. |
| `/health` | GET | Liveness probe. |
| `/metrics` | GET | Prometheus metrics. |

## Development

### Run Tests
```bash
go test ./...
```

### Build Binary
```bash
go build -o nexus-broker ./cmd/nexus-broker
```
