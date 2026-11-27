# SPDX-License-Identifier: MIT

variable "project_id" {
  description = "The Google Cloud project to deploy into."
  type        = string
}

variable "region" {
  description = "The region for the Cloud Run service and the image repository."
  type        = string
  default     = "us-central1"
}

variable "service_name" {
  description = "The Cloud Run service's name. Every other resource is named after it, so two deployments can share a project."
  type        = string
  default     = "review-workspace"

  validation {
    condition     = can(regex("^[a-z][-a-z0-9]{0,38}[a-z0-9]$", var.service_name))
    error_message = "service_name must be 2 to 40 lowercase letters, digits or hyphens, start with a letter and not end with a hyphen."
  }
}

variable "config_file" {
  description = "Path to the service's YAML configuration. See examples/workspace.config.yaml. Its listen and publicUrl are replaced; see main.tf."
  type        = string
}

variable "image" {
  description = "The container image to run, preferably pinned by digest. Required for the service; deploy.sh builds one and passes it."
  type        = string
  default     = null
}

variable "public_url" {
  description = "Where users reach the service, if not the service's own run.app address: for example a custom domain you map yourself. Must be https."
  type        = string
  default     = ""

  validation {
    condition     = var.public_url == "" || can(regex("^https://[^/]+$", var.public_url))
    error_message = "public_url must be https://<host>, with no path or trailing slash."
  }
}

variable "secret_versions" {
  description = "Secret Manager version to use for each environment variable, by variable name. A variable not listed uses \"latest\". deploy.sh pins each to its newest enabled version, so a key added since the last deploy starts a new revision."
  type        = map(string)
  default     = {}
}

variable "retain_secret_envs" {
  description = "Environment variables whose secrets are kept even if the configuration no longer names them. deploy.sh sets it, for its first, targeted apply only, so that a secret is never deleted while the running service reads it. Leave it empty otherwise."
  type        = list(string)
  default     = []
}

variable "memory" {
  description = "Container memory, such as \"2Gi\". Empty means sized from the configuration's limits by the rule in docs/workspace.md."
  type        = string
  default     = ""

  validation {
    condition     = can(regex("^([0-9]+(Mi|Gi))?$", var.memory))
    error_message = "memory must be empty, or a whole number of Mi or Gi, such as \"2Gi\"."
  }
}

variable "cpu" {
  description = "Container CPUs, such as \"2\". Empty means the least Cloud Run allows for the memory."
  type        = string
  default     = ""

  validation {
    # At least one: below that Cloud Run serves one request at a time.
    condition     = contains(["", "1", "2", "4", "6", "8"], var.cpu)
    error_message = "cpu must be empty, or 1, 2, 4, 6 or 8."
  }
}

variable "min_instances" {
  description = "0 lets the container stop when idle, and the next request reads from GitHub again. 1 keeps memory warm, at a monthly cost."
  type        = number
  default     = 0

  validation {
    condition     = contains([0, 1], var.min_instances)
    error_message = "min_instances must be 0 or 1. The service runs on one instance at most."
  }
}
