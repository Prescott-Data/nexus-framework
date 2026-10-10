# GCP Secret Manager backend for nexus-broker

Terraform for `SECRET_BACKEND=gcp-secret-manager`. It grants the broker's service account the least it needs to keep provider client secrets in Secret Manager: a custom role with only `secretmanager.secrets.create`, and a second custom role with only get, delete and version add/access/list/destroy, restricted by an IAM condition to secrets named `<prefix>-*`. Neither carries IAM-policy permissions.

```hcl
module "nexus_secret_backend" {
  source                 = "github.com/Prescott-Data/nexus-framework//deploy/terraform/gcp/secret-backend?ref=main"
  project_id             = var.project_id
  broker_service_account = google_service_account.broker.email
  secret_prefix          = "nexus-providers"
}

# Then on the broker:
#   SECRET_BACKEND=gcp-secret-manager
#   GCP_PROJECT_ID=<project_id>          (optional on Cloud Run, GCE and GKE)
#   GCP_SECRET_PREFIX=nexus-providers
```

The broker checks the backend at startup and refuses to boot if it cannot reach Secret Manager under the prefix, so a missing binding shows up in the first log line rather than at the first OAuth exchange.

See `docs/guides/provider-secret-backends.md` for how the backend behaves.
