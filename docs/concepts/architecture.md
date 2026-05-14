---
icon: material/layers-triple
---

# Architecture

Nexus is split into a control plane and a data plane. The control plane manages the lifecycle of credentials: registering providers, completing OAuth handshakes, storing tokens, and refreshing them before they expire. The data plane serves credentials to agents when they need them and injects those credentials into outgoing requests.

Understanding this separation is the mental model for everything else in this documentation.

---

## The four components

### The Broker

The Broker is the most sensitive component in the system. It is the only service that ever holds refresh tokens or provider API keys. Everything it stores is encrypted at rest using AES-GCM 256-bit with a key you supply and manage. The Broker never sends a refresh token to any other service. When the Gateway asks for credentials, the Broker decrypts the stored token, fetches a fresh access token from the provider, and returns only that.

The Broker runs a background refresh loop. Before any stored token expires, the Broker fetches a new one using the stored refresh token. If a provider rejects the refresh (for example, because a user revoked access), the Broker transitions that connection to `attention_required` and records the failure in the audit log. The agent does not need to handle this case; it will simply receive an error on the next credential request and can surface it to the user.

See [The Broker](broker.md) for database schema, encryption details, and refresh loop internals.

### The Gateway

The Gateway is the public-facing API that agents call. It accepts REST requests, proxies them to the Broker using an internal API key, and returns the response. Agents never reach the Broker directly. This decoupling means you can expose the Gateway to agents running outside your network without exposing the Broker.

The Gateway handles CORS, request validation, and API versioning. From an agent's perspective, the Gateway is Nexus.

See [The Gateway](gateway.md) for API endpoints, the resolve workflow, and health checks.

### The Bridge

The Bridge is a Go library that runs inside your agent process. You instantiate it with a Gateway client, and it handles credential retrieval and request signing automatically. For a persistent WebSocket or gRPC connection, you call `bridge.MaintainWebSocket()` or `bridge.MaintainGRPCConnection()` and the Bridge keeps the connection authenticated through token rotations, including exponential backoff on failures. It also exposes a Prometheus `/metrics` endpoint for operational visibility.

Use the Bridge when your agent is written in Go and makes ongoing connections to provider services.

See [The Bridge](bridge.md) for transport details, the authentication engine, and observability hooks.

### The SDKs

The SDKs are thin HTTP clients for the Gateway API. They expose `GetToken()`, `ResolveToken()`, and related methods, giving you direct control over credential retrieval without the connection management logic the Bridge provides. Use an SDK when you want to fetch credentials explicitly, when you are building an MCP server, or when your agent is written in TypeScript or Python.

Nexus ships three SDKs with full feature parity: Go, TypeScript, and Python. See the [SDK Reference](../reference/api.md) for the full method surface.

---

## The OAuth handshake flow

When a user connects a new provider account, the flow runs as follows.

Your application backend calls `POST /v1/request-connection` on the Gateway, passing the user identifier, the provider name, the requested scopes, and a return URL. The Gateway forwards this to the Broker, which generates a unique `state` parameter signed with the `STATE_KEY` and returns an authorization URL and a `connection_id`.

Your application redirects the user's browser to the authorization URL. The user authenticates with the provider and grants consent. The provider redirects back to the Broker's callback endpoint, which validates the `state` parameter, exchanges the authorization code for tokens, encrypts the tokens, and stores them in PostgreSQL.

The Broker then redirects the user's browser to your `return_url` with the `connection_id` and a `status=success` parameter. Your application captures the `connection_id` and stores it. That is the only thing your application needs to hold. The actual tokens stay in the Broker.

---

## The credential retrieval flow

When your agent needs to call a provider, the flow is simpler.

Your agent calls `GET /v1/token/{connection_id}` on the Gateway. The Gateway forwards the request to the Broker, which retrieves the stored tokens, decrypts them in RAM, and returns a credential payload to the Gateway. The payload contains a `strategy` field that describes how to use the credentials and a `credentials` object with the actual values.

The `strategy` field exists because Nexus supports multiple credential types. An OAuth2 connection returns `{"type": "oauth2"}` with an `access_token`. An API key connection returns `{"type": "api_key"}` with the key value in a field matching the provider's schema. The Bridge handles strategy interpretation automatically. If you are making direct HTTP calls, you inspect `strategy.type` and apply the credentials accordingly.

---

## Connection states

A connection moves through a small set of states during its lifetime.

`pending` is the initial state after `request-connection` returns but before the user has completed consent. `active` is the normal operating state: tokens are valid and the background refresh loop is keeping them current. `attention_required` means the last refresh attempt failed with a provider error that cannot be retried, typically because the user revoked the application's access. `failed` means the initial token exchange failed.

Your application should surface `attention_required` to the user so they can reconnect the provider account. See [Handling Attention State](../guides/attention-state.md) for implementation details.

---

## What Nexus does not do

Nexus does not make API calls to providers on behalf of agents. It provides credentials; agents make the calls. Nexus does not manage authorization within your own application (which users can access which connections). It manages authentication with external providers. Nexus does not run inside your agent. The Bridge is an in-process library, but the Broker and Gateway are network services that you deploy separately.
