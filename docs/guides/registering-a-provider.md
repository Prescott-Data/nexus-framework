---
icon: material/plus-network-outline
---

# Providers

A provider profile tells Nexus how to authenticate users against a third-party service and how to apply those credentials to outgoing requests. This guide covers registering new providers, the full range of auth types Nexus supports, and how to list, update, and delete providers after they are registered.

For declarative provider management in production, see [Security-as-Code](security-as-code.md).

---

## Step 1 — Set up the OAuth app in the provider console

Every OAuth2 provider requires you to register an application in their developer portal before issuing credentials. The terminology varies — "OAuth Apps", "API Credentials", "Integrations" — but the process is the same: create an app, collect the client ID and secret, and register the Broker's callback URI.

### What you need from the provider

| Field | Description |
|---|---|
| Client ID | The public identifier for your application |
| Client Secret | The secret Nexus uses to exchange authorization codes for tokens |
| Authorization URL | The endpoint users are redirected to for consent |
| Token URL | The endpoint Nexus calls to exchange codes and refresh tokens |
| Issuer URL | For OIDC-capable providers — replaces auth URL and token URL |

### The redirect URI

Register the Broker's callback endpoint as the redirect URI in the provider console:

```
https://your-broker.example.com/auth/callback
```

For local development:

```
http://localhost:8080/auth/callback
```

The Broker's `BASE_URL` + `/auth/callback` must match this exactly. Most providers perform strict string matching — a trailing slash or `http` vs `https` mismatch causes every OAuth flow to fail.

### Compliance fields

Most providers sandbox new apps in "Development Mode" until compliance metadata is filled in. This limits you to a small number of test users and blocks production access.

| Field | What to provide |
|---|---|
| App name | Your product name |
| App logo | Your company logo |
| Website URL | `https://your-company.com` |
| Privacy Policy URL | `https://your-company.com/privacy` |
| Terms of Service URL | `https://your-company.com/terms` |
| Support email | Your support address |

Fill these in before requesting production access from the provider.

---

## Step 2 — Register the provider in Nexus

All provider registration goes through `POST /v1/providers` on the Gateway. The `X-API-Key` header must carry your `API_KEY`.

### OAuth2 with OIDC discovery

Use OIDC discovery when the provider supports it (Google, Microsoft Entra, Okta, Auth0). Nexus fetches the authorization endpoint, token endpoint, and JWKS URI automatically from `{issuer}/.well-known/openid-configuration`.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
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
  }' | jq .
```

### OAuth2 with manual endpoints

Use manual configuration for providers without OIDC discovery (GitHub, Slack, Stripe, Salesforce).

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
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

Static credential providers do not use a redirect flow. The `credential_schema` defines the form your application presents to collect the credential from the user.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
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

### Basic auth provider

Username and password credentials. The Bridge encodes them as Base64 and sets the `Authorization: Basic` header automatically.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "jira-basic",
    "auth_type": "basic_auth",
    "params": {
      "credential_schema": {
        "type": "object",
        "required": ["username", "password"],
        "properties": {
          "username": { "type": "string", "title": "Username" },
          "password": { "type": "string", "title": "Password", "format": "password" }
        }
      }
    }
  }' | jq .
```

### SAML provider

SAML providers authenticate users through an enterprise IdP and return signed identity assertions to Nexus. Register the Broker ACS URL (`https://your-broker.example.com/saml/acs`) in the IdP application before testing the flow.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "okta-saml",
    "auth_type": "saml",
    "saml_idp_entity_id": "https://idp.example.com/app/abc123",
    "saml_idp_sso_url": "https://idp.example.com/app/abc123/sso/saml",
    "saml_idp_x509_cert": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
    "saml_sp_entity_id": "https://your-broker.example.com/saml/sp/okta"
  }' | jq .
```

After registration, retrieve the Broker SP metadata with `GET /saml/metadata/{providerID}` on the Broker using `X-API-Key`, then upload it to the IdP if the IdP supports metadata import.

### AWS SigV4 provider

For services that use AWS Signature Version 4 request signing. `params.service` and `params.region` configure the signing scope.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "bedrock-us-east",
    "auth_type": "aws_sigv4",
    "params": {
      "service": "bedrock",
      "region": "us-east-1",
      "credential_schema": {
        "type": "object",
        "required": ["access_key", "secret_key"],
        "properties": {
          "access_key": { "type": "string", "title": "AWS Access Key ID" },
          "secret_key": { "type": "string", "title": "AWS Secret Access Key" }
        }
      }
    }
  }' | jq .
```

### Query param provider

Injects the API key into the URL query string instead of a header. `params.param_name` sets the query parameter name.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "weatherapi",
    "auth_type": "query_param",
    "params": {
      "param_name": "api_token",
      "credential_schema": {
        "type": "object",
        "required": ["api_key"],
        "properties": {
          "api_key": { "type": "string", "title": "API Token" }
        }
      }
    }
  }' | jq .
```

### HMAC signature provider

For APIs that require request signing with a shared secret. `params.header_name`, `params.algo`, and `params.encoding` configure the signature format.

```bash
curl -s -X POST https://your-gateway.example.com/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "webhook-hmac",
    "auth_type": "hmac_payload",
    "params": {
      "header_name": "X-Signature",
      "algo": "sha256",
      "encoding": "hex",
      "credential_schema": {
        "type": "object",
        "required": ["api_secret"],
        "properties": {
          "api_secret": { "type": "string", "title": "Signing Secret" }
        }
      }
    }
  }' | jq .
```

### Provider field reference

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique alias. Used in all subsequent operations. |
| `auth_type` | string | `oauth2`, `saml`, `api_key`, `basic_auth`, `aws_sigv4`, `query_param`, `hmac_payload` |
| `client_id` | string | OAuth 2.0 client ID |
| `client_secret` | string | OAuth 2.0 client secret |
| `issuer` | string | OIDC issuer URL — required when `enable_discovery: true` |
| `auth_url` | string | Authorization endpoint — required when `enable_discovery: false` |
| `token_url` | string | Token endpoint — required when `enable_discovery: false` |
| `api_base_url` | string | Provider API root URL |
| `enable_discovery` | boolean | Fetch endpoints from OIDC discovery document |
| `scopes` | array | Default scopes to request during the OAuth handshake |
| `saml_idp_entity_id` | string | SAML IdP entity ID |
| `saml_idp_sso_url` | string | SAML IdP SSO endpoint |
| `saml_idp_x509_cert` | string | SAML IdP signing certificate as PEM or base64 DER |
| `saml_sp_entity_id` | string | Nexus SAML SP entity ID |
| `params` | object | Provider-specific configuration (schemas, signing params, quirks) |

### Provider-specific quirks

Some providers deviate from the OAuth2 spec in ways that require additional params:

| Provider | Issue | Fix |
|---|---|---|
| Salesforce | Rejects `scope` on the authorization URL | `"params": { "skip_scope_on_auth": true }` |
| Salesforce | Rejects `scope` on the token exchange | `"params": { "skip_scope_on_exchange": true }` |
| Twitter/X | Requires Basic Auth for token exchange | `"auth_header": "client_secret_basic"` |
| Microsoft Entra | Requires `scope` on the token exchange | Default behaviour — no change needed |

---

## Step 3 — Verify the registration

Test an OAuth2 or SAML provider by requesting a connection URL and completing the flow in your browser:

```bash
curl -s -X POST https://your-gateway.example.com/v1/request-connection \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "workspace_id": "test-user-001",
    "provider_id": "PROVIDER_UUID_FROM_REGISTRATION",
    "scopes": ["openid", "email"],
    "return_url": "https://httpbin.org/get"
  }' | jq .
```

Open the `auth_url` from the response in a browser. OAuth providers start an authorization-code flow; SAML providers redirect to the IdP with an AuthnRequest. After authorizing, you should be redirected to `httpbin.org/get` with `connection_id` and `status=success` as query parameters.

---

## Listing providers

```bash
curl -s https://your-gateway.example.com/v1/providers \
  -H "X-API-Key: your-api-key" | jq .
```

To retrieve a condensed metadata view (useful for rendering a connection UI):

```bash
curl -s https://your-gateway.example.com/v1/providers/metadata \
  -H "X-API-Key: your-api-key" | jq .
```

The metadata endpoint returns providers grouped by `auth_type`, with only the fields needed for connection UI rendering: `api_base_url`, `user_info_endpoint`, and `scopes`.

---

## Updating a provider

Use `PATCH` to update specific fields. Do not delete and recreate a provider — this orphans every active connection that references it.

```bash
curl -s -X PATCH https://your-gateway.example.com/v1/providers/PROVIDER_ID \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "client_secret": "ROTATED_SECRET"
  }' | jq .
```

Every update is recorded in the [audit log](../reference/audit-log.md).

---

## Deleting a provider

Deleting a provider removes its configuration. Existing connections that reference the provider will fail credential retrieval immediately because the client credentials are gone. Verify you have migrated or decommissioned all dependent connections before deleting.

```bash
curl -s -X DELETE https://your-gateway.example.com/v1/providers/PROVIDER_ID \
  -H "X-API-Key: your-api-key"
```

Delete operations are audit-logged with the caller IP and timestamp.
