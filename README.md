# Nexus Framework

The Nexus Framework is a provider-agnostic, secure integration layer for managing OAuth 2.0 and OIDC connections. It abstracts away the complexity of managing tokens, refreshes, and provider quirks, allowing your agents and services to focus on business logic.

## ⚠️ Critical Configuration

The Nexus Framework requires two primary shared secrets to operate securely:

1.  **`ENCRYPTION_KEY`**: A 32-byte key used by the Broker to encrypt tokens at rest.
2.  **`STATE_KEY`**: A 32-byte key shared between the Broker and Gateway to sign and verify the OAuth `state` parameter.

**Both services will refuse to start if these variables are missing or invalid.** In distributed deployments, the `STATE_KEY` **must** be identical across all Broker and Gateway instances, or OAuth callbacks will fail with "Invalid state" errors.

Generate a secure key with: `openssl rand -base64 32`

## Quick Start

The fastest way to get started is with Docker Compose. This will spin up the Broker, Gateway, Postgres, and Redis.

```bash
# 1. Configure environment
cp .env.example .env

# 2. Start the stack
make up

# Or if you don't have make:
docker-compose up -d --build
```

- **Broker**: http://localhost:8080
- **Gateway**: http://localhost:8090
- **Admin API Key**: Configured in `.env` (Default: `nexus-admin-key`)

## Documentation

- **[Architecture](docs/architecture.md)**: System overview, components, and data flow.
- **[Deployment & Config](docs/deployment.md)**: How to configure, build, and deploy the services.
- **[Agent Integration Guide](docs/guides/integrating-agents.md)**: How to build agents that consume connections (including the Go Bridge).
- **[Provider Management Guide](docs/guides/managing-providers.md)**: How to register and configure identity providers (OAuth2, API Keys).
- **[API Reference](docs/reference/api.md)**: Links to OpenAPI specifications.
- **[Security Model](docs/reference/security-model.md)**: Security guardrails and hardening.
- **[Technical Debt & Roadmap](docs/reference/tech-debt.md)**: Known issues and future plans.

## Quick Links

- **[Broker Service](nexus-broker/README.md)**: Backend service details.
- **[Gateway Service](nexus-gateway/README.md)**: Frontend API service details.
- **[Bridge Library](nexus-bridge/README.md)**: Go client library details.