#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Deploys the review workspace to a Google Cloud project, or deploys it again.
# See deploy/gcp/README.md. Run it from anywhere:
#
#   deploy/gcp/deploy.sh --project my-project --config path/to/workspace.config.yaml
#
# In order, every time:
#   1. turns on the APIs, and makes a bucket for Terraform's state
#   2. applies the parts that hold things: the image repository, the secrets,
#      the runtime service account
#   3. puts a value in every secret that has none, and in any named by --rotate
#   4. builds the image from this checkout and pushes it, unless --image is given
#   5. applies the rest: the Cloud Run service, on that image and those secrets
#
# Every step is safe to repeat. Keys are read from the environment or the
# terminal and handed to gcloud on standard input: none is printed, written to
# a file, or given to Terraform.
#
# It runs on the bash 3.2 that macOS ships: no associative arrays, and every
# possibly empty array is expanded as ${a[@]+"${a[@]}"}.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: deploy.sh --project ID --config FILE [options] [-- terraform apply options]

Required:
  --project ID         The Google Cloud project to deploy into.
  --config FILE        The service's configuration. See examples/workspace.config.yaml.
                       Its listen and publicUrl are set by the deployment.

Options:
  --region REGION      Default: us-central1.
  --service NAME       The Cloud Run service's name. Default: review-workspace.
  --public-url URL     Where users reach the service, if not its run.app address.
  --state-bucket NAME  The Cloud Storage bucket for Terraform's state.
                       Default: <project>-<service>-tfstate, created if missing.
  --image REF          Run this image instead of building one from the checkout.
  --rotate VAR         Put a new value in the secret for this environment variable,
                       even though it has one. WORKSPACE_COOKIE_KEY gets new random
                       bytes, which signs everyone out. Repeatable.
  --destroy            Delete the deployment: the service, its images, its secrets
                       and their values, and its service account.
  --yes                Apply, or destroy, without asking.
  -h, --help           This text.

Anything after -- is passed to the last terraform apply, for example
  -- -var min_instances=1 -var memory=4Gi
Nothing is remembered between runs: give the same options every time.

Each repository's model provider key is read from the environment variable its
apiKeyEnv names, or asked for at the terminal, when its secret has no value yet.
EOF
}

say() { printf '==> %s\n' "$*" >&2; }
die() { printf 'deploy.sh: %s\n' "$*" >&2; exit 1; }

TF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TF_DIR/../.." && pwd)"

project="" config="" region="us-central1" service="review-workspace"
public_url="" state_bucket="" image="" auto_approve=false destroy=false
rotate=() tf_extra=()

while (($#)); do
  case "$1" in
    --project) project="${2:?--project needs a value}"; shift 2 ;;
    --config) config="${2:?--config needs a value}"; shift 2 ;;
    --region) region="${2:?--region needs a value}"; shift 2 ;;
    --service) service="${2:?--service needs a value}"; shift 2 ;;
    --public-url) public_url="${2:?--public-url needs a value}"; shift 2 ;;
    --state-bucket) state_bucket="${2:?--state-bucket needs a value}"; shift 2 ;;
    --image) image="${2:?--image needs a value}"; shift 2 ;;
    --rotate) rotate+=("${2:?--rotate needs a value}"); shift 2 ;;
    --destroy) destroy=true; shift ;;
    --yes) auto_approve=true; shift ;;
    -h | --help) usage; exit 0 ;;
    --) shift; tf_extra=("$@"); break ;;
    *) usage >&2; die "unknown argument: $1" ;;
  esac
done

[[ -n "$project" ]] || { usage >&2; die "--project is required"; }
[[ -n "$config" ]] || { usage >&2; die "--config is required"; }
[[ -f "$config" ]] || die "no configuration at $config"
config="$(cd "$(dirname "$config")" && pwd)/$(basename "$config")"

state_bucket="${state_bucket:-$project-$service-tfstate}"
[[ "$state_bucket" =~ ^[a-z0-9][-a-z0-9_]{1,61}[a-z0-9]$ ]] ||
  die "\"$state_bucket\" is not a usable bucket name; choose one with --state-bucket"

need() { command -v "$1" >/dev/null || die "$1 is not installed or not on PATH"; }
need gcloud
need terraform
if [[ -z "$image" ]] && ! $destroy; then
  need docker
  need git
fi

# --- Credentials --------------------------------------------------------------

# Terraform acts as the account gcloud is signed in as, unless the caller has
# already given it credentials of its own (in CI, for example). The token lasts
# an hour, so it is fetched again before the last apply.
account="$(gcloud config get-value account 2>/dev/null || true)"
own_token=false
if [[ -z "${GOOGLE_OAUTH_ACCESS_TOKEN:-}" && -z "${GOOGLE_APPLICATION_CREDENTIALS:-}" ]]; then
  [[ -n "$account" ]] || die "gcloud is not signed in; run: gcloud auth login"
  own_token=true
fi
refresh_token() {
  if $own_token; then
    GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token)" ||
      die "gcloud could not give Terraform a token"
    export GOOGLE_OAUTH_ACCESS_TOKEN
  fi
}
refresh_token

if $own_token; then
  identity="$account"
elif [[ -n "${GOOGLE_OAUTH_ACCESS_TOKEN:-}" ]]; then
  identity="the token in GOOGLE_OAUTH_ACCESS_TOKEN"
else
  identity="the credentials in $GOOGLE_APPLICATION_CREDENTIALS"
fi

gcloud projects describe "$project" --format='value(projectId)' >/dev/null ||
  die "cannot read project $project as ${account:-the current gcloud credentials}"
# Everything this creates needs billing. The check is skipped when it cannot be
# made: reading billing needs a permission that deploying does not.
if [[ "$(gcloud billing projects describe "$project" --format='value(billingEnabled)' 2>/dev/null || true)" == False ]]; then
  die "project $project has no billing account; link one first"
fi

if $destroy; then
  say "Destroying $service in $project ($region), as $identity for Terraform and ${account:-none} for gcloud"
else
  say "Deploying $service to $project in $region, as $identity for Terraform and ${account:-none} for gcloud"
fi

# --- 1. APIs and the state bucket --------------------------------------------

if ! $destroy; then
  say "Turning on the APIs"
  gcloud services enable --project "$project" --quiet \
    serviceusage.googleapis.com cloudresourcemanager.googleapis.com storage.googleapis.com \
    run.googleapis.com artifactregistry.googleapis.com secretmanager.googleapis.com iam.googleapis.com

  if ! gcloud storage buckets describe "gs://$state_bucket" --format='value(name)' >/dev/null 2>&1; then
    say "Creating the state bucket gs://$state_bucket"
    gcloud storage buckets create "gs://$state_bucket" --project "$project" --location "$region" \
      --uniform-bucket-level-access --public-access-prevention --quiet
    # Every write to the state is kept, so a bad apply can be undone.
    gcloud storage buckets update "gs://$state_bucket" --versioning --quiet
  fi
fi

# One Terraform working directory per project and service, so that a command
# run by hand in it afterwards finds that deployment's state and no other.
export TF_DATA_DIR="$TF_DIR/.terraform/$project/$service"
tf() { terraform -chdir="$TF_DIR" "$@"; }

tf init -input=false -reconfigure \
  -backend-config="bucket=$state_bucket" -backend-config="prefix=$service" >/dev/null

vars=(-var "project_id=$project" -var "region=$region" -var "service_name=$service"
  -var "config_file=$config" -var "public_url=$public_url")
# Without --yes Terraform shows each plan and asks, which needs input on.
approve=()
$auto_approve && approve=(-input=false -auto-approve)

if $destroy; then
  # The image is required by a check that a destroy does not make.
  tf destroy ${approve[@]+"${approve[@]}"} "${vars[@]}" -var image=unused
  say "Destroyed. The state bucket gs://$state_bucket and the APIs are left on"
  exit 0
fi

# The environment variables the configuration reads secrets from, worked out
# from the file alone, before anything is changed.
envs_line="$(echo 'join(" ", local.secret_envs)' | tf console "${vars[@]}")" ||
  die "Terraform could not read the configuration $config"
envs_line="${envs_line//\"/}"
read -r -a envs <<<"$envs_line"

listed() { # needle, haystack...
  local needle="$1" x
  shift
  for x in "$@"; do [[ "$x" == "$needle" ]] && return 0; done
  return 1
}
for env in ${rotate[@]+"${rotate[@]}"}; do
  listed "$env" "${envs[@]}" ||
    die "--rotate $env: the configuration reads no secret by that name (it reads: ${envs[*]})"
done

# --- 2. The parts that hold things --------------------------------------------

# The service cannot start before its secrets have values and its image exists,
# so these come first. Targeted, so the service is left as it is. The secrets
# the last deploy made are kept here even if the configuration no longer names
# them: the running service reads them until the last apply takes them out of
# it, and only then deletes them.
retain=()
if previous="$(tf output -raw secret_ids_tsv 2>/dev/null)"; then
  while IFS=$'\t' read -r env _; do
    [[ -n "$env" ]] && retain+=("\"$env\"")
  done <<<"$previous"
fi
retain_list="[$(IFS=,; printf '%s' "${retain[*]+"${retain[*]}"}")]"

say "Applying the image repository, secrets and service account"
tf apply ${approve[@]+"${approve[@]}"} "${vars[@]}" -var "retain_secret_envs=$retain_list" \
  -target=google_artifact_registry_repository.images \
  -target=google_secret_manager_secret_iam_member.runtime ||
  die "the first apply failed. On a new project, APIs and permissions can take a few minutes to take effect: wait, then run the same command again"

# --- 3. Secret values ---------------------------------------------------------

newest_version() { # secret id; prints nothing if it has no enabled version
  gcloud secrets versions list "$1" --project "$project" --filter='state=ENABLED' \
    --sort-by='~createTime' --limit=1 --format='value(name.basename())'
}
add_version() { # secret id; the value arrives on standard input
  gcloud secrets versions add "$1" --project "$project" --data-file=- --quiet >/dev/null
}

tsv="$(tf output -raw secret_ids_tsv)" || die "Terraform did not report the secrets it made"

versions=()
while IFS=$'\t' read -r env sid; do
  [[ -n "$env" ]] || continue
  # A failure to list is not "no version": taking it for one would replace
  # the cookie key and sign everyone out.
  current="$(newest_version "$sid")" || die "could not list the versions of $sid"

  if [[ -z "$current" ]] || listed "$env" ${rotate[@]+"${rotate[@]}"}; then
    if [[ "$env" == WORKSPACE_COOKIE_KEY ]]; then
      say "Generating a cookie key in $sid"
      head -c 32 /dev/urandom | base64 | tr -d '\n' | add_version "$sid"
    else
      value="${!env:-}"
      if [[ -z "$value" && -t 0 ]]; then
        read -rsp "Model provider key for $env (not shown): " value </dev/tty
        printf '\n' >&2
      fi
      [[ -n "$value" ]] || die "$env has no value: set it in the environment, or run from a terminal"
      say "Storing $env in $sid"
      printf '%s' "$value" | add_version "$sid"
      unset value
    fi
    current="$(newest_version "$sid")" || die "could not list the versions of $sid"
    [[ -n "$current" ]] || die "$sid still has no version"
  fi
  versions+=("$env=\"$current\"")
done <<<"$tsv"
((${#versions[@]})) || die "Terraform reported no secrets"
# Pinned, so a key added since the last deploy makes a new revision.
secret_versions="{$(IFS=,; printf '%s' "${versions[*]}")}"

# --- 4. The image -------------------------------------------------------------

if [[ -z "$image" ]]; then
  repo="$(tf output -raw image_repository)"
  version="$(git -C "$REPO_ROOT" describe --tags --always --dirty)"
  tag="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)"
  if [[ -n "$(git -C "$REPO_ROOT" status --porcelain)" ]]; then
    say "The checkout has uncommitted changes, and they go into the image"
    tag="$tag-dirty-$(date -u +%Y%m%d%H%M%S)"
  fi

  say "Building $repo/workspace:$tag"
  # Adds gcloud as Docker's credential helper for this registry, in
  # ~/.docker/config.json.
  gcloud auth configure-docker "$region-docker.pkg.dev" --quiet >/dev/null ||
    die "could not set Docker up to push to $region-docker.pkg.dev"
  # Cloud Run runs amd64, whatever machine builds the image.
  docker build --platform linux/amd64 --build-arg "VERSION=$version" \
    -t "$repo/workspace:$tag" "$REPO_ROOT"
  docker push "$repo/workspace:$tag"
  # Run what was pushed, by digest: a tag can be moved.
  image="$(docker inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' \
    "$repo/workspace:$tag" | grep -m1 "^$repo/workspace@")" ||
    die "could not find the digest of the pushed image"
fi

# --- 5. The service -----------------------------------------------------------

refresh_token
say "Applying the service on $image"
tf apply ${approve[@]+"${approve[@]}"} "${vars[@]}" \
  -var "image=$image" -var "secret_versions=$secret_versions" ${tf_extra[@]+"${tf_extra[@]}"}

# Not /healthz: Cloud Run keeps some addresses ending in z for itself and
# answers them before the container sees them. The page is served at /.
url="$(tf output -raw url)"
if command -v curl >/dev/null && curl -fsS --max-time 30 -o /dev/null "$url/"; then
  say "Serving at $url"
else
  say "Deployed, but $url/ did not answer yet; see the service's log in Cloud Logging"
fi
