## Supported Providers Registry

Purpose: Track which identity providers (IDPs) are supported by the broker, the exact endpoints we use, and any provider-specific notes. Update this file whenever a new provider profile is registered.

### How to add a provider
1) Create an OAuth app in the provider console and add the redirect URI: `BASE_URL + REDIRECT_PATH` (e.g., `http://localhost:8080/auth/callback` in dev).
2) Prepare the provider JSON using this template:
```json
{
  "name": "<provider-name>",
  "auth_url": "<authorize-endpoint>",
  "token_url": "<token-endpoint>",
  "client_id": "<client-id>",
  "client_secret": "<client-secret-value>",
  "scopes": ["openid","email"]
}
```
3) Post it to the broker (note: wrap under `profile` when posting):
```bash
jq -c '{profile: .}' path/to/provider.json \
| curl -s -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" -d @- | jq .
```
4) The response returns `provider_id`. Use it with `/auth/consent-spec`.

### Currently supported

#### Google
- Authorization endpoint: `https://accounts.google.com/o/oauth2/v2/auth`
- Token endpoint: `https://oauth2.googleapis.com/token`
- Default scopes: `openid`, `email` (more can be requested per flow)
- Special handling:
  - Do not request `offline_access` scope (Google doesn’t support it)
  - We add `access_type=offline` and `prompt=consent` query params to obtain refresh tokens

#### Microsoft Entra ID (Azure AD) – Multitenant + Personal (common)
- Authorization endpoint: `https://login.microsoftonline.com/common/oauth2/v2.0/authorize`
- Token endpoint: `https://login.microsoftonline.com/common/oauth2/v2.0/token`
- Typical scopes: `openid`, `email`, `profile`, `offline_access`, plus Graph scopes like `User.Read`
- Notes:
  - Using `{tenant}=common` enables both organizational and personal Microsoft accounts
  - Keep `offline_access` to receive refresh tokens
  - Some enterprise APIs may not apply to personal accounts

### Operational notes
- We treat tokens as opaque and store them encrypted (AES-GCM). Refresh is on-demand.
- For Google, we automatically remove `offline_access` from scopes but add the correct URL params.
- For non-Google IDPs (e.g., Microsoft), we keep `offline_access` in scopes.

### Verifying what’s installed
- To see registered providers, query the database (example):
```sql
SELECT id, name, auth_url, token_url, scopes FROM provider_profiles ORDER BY created_at DESC;
```
- Or re-run the POST and note the returned `id`.

### Adding new providers (template section to copy)
#### <Provider Name>
- Authorization endpoint: `<authorize-endpoint>`
- Token endpoint: `<token-endpoint>`
- Typical scopes: `openid`, `email`, `offline_access` (if supported), plus any API scopes
- Provider-specific notes:
  - `<notes about offline_access, audience params, discovery, etc.>`


