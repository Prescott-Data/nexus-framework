# API Overview

Nexus exposes two API surfaces: the Gateway API, which agents and applications call, and the Broker API, which is internal and used only by the Gateway and administrative tooling.

---

## Gateway API

The Gateway API is the stable, public-facing surface for all agent integrations. It is versioned at `/v1` and follows standard REST conventions.

The full OpenAPI 3.0 specification is in the repository at [`openapi.yaml`](https://github.com/Prescott-Data/nexus-framework/blob/main/openapi.yaml). The v1 surface is frozen. No breaking changes will be made without a major version bump and a deprecation period.

### Core endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/request-connection` | Initiate an OAuth handshake or API key connection |
| `GET` | `/v1/check-connection/{id}` | Poll the connection status |
| `GET` | `/v1/token/{id}` | Retrieve credentials for an active connection |
| `POST` | `/v1/obo-sessions` | Create an On Behalf Of session |
| `DELETE` | `/v1/obo-sessions/{id}` | Close an OBO session |
| `GET` | `/v1/health` | Health check |

### Authentication

The Gateway does not require authentication for connection initiation or token retrieval. Access control is enforced by the `connection_id` itself. Only callers who hold a valid `connection_id` can retrieve its credentials. Protect `connection_id` values as you would a session token.

For administrative operations (provider management), calls go directly to the Broker and require the `X-API-Key` header.

### Credential payload

The `GET /v1/token/{id}` endpoint returns a structured payload regardless of credential type:

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": {
    "access_token": "...",
    "expires_at": 1715000000
  },
  "expires_at": 1715000000
}
```

The `strategy.type` field tells you how to apply the credentials. See [Provider Types](../concepts/provider-types.md) for the full list of strategy types and their credential shapes.

---

## Broker API

The Broker API is internal. It is called by the Gateway for token operations and by administrative tooling (including `nexus-cli`) for provider management. Do not expose the Broker API to agents or to untrusted networks.

The Broker's OpenAPI specification is at [`nexus-broker/openapi.yaml`](https://github.com/Prescott-Data/nexus-framework/blob/main/nexus-broker/openapi.yaml). It is marked internal and evolving. Fields and endpoints may change between minor versions.

### Provider management endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/providers` | List all registered providers |
| `GET` | `/providers/metadata` | Grouped metadata for frontend use |
| `POST` | `/providers` | Register a new provider |
| `GET` | `/providers/{name}` | Get a provider by name |
| `PUT` | `/providers/{name}` | Replace a provider configuration |
| `PATCH` | `/providers/{name}` | Update specific fields of a provider |
| `DELETE` | `/providers/{name}` | Delete a provider |

### Authentication

All Broker API calls require the `X-API-Key` header carrying the value set in the Broker's `API_KEY` environment variable.

### Audit endpoint

| Method | Path | Description |
|---|---|---|
| `GET` | `/audit` | Query audit log events |

See the [Audit Log reference](audit-log.md) for query parameters and response schema.
