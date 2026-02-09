# Dromos OAuth Framework

The Dromos OAuth Framework is a provider-agnostic, secure integration layer for managing OAuth 2.0 and OIDC connections. It abstracts away the complexity of managing tokens, refreshes, and provider quirks, allowing your agents and services to focus on business logic.

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
- **[Bridge Library](bridge/README.md)**: Go client library details.