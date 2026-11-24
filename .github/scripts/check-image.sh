#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Holds the workspace service's image to one rule: it contains the programs it
# is meant to run and nothing that could run anything else.
#
# The service reads pull requests whose authors it does not trust, and it must
# never execute their code (docs/designs/review-workspace.md, Goal 7). A test in
# the Go suite checks that the service's own packages never start a program.
# This checks the other half: that the image offers nothing to start. No shell,
# no git, no interpreter, and no executable file that is not on the list.
#
# usage: check-image.sh IMAGE ALLOWED_EXECUTABLE...
#   for example: check-image.sh workspace:ci usr/local/bin/workspace

set -euo pipefail

die() { printf 'check-image: %s\n' "$1" >&2; exit 1; }

[ "$#" -ge 2 ] || die "usage: check-image.sh IMAGE ALLOWED_EXECUTABLE..."
image=$1
shift

# Files the distroless base image ships with an execute bit that are not
# programs: three data files, and one shell script with no shell to run it.
# Listed so that anything else with an execute bit is a surprise worth a look.
# Checked against the base image's layers when the digest in the Dockerfile was
# pinned; a new base image that changes this list fails here, on purpose.
base_data=(
  etc/update-motd.d/10-uname
  usr/lib/os-release
  etc/ssl/certs/ca-certificates.crt
  usr/share/doc/ca-certificates/copyright
)

container=$(docker create "$image")
trap 'docker rm -f "$container" >/dev/null' EXIT

# One line per entry: permissions, then the path with any leading ./ removed.
listing=$(docker export "$container" | tar -tv | awk '{ sub(/^\.\//, "", $6); print $1, $6 }')
[ -n "$listing" ] || die "the image has no files, so the check proved nothing"

failed=0

# Anything by one of these names, whatever its permissions.
forbidden='(^|/)(sh|bash|dash|ash|zsh|busybox|git|curl|wget|nc|ssh|env|perl|node|deno|bun|python[0-9.]*|ruby)$'
while read -r _ path; do
  if [[ "$path" =~ $forbidden ]]; then
    printf 'check-image: the image contains %s, which can start or fetch other programs\n' "$path" >&2
    failed=1
  fi
done <<<"$listing"

# Every regular file with an execute bit must be on a list.
allowed=("$@" "${base_data[@]}")
while read -r mode path; do
  [[ "$mode" == -* && "$mode" == *x* ]] || continue
  known=0
  for ok in "${allowed[@]}"; do
    [ "$path" = "$ok" ] && known=1 && break
  done
  if [ "$known" -eq 0 ]; then
    printf 'check-image: unexpected executable in the image: %s\n' "$path" >&2
    failed=1
  fi
done <<<"$listing"

# And each program the image is meant to hold has to be there.
for want in "$@"; do
  if ! grep -q -E "^-[^ ]*x[^ ]* ${want}\$" <<<"$listing"; then
    printf 'check-image: expected executable is missing: %s\n' "$want" >&2
    failed=1
  fi
done

[ "$failed" -eq 0 ] || exit 1
printf 'check-image: %s holds %s and nothing else that can run\n' "$image" "$*"
