output "secret_prefix" {
  description = "Value to set as GCP_SECRET_PREFIX on the broker."
  value       = var.secret_prefix
}

output "broker_env" {
  description = "Environment variables that turn the backend on for the broker."
  value = {
    SECRET_BACKEND    = "gcp-secret-manager"
    GCP_PROJECT_ID    = var.project_id
    GCP_SECRET_PREFIX = var.secret_prefix
  }
}
