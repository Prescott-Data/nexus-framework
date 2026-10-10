variable "project_id" {
  description = "Project that hosts the broker's provider secrets. Set GCP_PROJECT_ID on the broker to the same value, or leave it unset on Cloud Run, GCE and GKE where the metadata server supplies it."
  type        = string
}

variable "broker_service_account" {
  description = "Email of the service account the nexus-broker runs as."
  type        = string
}

variable "secret_prefix" {
  description = "Prefix for secret IDs. Must equal the broker's GCP_SECRET_PREFIX. Letters, digits, '-' and '_' only."
  type        = string
  default     = "nexus-providers"

  validation {
    condition     = can(regex("^[A-Za-z0-9_-]+$", var.secret_prefix))
    error_message = "secret_prefix may only contain letters, digits, '-' and '_'."
  }
}

variable "enable_api" {
  description = "Enable secretmanager.googleapis.com on the project. Set false if another module already manages project services."
  type        = bool
  default     = true
}

variable "creator_role_id" {
  description = "ID for the custom role that carries secretmanager.secrets.create."
  type        = string
  default     = "nexusProviderSecretCreator"
}
