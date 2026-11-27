# SPDX-License-Identifier: MIT

terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 8.12, < 9.0"
    }
  }

  # State lives in a Cloud Storage bucket in the target project. The bucket and
  # prefix are given at init, so one copy of these files deploys to any project:
  #
  #   terraform init -backend-config=bucket=<bucket> -backend-config=prefix=<service>
  #
  # deploy.sh creates the bucket and passes both.
  backend "gcs" {}
}

provider "google" {
  project = var.project_id
  region  = var.region
}
