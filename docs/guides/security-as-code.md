# Security-as-Code: Declarative Provider Management

The **`nexus-cli`** tool brings a GitOps-compatible, Terraform-style workflow to managing your Nexus provider configurations. Instead of managing providers through direct API calls (which leave no version history and are impossible to review), you declare your desired state in a YAML manifest, commit it to your repository, and let `nexus-cli` reconcile the live Broker against that source of truth.

!!! tip "Why this matters"
    Nexus holds Refresh Tokens and API Keys for every provider a workspace connects to — it is critical infrastructure. Without declarative management, a single bad API call can silently break all agents that depend on a provider, with no git history to recover from.

---

## How It Works

`nexus-cli` follows a **plan → confirm → apply** workflow:

1. **Fetches** the current live state from `GET /providers`.
2. **Diffs** it against your `nexus-providers.yaml` manifest.
3. **Prints** a human-readable plan showing creates, updates, and orphaned providers.
4. **Applies** the changes only after you confirm with `yes` (or non-interactively in CI).

---

## Installation

Build from source within the repository:

```bash
cd nexus-cli
go build -o nexus-cli .
```

Or install directly:

```bash
go install github.com/Prescott-Data/nexus-framework/nexus-cli@latest
```

---

## Configuration

`nexus-cli` is configured via environment variables:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `BROKER_BASE_URL` | Base URL of the Nexus Broker | `http://localhost:8080` |
| `API_KEY` | API key for Broker authentication | *(none)* |

---

## The Provider Manifest

Create a `nexus-providers.yaml` file and **commit it to your GitOps repository**. This file is your single source of truth for all provider configurations.

Environment variables are expanded at runtime, so secrets never need to be hardcoded.

```yaml title="nexus-providers.yaml"
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

### Manifest Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `name` | string | Unique provider name (used as the reconciliation key) |
| `auth_type` | string | `oauth2` or `api_key` |
| `client_id` | string | OAuth client ID |
| `client_secret` | string | OAuth client secret |
| `issuer` | string | OIDC issuer URL for auto-discovery |
| `auth_url` | string | Authorization endpoint (if not using discovery) |
| `token_url` | string | Token endpoint (if not using discovery) |
| `api_base_url` | string | Provider API root URL |
| `enable_discovery` | bool | Use OIDC discovery if `true` |
| `scopes` | list | Default scopes to request |
| `params` | map | Provider-specific extra parameters |

---

## Commands

### `plan` — Preview Changes

Show what would change without making any mutations:

```bash
nexus-cli plan
# Or with a custom manifest path:
nexus-cli plan --file ./path/to/nexus-providers.yaml
```

**Example output:**

```
Read 2 providers from nexus-providers.yaml

--- Execution Plan ---
+ CREATE : github
~ UPDATE : google-workspace
! ORPHAN : old-slack-provider (would be deleted if --prune was passed)

Plan complete. Run 'nexus-cli apply' to perform these actions.
```

The symbols mean:

| Symbol | Action |
| :--- | :--- |
| `+` | Provider will be created |
| `~` | Provider will be updated |
| `-` | Provider will be deleted (only shown with `--prune`) |
| `!` | Provider exists in live state but not in manifest (orphan) |

### `apply` — Apply Changes

Apply the manifest, with an interactive confirmation prompt:

```bash
nexus-cli apply
```

```
Read 2 providers from nexus-providers.yaml

--- Execution Plan ---
+ CREATE : github
~ UPDATE : google-workspace

Do you want to perform these actions?
  Nexus will perform the actions described above.
  Only 'yes' will be accepted to approve.

  Enter a value: yes

--- Applying Changes ---
Creating github... OK
Updating google-workspace... OK
```

#### Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--file` | `nexus-providers.yaml` | Path to the manifest file |
| `--prune` | `false` | Also delete providers in live state not in the manifest |

!!! warning "Using `--prune`"
    The `--prune` flag will **delete** providers that exist in the Broker but are absent from your manifest. Only use this when you are certain your manifest is the complete desired state. Any agents depending on a pruned provider will immediately lose their connections.

---

## CI/CD Integration (Optional)

`nexus-cli` is a standalone binary — you can run it from your laptop, a bastion host, or a CI pipeline. If you want to integrate it into your own CI/CD, here's a recommended pattern:

- **On pull requests**: run `nexus-cli plan` as an informational check so reviewers can see what would change.
- **Apply manually**: use a `workflow_dispatch` trigger or run `nexus-cli apply` from a trusted environment when you're ready.

> **Note:** Auto-applying on merge is discouraged. Provider configurations are live operational data — you should always review a plan before applying.

### Example GitHub Actions Snippet

```yaml
# Add this to your internal repo's workflow — not the open-source framework repo.
- name: Plan
  env:
    BROKER_BASE_URL: ${{ secrets.BROKER_BASE_URL }}
    API_KEY: ${{ secrets.BROKER_API_KEY }}
    # Add all env vars referenced in your manifest
  run: ./nexus-cli plan
```

### Required Environment Variables

| Variable | Description |
| :--- | :--- |
| `BROKER_BASE_URL` | URL of your target Nexus Broker (staging, prod, etc.) |
| `API_KEY` | API key for Broker authentication |
| `*_CLIENT_ID` / `*_CLIENT_SECRET` | Any provider credentials referenced via `${...}` in your manifest |

---

## Best Practices

1. **Treat `nexus-providers.yaml` as infrastructure code** — require PR reviews for all changes.
2. **Never hardcode secrets** — always use `${ENV_VAR}` expansion and inject via CI secrets.
3. **Start without `--prune`** — let orphans accumulate warnings first so you can audit them intentionally before deletion.
4. **One manifest per environment** — keep a `nexus-providers.prod.yaml` and `nexus-providers.staging.yaml` and set `BROKER_BASE_URL` accordingly in each CI environment.
5. **All mutations are audited** — every create, update, or delete applied by `nexus-cli` is recorded in the [Audit Log](../reference/audit-log.md).
