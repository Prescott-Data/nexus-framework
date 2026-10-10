---
icon: material/safe
---

# Provider Secret Backends

The Broker stores two kinds of secret, and they live in different places.

| Secret | Where it lives | Protected by |
|---|---|---|
| Tokens and captured credentials for a connection | `tokens.encrypted_data` in Postgres | AES-256-GCM under `ENCRYPTION_KEY`; the database only ever sees ciphertext |
| An OAuth2 provider's `client_secret` | `provider_profiles.client_secret` in Postgres by default, or an external **secret backend** | The backend's own access control |

By default the provider's client secret sits in the same row as its metadata. That is fine for a single-team deployment, but many environments require application credentials to live only in an approved secret manager, and nobody wants client secrets turning up in a database dump. `SECRET_BACKEND` moves them out.

## Configuration

| Variable | Required | Description |
|---|---|---|
| `SECRET_BACKEND` | No | `internal` (default) keeps client secrets in Postgres. `gcp-secret-manager` stores them in Google Cloud Secret Manager. |
| `GCP_PROJECT_ID` | Conditional | Project that holds the secrets. Optional on Cloud Run, GCE and GKE, where the metadata server supplies it. Required elsewhere. |
| `GCP_SECRET_PREFIX` | No | Prefix for secret IDs. Default `nexus-providers`. Letters, digits, `-` and `_` only. |

The Broker authenticates to Secret Manager with Application Default Credentials: the attached service account on Google Cloud, or `gcloud auth application-default login` on a workstation.

## What changes with an external backend

- `POST /providers` writes the provider row with `client_secret` set to NULL and stores the secret as `<prefix>-<provider id>` in the backend. If the backend write fails, the row is removed again and the request fails, so there is never a provider without its secret.
- `PATCH /providers/{id}` with `client_secret` rotates the secret in the backend. Earlier versions are destroyed, so only the current value is readable and billed.
- `PUT /providers/{id}` that omits `client_secret` leaves the stored secret unchanged. Clearing a secret is done by deleting the provider.
- `DELETE /providers/...` deletes the secret from the backend as well as soft-deleting the row.
- The OAuth exchange, token refresh, revocation and the provider health probe read the secret from the backend when they need it. It is never returned by the API.
- If the backend has no secret for a provider (removed out of band), the provider still lists and can be inspected, but the exchange and refresh fail with `provider_secret_missing`, revocation destroys the credential locally only, and the health probe reports the provider `unhealthy` with the reason. An empty secret is never sent to the provider. To recover, set a new secret in place with `PATCH /providers/{id}` and a `client_secret`, or delete the provider and register it again; registering under the same name while the provider exists is rejected as a duplicate.
- Secrets are keyed by provider ID, so renaming a provider never orphans its secret.

Existing rows that still carry a column value keep working after you switch backends: the Broker reads the column when it is set and the backend only when it is NULL. New and updated providers go to the backend. Nothing moves automatically.

Switching back from an external backend to `internal` is not transparent: providers registered while the backend was active have a NULL column, and the internal backend flags them as missing their secret rather than serving an empty one. Rotate each with `PATCH` to store the secret in the column, or keep the backend configured. Under the internal backend the Broker cannot delete Secret Manager secrets, so after rotating or deleting such a provider remove its old secret (`<prefix>-<provider id>`) yourself; cleaner is to delete the providers you intend to drop while the GCP backend is still configured.

## Startup check

With an external backend the Broker verifies at boot that it can do everything a registration does: it writes, reads and deletes a probe secret under the prefix (a fresh random id per start, removed on every exit path, alive for under a second) and refuses to start otherwise. A missing permission therefore shows up in the first log lines rather than on the first provider registration. The first log lines say which backend is active:

```
Provider client secrets: gcp-secret-manager (project my-project, prefix nexus-providers)
```

## Permissions on Google Cloud

The Broker's service account needs `secretmanager.secrets.create` on the project, and on secrets under the prefix only get, delete and version add/access/list/destroy. Creating a secret is authorised against the project, so it cannot be restricted by name; the per-secret grant can be, with an IAM condition on `resource.name`, and it carries no IAM-policy permissions. The Terraform module at [`deploy/terraform/gcp/secret-backend`](https://github.com/Prescott-Data/nexus-framework/tree/main/deploy/terraform/gcp/secret-backend) sets up exactly that:

```hcl
module "nexus_secret_backend" {
  source                 = "github.com/Prescott-Data/nexus-framework//deploy/terraform/gcp/secret-backend?ref=main"
  project_id             = "my-project"
  broker_service_account = google_service_account.broker.email
  secret_prefix          = "nexus-providers"
}
```

Secrets the Broker did not create, such as the ones holding `ENCRYPTION_KEY` or the database URL, stay unreadable to it.

## Cost

Secret Manager bills per active secret version per month and per ten thousand access operations. The Broker keeps one enabled version per provider and reads it only during OAuth exchanges, refreshes, revocations and the periodic health probe. For a deployment with tens of providers this is well under a dollar a month.

## Other backends

`SECRET_BACKEND` is a small interface (`pkg/vault/providers.Backend`: `Get`, `Set`, `Delete`, `Ping`). HashiCorp Vault and AWS Secrets Manager are tracked in [#63](https://github.com/Prescott-Data/nexus-framework/issues/63).
