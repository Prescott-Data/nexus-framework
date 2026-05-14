# Provider Types

A provider profile tells Nexus how to authenticate users against a third-party service. The provider type determines the authorization flow and the shape of stored credentials.

## OAuth2

OAuth2 providers use the Authorization Code flow with PKCE. Nexus manages the full token lifecycle — your agents always receive a current access token.

### OIDC discovery

Set `enable_discovery: true` and provide an `issuer` URL. Nexus fetches `{issuer}/.well-known/openid-configuration` to populate `authorization_endpoint` and `token_endpoint` automatically.

Use this for Google, Microsoft Entra ID, Auth0, and any provider with a published discovery document.

### Manual configuration

Set `auth_url` and `token_url` explicitly. Use this for GitHub and other OAuth2 providers without a discovery document.

### Provider profile fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Unique name for this provider within your Nexus instance |
| `auth_type` | yes | `oauth2`, `api_key`, or `basic_auth` |
| `client_id` | OAuth2 | OAuth2 application client ID |
| `client_secret` | OAuth2 | OAuth2 application client secret |
| `auth_url` | OAuth2 (manual) | Authorization endpoint |
| `token_url` | OAuth2 (manual) | Token endpoint |
| `issuer` | OAuth2 (discovery) | OIDC issuer URL |
| `enable_discovery` | no | `true` to use OIDC discovery |
| `scopes` | no | Default OAuth2 scopes for this provider |
| `auth_header` | static | Header name for static-key injection |
| `params` | no | Additional provider-specific parameters as JSON |

### PKCE

All OAuth2 flows use PKCE (RFC 7636). The Broker generates a random `code_verifier`, sends the SHA-256 `code_challenge` to the provider, and verifies the exchange on callback. You do not configure this — it is always enabled.

## Static credentials

Static providers authenticate with credentials that do not expire and cannot be refreshed.

### api_key

A single opaque key. Your backend calls `GET /v1/capture-schema` to get the field definition, presents it to the user, and submits via `POST /v1/capture-credential`. The connection goes directly to `active`. Set `auth_strategy` to `header` or `query_param` on the provider profile to control how the key is injected.

### basic_auth

Username and password pair. The capture flow is identical to `api_key`. The stored credentials map has `username` and `password` keys. The auth strategy is always `basic_auth`.

## Scopes

The `scopes` array on the provider profile is the default for new connections. Individual connections can request a different subset by passing `scopes` to `POST /v1/request-connection`. Static providers ignore scopes entirely.

## Registration and deletion

Register providers via `POST /v1/providers`. Each provider has a unique name. Deleting a provider with `DELETE /v1/providers/{id}` does not delete its connections — clean up connections first to avoid orphaned records.
