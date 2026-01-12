# Architecture Overview

## System Components

The Dromos OAuth Framework consists of three main components:

- **dromos-oauth-broker (Backend)**: The core "brain" of the operation. It handles:
    - Provider discovery and registry.
    - Consent specification (PKCE + signed state).
    - Callback handling and code exchange.
    - Token encryption and storage (PostgreSQL).
    - Token retrieval and refresh.
- **dromos-oauth-gateway (Front Door)**: A stateless proxy that exposes a stable gRPC/HTTP API for agents. It:
    - Initiates connections via the Broker.
    - Allows clients to poll for connection status.
    - Fetches tokens on demand (never storing them).
- **bridge (Client Library)**: A universal connector for agents (Go) that manages persistent, observable connections (WebSocket/gRPC) and handles authentication strategies.

## Auth Flow

1.  **Request Connection**: Agent (server) requests a connection from the Gateway. The Gateway calls the Broker to build a consent spec (PKCE + signed state) and returns an `authUrl` + `connection_id`.
2.  **Consent**: The User completes consent at the provider (e.g., Google).
3.  **Callback**: The Provider redirects to the Broker callback. The Broker validates state, exchanges the code for tokens, encrypts/stores them, marks the connection `active`, and redirects the user to the agent’s `return_url` with the `connection_id`.
4.  **Usage**: The Agent fetches tokens on demand from the Gateway using the `connection_id`. If tokens are expired, the broker handles the refresh.

## Key Endpoints

### Gateway (HTTP)
- `POST /v1/request-connection`: Initiate a flow.
- `GET /v1/check-connection/{connection_id}`: Poll status.
- `GET /v1/token/{connection_id}`: Fetch credentials.
- `GET /metrics`: Prometheus metrics.

### Broker (Internal HTTP)
- `POST /auth/consent-spec`: Generate auth URL.
- `GET /auth/callback`: Public provider redirect handler.
- `GET /connections/{connection_id}/token`: Retrieve encrypted token.
- `POST /connections/{connection_id}/refresh`: Force refresh.
- `GET /metrics`: Prometheus metrics.

## Observability

Both services expose Prometheus metrics at `/metrics`.

**Broker Metrics:**
- `oauth_consents_created_total`
- `oauth_token_exchanges_total{status}`
- `oauth_token_get_total{provider,has_id_token}`
- `oidc_discovery_total`
- `oidc_verifications_total`

**Prometheus Config Example:**
```yaml
- job_name: 'oauth-broker'
  scheme: https
  metrics_path: /metrics
  static_configs:
    - targets: ['<broker-domain>']

- job_name: 'oauth-gateway'
  scheme: https
  metrics_path: /metrics
  static_configs:
    - targets: ['<gateway-domain>']
```
