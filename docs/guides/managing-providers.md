# Managing Providers

A provider in Nexus represents the configuration for a third-party service your agents connect to. This guide covers how to register, update, and delete providers through the Broker's REST API, and how to list the providers available in your workspace.

For declarative provider management using `nexus-cli`, see the [Security-as-Code](security-as-code.md) guide. For production environments where provider configuration is sensitive infrastructure, the declarative approach is preferred.

---

## Registering an OAuth 2.0 provider

POST to `/providers` on the Broker with the provider configuration. The `X-API-Key` header must carry the Broker's `API_KEY`.

### Discovery-based provider

For providers that support OIDC discovery, set `enable_discovery: true` and supply the `issuer` URL. Nexus fetches the authorization endpoint, token endpoint, and JWKS URI from the discovery document automatically.

```bash
curl -s -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "google-workspace",
    "auth_type": "oauth2",
    "client_id": "YOUR_CLIENT_ID",
    "client_secret": "YOUR_CLIENT_SECRET",
    "issuer": "https://accounts.google.com",
    "enable_discovery": true,
    "scopes": ["openid", "email", "profile", "offline_access"]
  }'
```

### Manual configuration

For providers without OIDC discovery, supply the `auth_url` and `token_url` explicitly:

```bash
curl -s -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "github",
    "auth_type": "oauth2",
    "client_id": "YOUR_CLIENT_ID",
    "client_secret": "YOUR_CLIENT_SECRET",
    "auth_url": "https://github.com/login/oauth/authorize",
    "token_url": "https://github.com/login/oauth/access_token",
    "api_base_url": "https://api.github.com",
    "enable_discovery": false,
    "scopes": ["read:user", "user:email"]
  }'
```

---

## Registering a static key provider

For providers that use API keys rather than OAuth, set `auth_type` to `api_key` and define a `credential_schema` that describes the shape of the credential:

```bash
curl -s -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "stripe",
    "auth_type": "api_key",
    "api_base_url": "https://api.stripe.com",
    "credential_schema": {
      "fields": [
        { "name": "secret_key", "label": "Secret Key", "sensitive": true }
      ]
    }
  }'
```

When a connection is established for this provider, the user supplies values for each field in the schema. Nexus encrypts and stores them.

---

## Provider fields reference

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique alias for the provider. Used in all subsequent operations. |
| `auth_type` | string | `oauth2` or `api_key` |
| `client_id` | string | OAuth 2.0 client ID |
| `client_secret` | string | OAuth 2.0 client secret |
| `issuer` | string | OIDC issuer URL (required when `enable_discovery: true`) |
| `auth_url` | string | Authorization endpoint (required when `enable_discovery: false`) |
| `token_url` | string | Token endpoint (required when `enable_discovery: false`) |
| `api_base_url` | string | Provider API root URL |
| `enable_discovery` | boolean | Fetch endpoints from OIDC discovery document |
| `scopes` | array | Default scopes to request during the OAuth handshake |
| `params` | object | Provider-specific extra parameters passed to the authorization request |
| `credential_schema` | object | Field definitions for `api_key` providers |

---

## Listing providers

To see all registered providers in a workspace:

```bash
curl -s http://localhost:8080/providers \
  -H "X-API-Key: your-api-key" | jq .
```

To retrieve grouped metadata (a condensed view suitable for frontend integration logic):

```bash
curl -s http://localhost:8080/providers/metadata \
  -H "X-API-Key: your-api-key" | jq .
```

The metadata endpoint returns providers grouped by `auth_type`, with only the fields needed to render a connection UI: `api_base_url`, `user_info_endpoint`, and `scopes`.

---

## Updating a provider

Updating a provider's `client_secret` or `scopes` is a PATCH operation. Only the fields you include in the request body are changed.

```bash
curl -s -X PATCH http://localhost:8080/providers/google-workspace \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"client_secret": "NEW_SECRET"}'
```

Every update is recorded in the [audit log](../reference/audit-log.md).

---

## Deleting a provider

Deleting a provider removes its configuration from the Broker. Existing connections that reference the provider will fail credential retrieval after deletion because the client credentials are gone.

```bash
curl -s -X DELETE http://localhost:8080/providers/google-workspace \
  -H "X-API-Key: your-api-key"
```

Delete operations are also audit-logged with the caller IP and timestamp.