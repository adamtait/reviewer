# SPDX-License-Identifier: MIT

output "url" {
  description = "Where users reach the service: the configuration's publicUrl."
  value       = local.public_url
}

output "image_repository" {
  description = "The Artifact Registry repository to push images to."
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"
}

output "secret_ids" {
  description = "The Secret Manager secret holding each environment variable, by variable name."
  value       = { for env, s in google_secret_manager_secret.env : env => s.secret_id }
}

output "service_account" {
  description = "The identity the container runs as."
  value       = google_service_account.runtime.email
}

output "resources" {
  description = "The container's memory and CPUs."
  value       = "${local.memory}, ${local.cpu} CPU"
}

output "secret_ids_tsv" {
  description = "secret_ids as lines of variable name, a tab and secret id, which deploy.sh reads without needing jq."
  value       = join("\n", [for env, s in google_secret_manager_secret.env : "${env}\t${s.secret_id}"])
}
