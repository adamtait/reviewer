# SPDX-License-Identifier: MIT
#
# The review workspace (cmd/workspace) on Cloud Run, in any project. What each
# setting is for is in docs/workspace.md, "Deploying on Cloud Run"; this file
# carries those settings out and says why only where the guide does not.
#
# Terraform creates the places secrets live and never their values: no key
# passes through Terraform's state. deploy.sh puts the values in, or see
# deploy/gcp/README.md for the gcloud commands.

data "google_project" "this" {}

locals {
  config = yamldecode(file(var.config_file))

  # The run.app address Cloud Run gives a service is fixed by its name, the
  # project's number and the region, so it is known before the service exists.
  # The configuration needs it at start, and the service cannot learn it.
  public_url = (var.public_url != "" ? var.public_url :
  "https://${var.service_name}-${data.google_project.this.number}.${var.region}.run.app")

  # The file as given, with the two settings this deployment decides. Cloud Run
  # sends traffic to port 8080, and publicUrl must be where users actually
  # arrive or sign-in breaks.
  rendered_config = yamlencode(merge(local.config, {
    listen    = ":8080"
    publicUrl = local.public_url
  }))

  # Every environment variable the service reads a secret from: the cookie key
  # and each repository's model provider key, by the name its apiKeyEnv gives.
  model_key_envs = distinct([
    for r in try(local.config.repositories, []) : r.apiKeyEnv
    if try(r.apiKeyEnv, "") != ""
  ])
  secret_envs = concat(["WORKSPACE_COOKIE_KEY"], local.model_key_envs)

  # Memory, by the rule in docs/workspace.md, "Sizing memory": what is held,
  # two archives being unpacked, one index being decoded, and 256 MiB for the
  # runtime and the scanner's scratch files. The guide says to leave headroom
  # because the service's count is low, so a tenth more, rounded up to 512 MiB.
  mib    = 1024 * 1024
  limits = try(local.config.limits, {})
  needed_mib = ceil((
    try(local.limits.cacheBytes, 0)
    + 2 * try(local.limits.archiveBytes, 0)
    + try(local.limits.indexBytes, 0)
  ) / local.mib) + 256
  sized_mib = ceil(local.needed_mib * 1.1 / 512) * 512

  memory_mib = (var.memory == "" ? local.sized_mib :
    endswith(var.memory, "Gi") ? tonumber(trimsuffix(var.memory, "Gi")) * 1024 :
  tonumber(trimsuffix(var.memory, "Mi")))
  memory = var.memory != "" ? var.memory : "${local.sized_mib}Mi"

  # The fewest CPUs Cloud Run accepts with that much memory.
  cpu = var.cpu != "" ? var.cpu : (
    local.memory_mib <= 4096 ? "1" :
    local.memory_mib <= 8192 ? "2" :
    local.memory_mib <= 16384 ? "4" :
    local.memory_mib <= 24576 ? "6" : "8"
  )

  # The runtime service account's id: 6 to 30 characters.
  account_id = "${replace(substr(var.service_name, 0, 26), "/-+$/", "")}-run"
}

# The APIs these use are turned on by deploy.sh, before Terraform runs: see
# README.md, "Without the script", for the command.

resource "google_artifact_registry_repository" "images" {
  repository_id = var.service_name
  location      = var.region
  format        = "DOCKER"
  description   = "Images of the review workspace service."
}

# The identity the container runs as. It can read this deployment's secrets and
# nothing else: the service reaches GitHub with each user's own token.
resource "google_service_account" "runtime" {
  account_id   = local.account_id
  display_name = "Review workspace (${var.service_name})"
}

resource "google_secret_manager_secret" "env" {
  # Secrets in retain_secret_envs are kept even when the configuration no
  # longer names them; see the variable.
  for_each = toset(concat(local.secret_envs, var.retain_secret_envs))
  # "env" keeps a variable named CONFIG from taking the configuration's name.
  secret_id = "${var.service_name}-env-${lower(replace(each.value, "_", "-"))}"

  labels = { service = var.service_name }
  replication {
    auto {}
  }

  # A secret the configuration stops naming is deleted only after the service
  # has stopped reading it. Without this, Terraform deletes it first.
  lifecycle {
    create_before_destroy = true
  }
}

# The configuration is not secret, but Secret Manager is how Cloud Run mounts a
# file. It holds placeholders and variable names, never a key.
resource "google_secret_manager_secret" "config" {
  secret_id = "${var.service_name}-config"

  labels = { service = var.service_name }
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "config" {
  secret      = google_secret_manager_secret.config.id
  secret_data = local.rendered_config

  # The running revision reads the old version until the new revision is
  # ready, so the old one goes only after the new one exists.
  lifecycle {
    create_before_destroy = true
  }
}

resource "google_secret_manager_secret_iam_member" "runtime" {
  for_each = merge(
    { for env, s in google_secret_manager_secret.env : env => s.id },
    { config = google_secret_manager_secret.config.id },
  )
  secret_id = each.value
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.runtime.member

  # Removed after the service stops reading the secret, as for the secret.
  lifecycle {
    create_before_destroy = true
  }
}

resource "google_cloud_run_v2_service" "workspace" {
  name     = var.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  # The service does its own sign-in, so anyone may reach it. This leaves
  # Cloud Run's invoker check off instead of granting allUsers, which an
  # organisation policy restricting sharing to its own domain would refuse.
  invoker_iam_disabled = true
  # The service holds nothing that is not read again from GitHub, so it may
  # be destroyed and created again; deploy.sh --destroy does the first.
  deletion_protection = false

  template {
    service_account                  = google_service_account.runtime.email
    max_instance_request_concurrency = 250
    timeout                          = "900s"

    scaling {
      min_instance_count = var.min_instances
      max_instance_count = 1
    }

    containers {
      image = var.image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          memory = local.memory
          cpu    = local.cpu
        }
        # CPU only while a request is being served, which is when the service
        # works: a streamed answer is inside its request. The exception is an
        # archive or index load whose every requester has gone. It goes on,
        # slowly, and may pass limits.fetchSeconds; the next request for that
        # commit starts it again. CPU at all times would cost more than that.
        cpu_idle          = true
        startup_cpu_boost = true
      }

      dynamic "env" {
        for_each = google_secret_manager_secret.env
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = env.value.secret_id
              version = lookup(var.secret_versions, env.key, "latest")
            }
          }
        }
      }

      volume_mounts {
        name       = "config"
        mount_path = "/etc/workspace"
      }

      # A configuration the service cannot serve from makes it exit at once,
      # with every problem in the log, and the revision fails here.
      startup_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
        period_seconds    = 3
        timeout_seconds   = 2
        failure_threshold = 10
      }
    }

    volumes {
      name = "config"
      secret {
        secret = google_secret_manager_secret.config.secret_id
        items {
          path = "config.yaml"
          # A pinned version, so a changed configuration is a new revision.
          version = google_secret_manager_secret_version.config.version
        }
      }
    }
  }

  lifecycle {
    precondition {
      condition     = var.image != null && var.image != ""
      error_message = "Set image to the container image to run. deploy.sh builds one; see deploy/gcp/README.md."
    }
    precondition {
      condition = var.memory != "" || alltrue([
        for k in ["cacheBytes", "archiveBytes", "indexBytes"] : try(local.limits[k], 0) > 0
      ])
      error_message = "The configuration's limits.cacheBytes, limits.archiveBytes and limits.indexBytes size the container's memory. Set them, or set memory."
    }
  }

  depends_on = [google_secret_manager_secret_iam_member.runtime]
}

check "public_url_reaches_the_service" {
  assert {
    condition     = var.public_url != "" || contains(google_cloud_run_v2_service.workspace.urls, local.public_url)
    error_message = "Cloud Run did not give the service the address the configuration was given (${local.public_url}). Sign-in will fail. Set public_url to one of: ${join(", ", google_cloud_run_v2_service.workspace.urls)}."
  }
}
