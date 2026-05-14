# Registering a Provider

Before you can create connections, you need a provider profile in Nexus. This guide covers the two steps involved: setting up the OAuth application in the provider's developer console, then registering it in Nexus.

## Step 1 — Set up the OAuth app in the provider console

Every OAuth2 provider requires you to register an application in their developer portal before issuing credentials. The terminology varies — "OAuth Apps", "API Credentials", "Integrations" — but the process is the same.

### What you need from the provider

| Field | Description |
|---|---|
| Client ID | The public identifier for your application |
| Client Secret | The secret Nexus uses to exchange authorization codes for tokens |
| Authorization URL | The endpoint users are redirected to for consent |
| Token URL | The endpoint Nexus calls to exchange codes and refresh tokens |
| Issuer URL | For OIDC-capable providers — replaces auth URL and token URL |

### The redirect URI

Every provider console requires a redirect URI. This must be set to your Broker's callback endpoint:

```
https://your-broker.example.com/auth/callback
```

For local development:

```
http://localhost:8080/auth/callback
```

The Broker's `BASE_URL` + `/auth/callback` must match this exactly. Most providers perform strict string matching — a trailing slash or `http` vs `https` mismatch causes every OAuth flow to fail.

### Compliance fields

Most providers sandbox your app in "Development Mode" until you fill in compliance metadata. This limits you to a small number of test users and blocks production use.

| Field | What to provide |
|---|---|
| App name | Your product name |
| App logo | Your company logo |
| Website URL | `https://your-company.com` |
| Privacy Policy URL | `https://your-company.com/privacy` |
| Terms of Service URL | `https://your-company.com/terms` |
| Support email | Your support address |

Fill these in before requesting production access from the provider.

## Step 2 — Register the provider in Nexus

### OAuth2 with OIDC discovery

Use OIDC discovery when the provider supports it (Google, Microsoft Entra, Okta, Auth0). Nexus fetches the authorization and token endpoints automatically from `{issuer}/.well-known/openid-configuration`.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "name": "google-workspace",
    "auth_type": "oauth2",
    "client_id": "YOUR_CLIENT_ID",
    "client_secret": "YOUR_CLIENT_SECRET",
    "issuer": "https://accounts.google.com",
    "enable_discovery": true,
    "scopes": ["openid", "email", "profile", "offline_access"]
  }' | jq .
```

### OAuth2 with manual endpoints

Use manual configuration for providers without OIDC discovery (GitHub, Slack, Stripe).

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "name": "github",
    "auth_type": "oauth2",
    "client_id": "YOUR_CLIENT_ID",
    "client_secret": "YOUR_CLIENT_SECRET",
    "auth_url": "https://github.com/login/oauth/authorize",
    "token_url": "https://github.com/login/oauth/access_token",
    "scopes": ["repo", "read:user"]
  }' | jq .
```

### API key provider

Static credential providers do not use a redirect flow. The `params.credential_schema` field defines the form your application presents to the user to collect credentials.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "name": "airtable",
    "auth_type": "api_key",
    "params": {
      "credential_schema": {
        "type": "object",
        "required": ["api_key"],
        "properties": {
          "api_key": { "type": "string", "title": "Personal Access Token" }
        }
      }
    }
  }' | jq .
```

### Provider-specific quirks

Some providers deviate from the OAuth2 spec in ways that require additional params:

| Provider | Issue | Fix |
|---|---|---|
| Salesforce | Rejects `scope` on the authorization URL | `"params": { "skip_scope_on_auth": true }` |
| Salesforce | Rejects `scope` on the token exchange | `"params": { "skip_scope_on_exchange": true }` |
| Twitter/X | Requires Basic Auth for token exchange | `"auth_header": "client_secret_basic"` |
| Microsoft Entra | Requires `scope` on the token exchange | Default behaviour — no change needed |

## Step 3 — Verify the registration

Test an OAuth2 provider by requesting a connection URL and completing the flow in your browser:

```bash
curl -s -X POST https://your-gateway.example.com/v1/request-connection \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "workspace_id": "test-user-001",
    "provider_id": "PROVIDER_UUID_FROM_REGISTRATION",
    "scopes": ["openid", "email"],
    "return_url": "https://httpbin.org/get"
  }' | jq .
```

Open the `auth_url` from the response in a browser. After authorizing, you should be redirected to `httpbin.org/get` with `connection_id` and `status=success` as query parameters.

## Updating a registered provider

Use `PATCH` to update specific fields. Do not delete and recreate a provider — this orphans every active connection.

```bash
curl -s -X PATCH https://your-gateway.example.com/v1/providers/PROVIDER_ID \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "client_secret": "ROTATED_SECRET"
  }' | jq .
```
