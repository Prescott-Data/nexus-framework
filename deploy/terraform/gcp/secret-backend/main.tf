# Grants a nexus-broker service account exactly what SECRET_BACKEND=gcp-secret-manager
# needs, and nothing it does not:
#
#   - secretmanager.secrets.create on the project. Create is authorised against
#     the project, not the secret, so it cannot be scoped by name. The custom
#     role below carries only that one permission; it cannot read anything.
#   - Secret Manager admin on secrets whose name starts with the prefix. This
#     covers add-version, access, destroy-version and delete for the broker's
#     own secrets. Secrets outside the prefix (your ENCRYPTION_KEY, database
#     URL, and so on) stay out of reach.
#
# Usage from your own Terraform:
#
#   module "nexus_secret_backend" {
#     source                 = "github.com/Prescott-Data/nexus-framework//deploy/terraform/gcp/secret-backend?ref=main"
#     project_id             = "my-project"
#     broker_service_account = google_service_account.broker.email
#     secret_prefix          = "nexus-providers"   # must equal GCP_SECRET_PREFIX
#   }

terraform {
  required_version = ">= 1.5"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0"
    }
  }
}

data "google_project" "this" {
  project_id = var.project_id
}

resource "google_project_service" "secretmanager" {
  count = var.enable_api ? 1 : 0

  project            = var.project_id
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_iam_custom_role" "secret_creator" {
  project     = var.project_id
  role_id     = var.creator_role_id
  title       = "Nexus provider secret creator"
  description = "Lets nexus-broker create Secret Manager secrets. Read and write on existing secrets are granted separately, scoped by name prefix."
  permissions = ["secretmanager.secrets.create"]
}

resource "google_project_iam_member" "secret_creator" {
  project = var.project_id
  role    = google_project_iam_custom_role.secret_creator.id
  member  = "serviceAccount:${var.broker_service_account}"

  depends_on = [google_project_service.secretmanager]
}

resource "google_project_iam_member" "prefixed_secret_admin" {
  project = var.project_id
  role    = "roles/secretmanager.admin"
  member  = "serviceAccount:${var.broker_service_account}"

  condition {
    title       = "nexus-broker provider secrets only"
    description = "Secrets named ${var.secret_prefix}-<provider uuid>, created by nexus-broker."
    expression  = "resource.name.startsWith(\"projects/${data.google_project.this.number}/secrets/${var.secret_prefix}-\")"
  }

  depends_on = [google_project_service.secretmanager]
}
