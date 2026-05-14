# CLI Reference

`nexus-cli` is a command-line tool for managing Nexus provider configuration declaratively. It reads a YAML manifest, compares it against the live Broker state, and applies changes.

## Install

```bash
# Build from source
cd nexus-cli && go build -o nexus-cli .

# Install with Go
go install github.com/Prescott-Data/nexus-framework/nexus-cli@latest
```

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `BROKER_BASE_URL` | yes | Base URL of the Broker, e.g. `http://localhost:8080` |
| `API_KEY` | yes | Broker API key — same value as the Broker's `API_KEY` env var |

## Commands

### `plan`

Computes the diff between the manifest and the live Broker state. Prints the actions that would be taken. Makes no changes.

```bash
nexus-cli plan [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-file` | `nexus-providers.yaml` | Path to the providers manifest file |
| `-prune` | `false` | Include deletion of providers not in the manifest |

**Output:**

```
+ CREATE : google-workspace
~ UPDATE : salesforce
  client_secret: [redacted] → [redacted]
  scopes: ["crm:read"] → ["crm:read","crm:write"]
! ORPHAN : old-provider (would be deleted if --prune was passed)

Plan complete. Run 'nexus-cli apply' to perform these actions.
```

`client_secret` and `client_id` values are always masked in plan output.

---

### `apply`

Applies the manifest against the live Broker. Creates new providers, updates drifted fields, and optionally deletes orphans.

```bash
nexus-cli apply [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-file` | `nexus-providers.yaml` | Path to the providers manifest file |
| `-prune` | `false` | Delete providers that exist in the Broker but not in the manifest |

`apply` runs `plan` internally first and prints the same diff before executing. Use `-prune` with care — it permanently deletes providers and orphans every active connection tied to them.

---

## Manifest format

The manifest is a YAML file declaring the desired state of all providers.

```yaml
providers:
  - name: google-workspace
    auth_type: oauth2
    client_id: ${GOOGLE_CLIENT_ID}
    client_secret: ${GOOGLE_CLIENT_SECRET}
    issuer: https://accounts.google.com
    enable_discovery: true
    scopes:
      - openid
      - email
      - profile
      - offline_access

  - name: salesforce
    auth_type: oauth2
    client_id: ${SF_CLIENT_ID}
    client_secret: ${SF_CLIENT_SECRET}
    auth_url: https://login.salesforce.com/services/oauth2/authorize
    token_url: https://login.salesforce.com/services/oauth2/token
    scopes:
      - crm:contacts:read
    params:
      skip_scope_on_exchange: true

  - name: internal-api
    auth_type: api_key
    params:
      credential_schema:
        type: object
        required: [api_key]
        properties:
          api_key:
            type: string
            title: API Key
```

### Provider fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique slug — used as the identifier in connection requests |
| `auth_type` | string | yes | `oauth2`, `api_key`, `basic_auth`, `aws_sigv4`, `query_param`, `hmac_payload` |
| `client_id` | string | OAuth2 only | OAuth2 client ID |
| `client_secret` | string | OAuth2 only | OAuth2 client secret |
| `auth_url` | string | OAuth2 (no OIDC) | Authorization endpoint |
| `token_url` | string | OAuth2 (no OIDC) | Token endpoint |
| `issuer` | string | OIDC only | Issuer URL — enables OIDC discovery |
| `enable_discovery` | bool | OIDC only | Must be `true` when using `issuer` |
| `scopes` | []string | OAuth2 | Default scopes for this provider |
| `auth_header` | string | rarely | Auth header style, e.g. `client_secret_basic` for Twitter/X |
| `api_base_url` | string | no | Base URL for the provider's API — informational |
| `params` | object | varies | Strategy-specific and provider-specific parameters |

### Environment variable substitution

The CLI expands `${VAR}` references in the manifest at runtime using the process environment. Use this to keep secrets out of the manifest file:

```bash
export GOOGLE_CLIENT_ID=my-client-id
export GOOGLE_CLIENT_SECRET=my-client-secret
nexus-cli apply -file nexus-providers.yaml
```

## Drift detection

`nexus-cli` computes drift by comparing each field in the manifest against the live Broker response. Fields with semantic equivalence (empty array vs null) are not flagged as drift. Secret fields (`client_id`, `client_secret`) are always included in the update payload when any drift is detected on the provider — they cannot be compared because the Broker does not return secret values in GET responses.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success — no errors |
| `1` | Error — manifest read failure, API error, or unrecoverable drift |
