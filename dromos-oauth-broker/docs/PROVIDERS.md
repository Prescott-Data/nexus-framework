## Supported Providers Registry

Purpose: Track which identity providers (IDPs) are supported by the broker, the exact endpoints we use, and any provider-specific notes. Update this file whenever a new provider profile is registered.

### How to add a provider (preferred: issuer-first with discovery)
1) Create an OAuth/OIDC app in the provider console and add the redirect URI: `BASE_URL + REDIRECT_PATH` (e.g., `http://localhost:8080/auth/callback` in dev).
2) Prepare the provider JSON using this template (OIDC):
```json
{
  "name": "<provider-name>",
  "issuer": "<issuer-base-url>",
  "client_id": "<client-id>",
  "client_secret": "<client-secret-value>",
  "scopes": ["openid","email"],
  "enable_discovery": true
}
```
Examples (recommended):

- Google (issuer-first, discovery preferred; URLs provided as optional overrides for clarity)
```json
{
  "name": "Google",
  "issuer": "https://accounts.google.com",
  "client_id": "<your-client-id>",
  "client_secret": "<your-client-secret>",
  "scopes": ["openid", "email", "profile"],
  "enable_discovery": true,
  "auth_url": "https://accounts.google.com/o/oauth2/v2/auth",
  "token_url": "https://oauth2.googleapis.com/token"
}
```

- Microsoft Entra ID (Azure AD) – common (multi-tenant + personal). In production you may prefer tenant-specific issuer; discovery still resolves endpoints.
```json
{
  "name": "azure-ad-common",
  "issuer": "https://login.microsoftonline.com/common/v2.0",
  "client_id": "<your-app-id>",
  "client_secret": "<your-client-secret>",
  "scopes": ["openid", "profile", "email", "offline_access", "User.Read"],
  "enable_discovery": true,
  "auth_url": "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
  "token_url": "https://login.microsoftonline.com/common/oauth2/v2.0/token"
}
```
3) Optional endpoint overrides (used only if you need to force static endpoints or for non-OIDC providers):
```json
{
  "endpoint_overrides": {
    "authorization_endpoint": "<authorize-endpoint>",
    "token_endpoint": "<token-endpoint>"
  }
}
```
4) Post it to the broker (wrap under `profile`):
```bash
jq -c '{profile: .}' path/to/provider.json \
| curl -s -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <api-key-if-required>" \
  -d @- | jq .
```
5) The response returns `provider_id`. Use it with `/auth/consent-spec`.

### Managing Providers

In addition to registering new providers, you can also describe, update, and delete them using the following API endpoints. All of these endpoints are protected and require an API key.

#### Describe a Provider

To get the full details of a specific provider, use the `GET /providers/{id}` endpoint.

```bash
curl -s -X GET http://localhost:8080/providers/<provider-id> \
  -H "X-API-Key: <api-key-if-required>" | jq .
```

#### Update a Provider

To update a provider's information, use the `PUT /providers/{id}` endpoint. The request body should contain the fields to be updated.

```bash
curl -s -X PUT http://localhost:8080/providers/<provider-id> \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <api-key-if-required>" \
  -d '{"name": "New Name", "client_id": "new-client-id"}' | jq .
```

#### Delete a Provider

To delete a provider, use the `DELETE /providers/{id}` endpoint. This is a "soft delete", meaning the provider will be marked as deleted but not removed from the database. This prevents issues with existing connections that rely on the provider.

```bash
curl -s -X DELETE http://localhost:8080/providers/<provider-id> \
  -H "X-API-Key: <api-key-if-required>"
```

### Verifying what’s installed

To see a list of all active providers, you can use the `GET /providers` endpoint.

```bash
curl -s http://localhost:8080/providers \
  -H "X-API-Key: <api-key-if-required>" | jq .
```

### Currently supported

#### Google
- Authorization endpoint: `https://accounts.google.com/o/oauth2/v2/auth`
- Token endpoint: `https://oauth2.googleapis.com/token`
- Issuer: `https://accounts.google.com`
- Default scopes: `openid`, `email` (more can be requested per flow)
- Special handling:
  - Do not request `offline_access` scope (Google doesn’t support it)
  - We add `access_type=offline` and `prompt=consent` query params to obtain refresh tokens

#### Microsoft Entra ID (Azure AD)
- Preferred issuer: `https://login.microsoftonline.com/<tenant-id>/v2.0`
- Typical scopes: `openid`, `email`, `profile`, `offline_access`, plus Graph scopes like `User.Read`
- Notes:
  - Use tenant-specific issuer in production; discovery will resolve endpoints.
  - `offline_access` is needed for refresh tokens.

### Operational notes
- Tokens are encrypted (AES-GCM) and stored; refresh is on-demand.
- OIDC discovery is preferred for endpoints and JWKS. If discovery is unavailable, static endpoints (if configured) are used for OAuth flows, but `id_token` verification will fail-closed.
- `auth_url`/`token_url` fields serve as optional overrides and documentation; when `enable_discovery=true` the broker prefers discovered endpoints at runtime.
- For Google, we remove `offline_access` from scopes and add `access_type=offline` and `prompt=consent`.
- For Microsoft/others, keep `offline_access` in scopes to obtain refresh tokens.

### Adding new providers (template section to copy)
#### <Provider Name>
- Authorization endpoint: `<authorize-endpoint>`
- Token endpoint: `<token-endpoint>`
- Typical scopes: `openid`, `email`, `offline_access` (if supported), plus any API scopes
- Provider-specific notes:
  - `<notes about offline_access, audience params, discovery, etc.>`

