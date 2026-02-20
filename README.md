# Nexus Framework

[![Go Reference](https://pkg.go.dev/badge/github.com/Prescott-Data/nexus-framework.svg)](https://pkg.go.dev/github.com/Prescott-Data/nexus-framework)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

The **Nexus Framework** is a provider-agnostic, secure integration layer for managing identity and API connections, including OAuth 2.0, OIDC, API Keys, Basic Auth, and custom static credentials (like AWS SigV4). It abstracts away the complexity of managing tokens, cryptographic signing, credential refreshes, and provider quirks, allowing your agents and services to focus on business logic while maintaining zero-trust security.

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
- **[Roadmap](docs/ROADMAP.md)**: Known issues and future plans.

## Quick Links

- **[Broker Service](nexus-broker/README.md)**: Backend service details.
- **[Gateway Service](nexus-gateway/README.md)**: Frontend API service details.
- **[Bridge Library](nexus-bridge/README.md)**: Go client library details.
