---
icon: material/file-code-outline
---

# Security-as-Code with the nexus CLI

`nexus` applies a GitOps workflow to Nexus provider configuration. Instead of making ad-hoc API calls to register and update providers, you declare the desired state in YAML, commit it to your repository, and use `nexus` to reconcile the live Broker against it.

This matters for Nexus specifically because the Broker holds refresh tokens and API keys for every provider in your workspace. An undocumented API call that misconfigures or deletes a provider can silently break every agent that depends on it, with no record of what changed or who made the change. Declarative management gives you git history, peer review, and an audit trail for every provider mutation.

The full flag and format reference is in the [CLI Reference](../reference/cli.md). This guide is the workflow.

---

## Installation

```bash
go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@latest
```

Or from a checkout, stamped with the framework version:

```bash
make cli
nexus-cli/nexus version
```

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `BROKER_BASE_URL` | `http://localhost:8080` | URL of the Nexus Broker |
| `API_KEY` | none | Broker API key |
| `NEXUS_SECRETS` | `env` | Default secret source (see [Secrets](#secrets)) |
| `NEXUS_SECRET_PREFIX` | none | Default `-secret-prefix` for Google Secret Manager |

---

## The provider manifest

Keep one directory in your infrastructure repository with one file per provider, named after it. The directory is the single source of truth for the target environment.

```
infra/providers/
  github.yaml
  google-workspace.yaml
  salesforce.yaml
```

```yaml
# infra/providers/google-workspace.yaml
name: google-workspace
auth_type: oauth2
client_id: ${NEXUS_PROVIDER_GOOGLE_WORKSPACE_CLIENT_ID}
client_secret: ${NEXUS_PROVIDER_GOOGLE_WORKSPACE_CLIENT_SECRET}
issuer: https://accounts.google.com
enable_discovery: true
scopes:
  - openid
  - email
  - profile
  - offline_access
```

```yaml
# infra/providers/github.yaml
name: github
auth_type: oauth2
client_id: ${NEXUS_PROVIDER_GITHUB_CLIENT_ID}
client_secret: ${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}
auth_url: https://github.com/login/oauth/authorize
token_url: https://github.com/login/oauth/access_token
api_base_url: https://api.github.com
enable_discovery: false
scopes:
  - read:user
  - user:email
```

Secrets never appear in a file: `${PLACEHOLDER}` references are resolved at run time from a [secret source](#secrets). A single `nexus-providers.yaml` with a `providers:` list works too, and is fine for a handful of providers; the directory form scales to hundreds and gives each provider its own git history.

The CLI refuses a directory in which a file's `name` does not match its file name, a name appears twice, a file is not a single provider mapping, or a provider has a field it does not know (a typo such as `scope:`). Those are the copy-paste mistakes that used to reach the Broker as a confusing `400`, or worse, register a provider with a field silently missing.

### Manifest fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Provider alias. Used as the reconciliation key. Must be unique. |
| `auth_type` | string | `oauth2`, `saml`, or a static type: `api_key`, `basic_auth`, `header`, `query_param`, `hmac_payload`, `aws_sigv4` |
| `client_id` | string | OAuth client ID |
| `client_secret` | string | OAuth client secret |
| `issuer` | string | OIDC issuer URL for auto-discovery |
| `auth_url` | string | Authorization endpoint (when not using discovery) |
| `token_url` | string | Token endpoint (when not using discovery) |
| `api_base_url` | string | Provider API root URL |
| `enable_discovery` | bool | Fetch endpoints from OIDC discovery document |
| `scopes` | list | Default scopes to request |
| `params` | map | Provider-specific extra parameters |
| `saml_*` | string | The four SAML fields, for `auth_type: saml` |

`client_secret` is write-only: the Broker never returns it. It returns `client_secret_fingerprint`, a truncated SHA-256 of the stored secret, and `plan` compares the manifest value's fingerprint against it, so a rotated secret in the manifest shows as drift and `apply` sends only that, while an unchanged one is left alone. A provider registered before 0.6.4 has no fingerprint until its next rotation or the Broker's next use of the secret, and is not compared until then. Placeholders are expanded inside string values only, including nested `params`, after the YAML is parsed; comments and keys are left alone. Every unresolved placeholder is reported by name before anything is sent to the Broker.

---

## Secrets

`-secrets` (or `NEXUS_SECRETS`) says where placeholder values come from. The manifest itself is the same whichever you choose, so a repository can move from one to another without editing a provider file.

### The environment

The default. Export the values and run:

```bash
export NEXUS_PROVIDER_GITHUB_CLIENT_ID=...
export NEXUS_PROVIDER_GITHUB_CLIENT_SECRET=...
nexus plan -file infra/providers
```

Fine on a workstation for a few providers. It does not scale, and a shell history or a process listing is one way secrets leak.

### A SOPS file in the repository

[SOPS](https://github.com/getsops/sops) encrypts a file to a key you control, such as a Google Cloud KMS key, so the ciphertext can be committed next to the provider files and only someone the key admits can read it. The CLI decrypts it in memory with the credentials the process already has; the plaintext is never written anywhere.

```bash
# One-time: a KMS key the operators can use
gcloud kms keyrings create nexus --location global --project my-project
gcloud kms keys create providers --keyring nexus --location global --purpose encryption --project my-project

# Write the plaintext (not committed), encrypt it, commit the ciphertext
cat > /tmp/secrets.yaml <<'EOF'
NEXUS_PROVIDER_GITHUB_CLIENT_ID: ...
NEXUS_PROVIDER_GITHUB_CLIENT_SECRET: ...
EOF
sops --encrypt --gcp-kms projects/my-project/locations/global/keyRings/nexus/cryptoKeys/providers \
  /tmp/secrets.yaml > infra/providers.enc.yaml
rm /tmp/secrets.yaml

nexus plan -file infra/providers -secrets sops:infra/providers.enc.yaml
```

To rotate a provider's secret later, `sops infra/providers.enc.yaml` opens the decrypted file in your editor and re-encrypts on save; the commit records that the secret changed without recording what it is, and `nexus plan` shows the provider as `~ UPDATE` with `client_secret`. YAML, JSON and dotenv files are accepted; values must be flat scalars keyed by placeholder name.

### Google Secret Manager

One secret per placeholder, read directly from Secret Manager with application default credentials:

```bash
nexus plan -file infra/providers -secrets gcp-secret-manager:my-project -secret-prefix oauth-
```

The secret id is derived from the placeholder: `${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}` with prefix `oauth-` reads `oauth-github-client-secret`. Only the placeholders the manifest uses are fetched, concurrently, so a directory of 150 providers resolves in seconds. Rotate by adding a new secret version:

```bash
printf '%s' "$NEW_SECRET" | gcloud secrets versions add oauth-github-client-secret --data-file=- --project my-project
nexus plan -file infra/providers -secrets gcp-secret-manager:my-project -secret-prefix oauth-
```

With any source, a placeholder the source does not hold falls back to the environment. The source wins when both have a value; the environment only supplies what the source omits.

---

## Commands

### validate

`validate` checks the manifest offline with the Broker's own registration rules and reports every problem with the file it is in. Placeholders count as present, so it runs in a pull request without any secrets:

```bash
nexus validate -file infra/providers
```

```
Read 149 providers from infra/providers (149 files)
✗ infra/providers/acme.yaml: acme: auth_url: missing required field
✗ infra/providers/acme.yaml: acme: token_url: missing required field
144 placeholders used (not resolved; pass -secrets to check them)

✗ 2 problem(s)
```

Add `-secrets` to also prove that every placeholder resolves, for instance before an apply from the operator's machine. `plan` and `apply` run the same validation first, so a manifest the Broker would reject never gets as far as a half-applied run.

### plan

`plan` fetches the current live state from the Broker, computes the diff against your manifest, and prints what would change without making any mutations:

```bash
nexus plan -file infra/providers -secrets sops:infra/providers.enc.yaml
```

Example output:

```
Read 3 providers from infra/providers (3 files)

--- Execution Plan ---
+ CREATE : github
~ UPDATE : google-workspace
    client_secret: *** → ***
= OK     : salesforce (no changes)
! ORPHAN : old-slack-provider (would be deleted if -prune was passed)

Plan complete. Run 'nexus apply' to perform these actions.
```

| Symbol | Meaning |
|---|---|
| `+` | Provider will be created |
| `~` | Provider will be updated, with the changed fields beneath it |
| `=` | Provider matches |
| `!` | Provider exists in the live state but not in the manifest (orphan) |
| `-` | Provider will be deleted (only shown when `-prune` is passed) |

### apply

`apply` executes the plan after an interactive confirmation prompt:

```bash
nexus apply -file infra/providers -secrets sops:infra/providers.enc.yaml
```

Pass `-prune` to also delete orphaned providers. Do not use `-prune` until you are confident your manifest is the complete desired state for the environment. Deleting a provider immediately breaks all connections that reference it.

| Flag | Default | Description |
|---|---|---|
| `-file` | `nexus-providers.yaml` | Manifest file or provider directory |
| `-secrets` | `$NEXUS_SECRETS`, else `env` | Secret source |
| `-prune` | `false` | Delete providers not present in the manifest |
| `-auto-approve` | `false` | Skip the confirmation, for scripted applies from a trusted environment |

---

## CI/CD integration

Run `nexus validate` on every pull request that touches the provider directory; it needs no secrets and no Broker. Run `nexus plan` as an informational check where the pipeline can reach the Broker and the secret source, so reviewers see what would change before merging. Apply manually from a trusted environment when you are ready to change the live state.

Automatic apply on merge is not recommended. `apply -auto-approve` exists for scripted applies from a trusted environment, such as a seed script run by an operator; it is not an invitation to apply from a merge hook. Provider configuration is live operational data that affects all agents in the workspace. A plan review step before apply prevents accidental provider deletions or misconfigurations from reaching production silently.

```yaml
# Example GitHub Actions snippet
- name: Install nexus
  run: go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@latest

- name: Validate providers
  run: nexus validate -file infra/providers

- name: Nexus plan
  env:
    BROKER_BASE_URL: ${{ secrets.BROKER_BASE_URL }}
    API_KEY: ${{ secrets.BROKER_API_KEY }}
  run: nexus plan -file infra/providers -secrets sops:infra/providers.enc.yaml
```

Every `apply` run generates audit log entries on the Broker, giving you a record of which providers were created, updated, or deleted and at what time. Combined with the git history of your manifest files, you have two independent audit trails for every provider change.
