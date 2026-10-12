---
icon: material/console
---

# CLI Reference

`nexus` manages Nexus provider configuration declaratively. It reads a manifest (one file, or a directory of provider files), resolves the secrets it references, checks it against the Broker's rules, compares it with the live Broker state, and applies the difference.

## Install

```bash
# With Go (binary is named nexus)
go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@latest

# From a checkout, stamped with the framework version
make cli            # writes nexus-cli/nexus
nexus-cli/nexus version
```

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `BROKER_BASE_URL` | `plan`, `apply` | Base URL of the Broker, e.g. `http://localhost:8080` |
| `API_KEY` | `plan`, `apply` | Broker API key, the Broker's `API_KEY` |
| `NEXUS_SECRETS` | no | Default for `-secrets` |
| `NEXUS_SECRET_PREFIX` | no | Default for `-secret-prefix` |

## Common flags

Every command that reads a manifest takes these.

| Flag | Default | Description |
|---|---|---|
| `-file` | `nexus-providers.yaml` | A manifest file, or a directory of one provider per `<name>.yaml` (see [Manifest format](#manifest-format)) |
| `-secrets` | `plan`, `apply`: `$NEXUS_SECRETS`, else `env`. `validate`: unset, so placeholders are only listed unless the flag is given | Where `${PLACEHOLDER}` values come from: `env`, `sops:<path>` or `gcp-secret-manager:<project>` (see [Secret sources](#secret-sources)) |
| `-secret-prefix` | `$NEXUS_SECRET_PREFIX` | `gcp-secret-manager` only: prefix for derived secret ids |

## Commands

### `validate`

Checks the manifest offline, with the rules the Broker applies on registration, and prints every problem attributed to its file. Makes no network calls unless `-secrets` is given.

```bash
nexus validate -file providers/
nexus validate -file providers/ -secrets gcp-secret-manager:my-project -secret-prefix oauth-
```

Checked for every provider: `name` is present and matches `^[a-z0-9-]+$`; `auth_type` is `oauth2` (or empty), `saml`, or one of the static types `api_key`, `basic_auth`, `header`, `query_param`, `hmac_payload`, `aws_sigv4`; an `oauth2` provider has `client_id` and `client_secret`, and either `enable_discovery: true` with an `issuer` or both `auth_url` and `token_url`; a `saml` provider has all four `saml_*` fields, its `saml_idp_sso_url` is an http(s) URL with a host and its `saml_idp_x509_cert` is a CERTIFICATE PEM block or base64 DER that parses as X.509 (a field that is still a placeholder is left to the Broker); no name is defined twice. Unknown fields, a root that is not a `providers` list and multi-document files are rejected while reading, with line numbers. In directory mode, a file name that does not match its provider's `name` is an error before validation starts.

Placeholders count as present values, so `validate` passes in a pull request that cannot see any secrets. Pass `-secrets` to resolve every placeholder the manifest uses and run the same rules on the expanded manifest, exactly what `plan` would send: a missing placeholder, a secret that resolves to an empty string or a SAML certificate that resolves to something that is not one all fail. Values are never printed.

```
Read 149 providers from providers/ (149 files)
✗ providers/broken.yaml: broken: client_secret: missing required field
144 placeholders used (not resolved; pass -secrets to check them)

✗ 1 problem(s)
```

Exit code `0` when the manifest is valid, `1` otherwise.

---

### `plan`

Resolves secrets, validates, fetches the live state and prints what `apply` would do. Makes no changes.

```bash
nexus plan [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-prune` | `false` | Also list providers on the Broker that the manifest does not mention as deletions |

```
Read 3 providers from providers/ (3 files)

--- Execution Plan ---
+ CREATE : google-workspace
= OK     : github (no changes)
~ UPDATE : salesforce
    client_secret: *** → ***
    scopes: [crm:read] → [crm:read, crm:write]
! ORPHAN : old-provider (would be deleted if -prune was passed)

Plan complete. Run 'nexus apply' to perform these actions.
```

`client_secret` and `client_id` are always masked, and so is any field that held a placeholder in the manifest, whatever its name: a `params` map with `webhook_secret: ${WEBHOOK_SECRET}` prints as `params: *** → ***`. Providers are listed in manifest order, orphans in name order.

---

### `apply`

Runs the plan, asks for confirmation, then creates new providers, patches drifted fields and, with `-prune`, deletes orphans.

```bash
nexus apply [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-prune` | `false` | Delete providers that exist on the Broker but not in the manifest |
| `-auto-approve` | `false` | Skip the confirmation. For scripted applies from a trusted environment, not for apply-on-merge pipelines. |

Use `-prune` with care: deleting a provider orphans every connection tied to it.

---

### `version`

Prints the CLI version: the module version for a `go install ...@vX.Y.Z` binary, the framework version for `make cli`, `dev` for a plain `go build` in a checkout.

---

## Manifest format

### One file

A YAML file with a top-level `providers` list:

```yaml
providers:
  - name: google-workspace
    auth_type: oauth2
    client_id: ${GOOGLE_CLIENT_ID}
    client_secret: ${GOOGLE_CLIENT_SECRET}
    issuer: https://accounts.google.com
    enable_discovery: true
    scopes: [openid, email, profile, offline_access]

  - name: salesforce
    auth_type: oauth2
    client_id: ${SF_CLIENT_ID}
    client_secret: ${SF_CLIENT_SECRET}
    auth_url: https://login.salesforce.com/services/oauth2/authorize
    token_url: https://login.salesforce.com/services/oauth2/token
    scopes: [crm:contacts:read]
    params:
      skip_scope_on_exchange: true

  - name: internal-api
    auth_type: api_key
    params:
      credential_schema:
        type: object
        required: [api_key]
        properties:
          api_key: {type: string, title: API Key}
```

### A directory

Point `-file` at a directory and each `<name>.yaml` (or `.yml`) in it is one provider: the same mapping as a list item above, without the `providers:` wrapper. The split is for people; the CLI sees one configuration, like Terraform with a directory of `.tf` files.

```
providers/
  github.yaml
  google-workspace.yaml
  salesforce.yaml
```

```yaml
# providers/github.yaml
name: github
auth_type: oauth2
client_id: ${NEXUS_PROVIDER_GITHUB_CLIENT_ID}
client_secret: ${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}
auth_url: https://github.com/login/oauth/authorize
token_url: https://github.com/login/oauth/access_token
scopes: [read:user, user:email]
```

Rules, all enforced before anything is sent: the file's `name` must equal the file name without its extension; a name may appear in one file only; a file must hold exactly one YAML document with a single provider mapping, not a `providers:` list; a key that is not a provider field (a typo such as `scope:` for `scopes:`) is an error with its line number, since decoding would otherwise drop it silently. Files are read in name order. Hidden files and anything that is not `.yaml`/`.yml` are ignored, so a `README.md` can live alongside.

A single-file manifest is held to the same standard: its root must be a mapping whose only key is `providers`, holding a list of mappings. A typo such as `provider:` or an empty file is an error rather than a manifest with zero providers, because `apply -prune` would treat zero providers as an instruction to delete everything. An explicit `providers: []` is accepted as a deliberate empty state.

### Provider fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique slug, lowercase letters, digits and hyphens; the reconciliation key |
| `description` | string | no | Shown in provider listings and the Gateway's provider catalogue. Omitted: left as the Broker has it; `""`: cleared |
| `category` | string | no | Grouping label for provider listings, e.g. `crm`. Same omitted/empty rule |
| `auth_type` | string | yes | `oauth2`, `saml`, `api_key`, `basic_auth`, `header`, `query_param`, `hmac_payload`, `aws_sigv4` |
| `client_id` | string | OAuth2 | OAuth2 client ID |
| `client_secret` | string | OAuth2 | OAuth2 client secret |
| `auth_url` | string | OAuth2 (no discovery) | Authorization endpoint |
| `token_url` | string | OAuth2 (no discovery) | Token endpoint |
| `issuer` | string | OIDC | Issuer URL; set `enable_discovery: true` with it |
| `enable_discovery` | bool | OIDC | Fetch endpoints from the issuer's discovery document |
| `scopes` | []string | OAuth2 | Default scopes for this provider |
| `auth_header` | string | rarely | Token-endpoint client authentication, e.g. `client_secret_basic` for X |
| `api_base_url` | string | no | Base URL of the provider's API |
| `user_info_endpoint` | string | no | Path used to verify a captured credential |
| `params` | object | varies | Strategy-specific and provider-specific parameters |
| `saml_idp_entity_id`, `saml_idp_sso_url`, `saml_idp_x509_cert`, `saml_sp_entity_id` | string | SAML | All four are required for `auth_type: saml` |

### Placeholders

`${NAME}` (or `$NAME`) inside a string value is replaced at run time; keys and comments are never expanded, so a `$` in a comment is not a placeholder. A bare placeholder keeps the type its value implies (`skip_scope_on_exchange: ${SKIP}` with `SKIP=true` is a boolean); a quoted one is a string. Every placeholder that cannot be resolved is reported by name before anything is sent.

## Secret sources

`-secrets` names where placeholder values come from. Whatever the source, a placeholder it does not hold falls back to the process environment. The source wins when both have a value; the environment only supplies what the source omits, which is useful for a placeholder that is deliberately kept out of the source.

| Source | Where values live | Authentication |
|---|---|---|
| `env` (default) | The process environment: `export GITHUB_CLIENT_SECRET=...` | none |
| `sops:<path>` | A [SOPS](https://github.com/getsops/sops)-encrypted file: YAML or JSON with a flat `PLACEHOLDER: value` mapping, or dotenv `PLACEHOLDER=value` lines. Decrypted in memory; the plaintext never touches disk. | Whatever key the file was encrypted to and this process can reach: Google Cloud KMS or AWS KMS through the usual default credentials, age, PGP |
| `gcp-secret-manager:<project>` | One Secret Manager secret per placeholder, latest enabled version. Only the placeholders the manifest uses are read, concurrently. | Application default credentials with `secretmanager.versions.access` on the project |

**Secret id derivation for `gcp-secret-manager`.** Drop a leading `NEXUS_PROVIDER_`, lower-case, replace `_` with `-`, prepend `-secret-prefix`:

| Placeholder | `-secret-prefix` | Secret id |
|---|---|---|
| `${NEXUS_PROVIDER_TWITTER_CLIENT_ID}` | `oauth-` | `oauth-twitter-client-id` |
| `${NEXUS_PROVIDER_TWITTER_ADS_CLIENT_SECRET}` | `oauth-` | `oauth-twitter-ads-client-secret` |
| `${GITHUB_CLIENT_SECRET}` | (none) | `github-client-secret` |

A secret that does not exist is an unresolved placeholder, reported with the others; a permission or transport error stops the run.

**SOPS example.** Encrypt once to a Cloud KMS key, commit the ciphertext, and every `plan` decrypts it in memory:

```bash
# secrets.yaml (plaintext, never committed)
# NEXUS_PROVIDER_GITHUB_CLIENT_ID: ...
# NEXUS_PROVIDER_GITHUB_CLIENT_SECRET: ...
sops --encrypt --gcp-kms projects/P/locations/global/keyRings/nexus/cryptoKeys/providers \
  secrets.yaml > secrets.enc.yaml

nexus plan -file providers/ -secrets sops:secrets.enc.yaml
```

The `sops` key in a decrypted YAML or JSON file (its metadata) is ignored.

## Drift detection

Each manifest field is compared with the live Broker value; an empty list or string against a missing value is not drift. `client_secret` is never returned by the Broker, so it is compared by fingerprint: the Broker returns `client_secret_fingerprint` (the first 16 hex characters of the secret's SHA-256) and `plan` hashes the manifest value. A changed secret is drift and is sent; an unchanged one is left out of the update. A provider with no fingerprint yet (registered before 0.6.4 and not since rotated or used) is not compared and its secret is not sent. `client_id` is returned and compared like any other field.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or `plan` with nothing to do, or `apply` cancelled at the prompt |
| `1` | Manifest unreadable or invalid, unresolved placeholders, Broker error, or an apply step failed |
| `2` | Usage error: unknown command or flag |
