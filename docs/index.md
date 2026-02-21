# Nexus Framework

The Nexus Framework is a provider-agnostic, secure integration layer for managing OAuth 2.0 and OIDC connections. It abstracts away the complexity of managing tokens, refreshes, and provider quirks, allowing your agents and services to focus on business logic.

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

- **[Architecture](architecture.md)**: System overview, components, and data flow.
- **[Deployment & Config](deployment.md)**: How to configure, build, and deploy the services.
- **[Agent Integration Guide](guides/integrating-agents.md)**: How to build agents that consume connections (including the Go Bridge).
- **[Provider Management Guide](guides/managing-providers.md)**: How to register and configure identity providers (OAuth2, API Keys).
- **[API Reference](reference/api.md)**: Links to OpenAPI specifications.
- **[Security Model](reference/security-model.md)**: Security guardrails and hardening.
- **[Roadmap](ROADMAP.md)**: Known issues and future plans.

## Quick Links

- **[Broker Service](nexus-broker/README.md)**: Backend service details.
- **[Gateway Service](nexus-gateway/README.md)**: Frontend API service details.
- **[Bridge Library](nexus-bridge/README.md)**: Go client library details.
