<!-- SPDX-License-Identifier: MIT -->
# Deploying the review workspace to Google Cloud

These files put the review workspace (`cmd/workspace`) on Cloud Run in a Google Cloud project you
name. They carry out the settings in [`docs/workspace.md`](../../docs/workspace.md), "Deploying on
Cloud Run". Read that guide first: it says what the service does, who can sign in, and what the
operator can read.

`deploy.sh` does everything in one command. The Terraform in this directory can also be used
without it; see "Without the script".

## What it creates

All in the one project and region, and all named after the service (`review-workspace` unless you
choose another name):

| Resource | What it is for |
| --- | --- |
| A Cloud Run service | One container, at most one instance, 250 concurrent requests, a 900-second request timeout. Anyone can reach it, because the service does its own sign-in. |
| An Artifact Registry repository | The images `deploy.sh` builds. |
| A service account | The identity the container runs as. It can read this deployment's secrets and nothing else. |
| A secret for `WORKSPACE_COOKIE_KEY` | The key that seals session cookies. |
| A secret for each model provider key | One for each variable a repository's `apiKeyEnv` names. |
| A secret holding the configuration | The YAML file, mounted at `/etc/workspace/config.yaml`. It holds no key. |
| A Cloud Storage bucket | Terraform's state. Created by `deploy.sh`, not by Terraform. |

`deploy.sh` also turns on the Cloud Run, Artifact Registry, Secret Manager, IAM, Cloud Storage,
Service Usage and Cloud Resource Manager APIs. They are left on when the deployment is destroyed.

**No key passes through Terraform.** Terraform creates the secrets empty. `deploy.sh` gives each
value to `gcloud` on standard input, so no key reaches Terraform's state, a command line or a file.

## Before you start

- A project linked to a billing account, and an account that can create the resources above in it.
  Owner (`roles/owner`) is enough. So are Cloud Run Admin, Artifact Registry Admin, Secret Manager
  Admin, Service Account Admin, Service Account User, Service Usage Admin and Storage Admin
  together.
- If your organisation enforces the policy `run.managed.requireInvokerIam`, the service cannot be
  opened to everyone, and the deploy fails. An organisation policy administrator must exempt the
  project.
- `gcloud`, signed in with `gcloud auth login`. Terraform acts as that account too, unless
  `GOOGLE_APPLICATION_CREDENTIALS` or `GOOGLE_OAUTH_ACCESS_TOKEN` is set, in which case it acts as
  those. The script's first line says which.
- Terraform 1.5 or newer.
- Docker, to build the image. Not needed with `--image`. The script adds `gcloud` as Docker's
  credential helper for the registry, in `~/.docker/config.json`.
- A configuration file. Copy [`examples/workspace.config.yaml`](../../examples/workspace.config.yaml)
  and change the repositories. Keep it outside this checkout, or as
  `deploy/gcp/workspace.config.yaml`, which git ignores: it names your repositories, and those do
  not belong in this repository's history.

The deployment sets two of the file's settings itself. `listen` becomes `:8080`, the port Cloud Run
sends traffic to. `publicUrl` becomes the service's own address,
`https://<service>-<project number>.<region>.run.app`, or the address you give with `--public-url`.
Whatever the file says for either is replaced.

## Deploy

```
export WORKSPACE_MODEL_KEY_EXAMPLE=...   # one for each apiKeyEnv, the first time only
deploy/gcp/deploy.sh --project my-project --config ~/workspace.config.yaml
```

Terraform shows each plan and asks before applying it. With `--yes` it does not ask. At the end
the script prints the address and checks that the page answers.

A model provider key the environment does not hold is asked for at the terminal, without being
shown. Run without a terminal, the script stops and names the variable it needs.

A run does these five things, and each is safe to repeat:

1. Turns on the APIs, and makes the state bucket, `<project>-<service>-tfstate`, with every
   version of the state kept.
2. Applies the image repository, the secrets the configuration names and the service account,
   without the service. The service cannot start before its secrets have values and its image
   exists.
3. Puts a value in every secret that has none. The cookie key is 32 random bytes; each model key
   comes from the variable of the same name. A secret that has a value is left alone.
4. Builds the image from this checkout for `linux/amd64`, pushes it, and takes its digest. A
   checkout with uncommitted changes builds an image with them, and the tag says `dirty`.
5. Applies the service on that image, by digest, and on the newest version of each secret.

Run the same command again to deploy a new commit or a changed configuration. Each change makes a
new Cloud Run revision. **Nothing is remembered between runs:** give the same `--service`,
`--region`, `--public-url` and `--` options every time, or the ones you leave out return to their
defaults.

On a new project, the first run can fail at step 2 because APIs and permissions take a few minutes
to take effect. Wait, and run the same command again.

### Options

| Option | What it does |
| --- | --- |
| `--region` | Default `us-central1`. |
| `--service` | The service's name, and the stem of every other name. Two deployments with different names can share a project. |
| `--public-url` | Where users reach the service, if not its run.app address. See "A domain of your own". |
| `--state-bucket` | Another bucket for the state. Needed when the default name is not a valid bucket name. |
| `--image` | Run this image instead of building one. Give it by digest. An image in another project's registry needs the Cloud Run service agent of this project to be allowed to read it. |
| `--rotate VAR` | Put a new value in this variable's secret. Repeatable. |
| `--destroy` | Delete the deployment. See "Removing it". |
| `--yes` | Apply, or destroy, without asking. |
| `-- ...` | The rest goes to the last `terraform apply`, such as `-- -var min_instances=1`. |

### Terraform variables

These can be passed after `--`:

| Variable | Default | What it is |
| --- | --- | --- |
| `min_instances` | `0` | `0` lets the container stop after about 15 minutes idle, and the next request reads from GitHub again. `1` keeps it warm, at a monthly cost. |
| `memory` | sized | Container memory, such as `4Gi`. By default it is the sum in "Sizing memory" with a tenth added for headroom, rounded up to 512 MiB: 2560 MiB for the example file. |
| `cpu` | sized | Container CPUs: 1, 2, 4, 6 or 8. By default the fewest Cloud Run accepts with that memory. |

The container has CPU only while it is serving a request. An archive fetch whose every requester
has gone away goes on slowly, and may pass `limits.fetchSeconds`; the next request for that commit
starts it again.

## When a deploy fails

- **At step 2 on a new project:** wait a few minutes and run again, as above.
- **At step 5, the revision never became ready:** the service could not start. Its log in Cloud
  Logging, under the Cloud Run service, lists every problem with the configuration at once. Fix
  the file, or the key, and run again. On a redeploy the previous revision keeps serving
  meanwhile. On a first deploy there is none, and the next run replaces the failed service. If that
  run instead reports that the service already exists, delete it with
  `gcloud run services delete <service> --project <project> --region <region>` and run again.
- **A key is wrong:** the service starts, and the first question to the assistant reports
  `provider_error`. Run again with `--rotate` and the right key.

## Changing a key

```
export WORKSPACE_MODEL_KEY_EXAMPLE=the-new-key
deploy/gcp/deploy.sh --project my-project --config ~/workspace.config.yaml --rotate WORKSPACE_MODEL_KEY_EXAMPLE
```

The script adds a version to the secret, and the deploy starts a new revision that reads it.
`--rotate WORKSPACE_COOKIE_KEY` makes a new cookie key, which signs everyone out.

A key added with `gcloud secrets versions add` is picked up the same way, at the next run of
`deploy.sh`. The old versions stay in Secret Manager until you disable or destroy them.

Removing a repository's `apiKeyEnv` from the configuration removes its secret, and the key in it,
at the next deploy, after the service has stopped reading it.

## A domain of your own

The service works at its run.app address with nothing more. To serve it at a domain of your own,
put the domain in front of the service yourself, with Cloud Run domain mapping where the region
offers it or an external Application Load Balancer, and deploy with
`--public-url https://your.domain`. The domain must be `https`, and what is in front must not
remove or loosen the service's content security policy header. It must also let a request run for
900 seconds, because answers are streamed: Firebase Hosting, for one, stops at 60.

Terraform checks after each apply that Cloud Run gave the service the address it expected. If it
did not, the apply ends with a warning naming the addresses it did give: deploy again with one of
them as `--public-url`.

## Logs

The service's log is in Cloud Logging, under the Cloud Run service. The queries in
`docs/workspace.md` are for Log Analytics, which needs the project's log bucket upgraded to use it.

## Removing it

```
deploy/gcp/deploy.sh --project my-project --config ~/workspace.config.yaml --destroy
```

Give the same `--service`, `--region` and `--state-bucket` as when deploying. Terraform shows what
it will delete and asks first, unless `--yes` is given. This deletes the service, the images, the
secrets and their values, and the service account. A destroyed cookie key cannot be recovered, and
every session sealed with it ends. The state bucket and the APIs are left; delete the bucket with
`gcloud storage rm --recursive gs://<bucket>`.

## Without the script

The same steps by hand. Set these first:

```
P=my-project R=us-central1 S=review-workspace B=$P-$S-tfstate CONFIG=~/workspace.config.yaml
```

Turn on the APIs, make the state bucket, and give Terraform credentials:

```
gcloud services enable --project $P serviceusage.googleapis.com cloudresourcemanager.googleapis.com \
  storage.googleapis.com run.googleapis.com artifactregistry.googleapis.com \
  secretmanager.googleapis.com iam.googleapis.com
gcloud storage buckets create gs://$B --project $P --location $R \
  --uniform-bucket-level-access --public-access-prevention
gcloud storage buckets update gs://$B --versioning
export GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token)"   # lasts an hour
```

Apply everything but the service, then give each secret a value. `terraform output secret_ids`
names the secret for each variable:

```
cd deploy/gcp
terraform init -backend-config=bucket=$B -backend-config=prefix=$S
VARS=(-var project_id=$P -var region=$R -var service_name=$S -var config_file=$CONFIG)
terraform apply "${VARS[@]}" \
  -target=google_artifact_registry_repository.images \
  -target=google_secret_manager_secret_iam_member.runtime

head -c 32 /dev/urandom | base64 | tr -d '\n' |
  gcloud secrets versions add $S-env-workspace-cookie-key --project $P --data-file=-
printf '%s' "$WORKSPACE_MODEL_KEY_EXAMPLE" |
  gcloud secrets versions add $S-env-workspace-model-key-example --project $P --data-file=-
```

Build and push the image, from the repository's root, and apply the service on it by digest:

```
IMG=$R-docker.pkg.dev/$P/$S/workspace:$(git rev-parse --short=12 HEAD)
gcloud auth configure-docker $R-docker.pkg.dev
docker build --platform linux/amd64 -t $IMG ../.. && docker push $IMG
DIGEST=$(docker inspect --format '{{index .RepoDigests 0}}' $IMG)
terraform apply "${VARS[@]}" -var image=$DIGEST
```

Without `secret_versions`, which the script sets, the service reads the latest version of each
secret when an instance starts, so a new key reaches it only at the next revision or restart.

If the configuration no longer names a variable that the last deploy had a secret for, add
`-var 'retain_secret_envs=["THAT_VARIABLE"]'` to the targeted apply. Otherwise that apply deletes
the secret while the running service still reads it. The last apply, run without that option,
takes the variable out of the service first and then deletes the secret.
