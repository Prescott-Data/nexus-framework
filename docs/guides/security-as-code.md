# Security-as-Code with nexus-cli

`nexus-cli` is a command-line tool that applies a GitOps workflow to Nexus provider configuration. Instead of making ad-hoc API calls to register and update providers, you declare the desired state in a YAML manifest, commit it to your repository, and use `nexus-cli` to reconcile the live Broker against that manifest.

This matters for Nexus specifically because the Broker holds refresh tokens and API keys for every provider in your workspace. An undocumented API call that misconfigures or deletes a provider can silently break every agent that depends on it, with no record of what changed or who made the change. Declarative management gives you git history, peer review, and an audit trail for every provider mutation.

---

## Installation

Build from source within the repository:

```bash
cd nexus-cli
go build -o nexus-cli .
```

Or install directly with Go:

```bash
go install github.com/Prescott-Data/nexus-framework/nexus-cli@latest
```

---

## Configuration

`nexus-cli` is configured through environment variables:

| Variable | Default | Description |
|---|---|---|
| `BROKER_BASE_URL` | `http://localhost:8080` | URL of the Nexus Broker |
| `API_KEY` | none | API key for Broker authentication |

---

## The provider manifest

Create a file named `nexus-providers.yaml` and commit it to your infrastructure repository. This file is the single source of truth for all provider configurations in the target environment.

Environment variable references in the manifest are expanded at runtime, so secrets never appear in the file itself:

```yaml
providers:
  - name: google-workspace
    auth_type: oauth2
    client_id: "${GOOGLE_CLIENT_ID}"
    client_secret: "${GOOGLE_CLIENT_SECRET}"
    issuer: "https://accounts.google.com"
    enable_discovery: true
    scopes:
      - openid
      - email
      - profile
      - offline_access

  - name: github
    auth_type: oauth2
    client_id: "${GITHUB_CLIENT_ID}"
    client_secret: "${GITHUB_CLIENT_SECRET}"
    auth_url: "https://github.com/login/oauth/authorize"
    token_url: "https://github.com/login/oauth/access_token"
    api_base_url: "https://api.github.com"
    enable_discovery: false
    scopes:
      - read:user
      - user:email
```

### Manifest fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Provider alias. Used as the reconciliation key. Must be unique. |
| `auth_type` | string | `oauth2` or `api_key` |
| `client_id` | string | OAuth client ID |
| `client_secret` | string | OAuth client secret |
| `issuer` | string | OIDC issuer URL for auto-discovery |
| `auth_url` | string | Authorization endpoint (when not using discovery) |
| `token_url` | string | Token endpoint (when not using discovery) |
| `api_base_url` | string | Provider API root URL |
| `enable_discovery` | bool | Fetch endpoints from OIDC discovery document |
| `scopes` | list | Default scopes to request |
| `params` | map | Provider-specific extra parameters |

---

## Commands

### plan

`plan` fetches the current live state from the Broker, computes the diff against your manifest, and prints what would change without making any mutations:

```bash
nexus-cli plan
nexus-cli plan --file ./infra/nexus-providers.prod.yaml
```

Example output:

```
Read 2 providers from nexus-providers.yaml

--- Execution Plan ---
+ CREATE : github
~ UPDATE : google-workspace
! ORPHAN : old-slack-provider

Plan complete. Run 'nexus-cli apply' to perform these actions.
```

The symbols in the plan output:

| Symbol | Meaning |
|---|---|
| `+` | Provider will be created |
| `~` | Provider will be updated |
| `!` | Provider exists in the live state but not in the manifest (orphan) |
| `-` | Provider will be deleted (only shown when `--prune` is passed) |

### apply

`apply` executes the plan after an interactive confirmation prompt:

```bash
nexus-cli apply
```

Pass `--prune` to also delete orphaned providers. Do not use `--prune` until you are confident your manifest is the complete desired state for the environment. Deleting a provider immediately breaks all connections that reference it.

| Flag | Default | Description |
|---|---|---|
| `--file` | `nexus-providers.yaml` | Path to the manifest file |
| `--prune` | `false` | Delete providers not present in the manifest |

---

## CI/CD integration

Run `nexus-cli plan` as an informational check on pull requests so reviewers can see what would change before merging. Apply manually from a trusted environment when you are ready to change the live state.

Automatic apply on merge is not recommended. Provider configuration is live operational data that affects all agents in the workspace. A plan review step before apply prevents accidental provider deletions or misconfigurations from reaching production silently.

```yaml
# Example GitHub Actions snippet
- name: Nexus plan
  env:
    BROKER_BASE_URL: ${{ secrets.BROKER_BASE_URL }}
    API_KEY: ${{ secrets.BROKER_API_KEY }}
    GOOGLE_CLIENT_ID: ${{ secrets.GOOGLE_CLIENT_ID }}
    GOOGLE_CLIENT_SECRET: ${{ secrets.GOOGLE_CLIENT_SECRET }}
  run: ./nexus-cli plan
```

Every `apply` run generates audit log entries on the Broker, giving you a record of which providers were created, updated, or deleted and at what time. Combined with the git history of your manifest file, you have two independent audit trails for every provider change.
