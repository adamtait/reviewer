#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Installs the third-party binaries the analyzers spawn, by pinned version and
# SHA-256 checksum.
#
# CI and `make tools` both run this, so a contributor's machine and a runner get
# the same versions. That is not tidiness: three of these tools decide what a
# review reports, and "passes locally, fails in CI" is the cheapest version of
# that problem to have.
#
# A checksum mismatch is fatal. These binaries read the repository under review
# and, in the scanner's case, are trusted to say whether it contains a
# credential — a substituted one is not a degraded review, it is a false clean.
#
# usage: install-tools.sh [DESTDIR]   (default: the first writable of
#                                      $GOPATH/bin, /usr/local/bin, ./bin)

set -euo pipefail

GITLEAKS_VERSION=8.28.0
OPENGREP_VERSION=1.9.0
OSV_VERSION=2.2.3

die() { printf 'install-tools: %s\n' "$1" >&2; exit 1; }
log() { printf 'install-tools: %s\n' "$1" >&2; }

# platform maps uname output onto the names each project uses for its assets.
# They disagree with each other and with uname, so the mapping is per tool.
case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported operating system: $(uname -s). Install gitleaks, opengrep and osv-scanner by hand." ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=x86_64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac
platform="$os-$arch"

# Each row: tool | asset URL | SHA-256 | how to extract.
#
# The checksums for gitleaks come from the checksums file published with its
# release. Opengrep and osv-scanner publish bare binaries with no checksums file,
# so these were computed from the downloaded artifacts and recorded here; that is
# what pinning means either way — the hash of the thing we actually verified.
gitleaks_url=""; gitleaks_sha=""
opengrep_url=""; opengrep_sha=""
osv_url="";      osv_sha=""

case "$platform" in
  linux-x86_64)
    gitleaks_url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
    gitleaks_sha="a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb"
    opengrep_url="https://github.com/opengrep/opengrep/releases/download/v${OPENGREP_VERSION}/opengrep_manylinux_x86"
    opengrep_sha="57a5a7fcc2df876e02f606fd86841078ee6f15c7deb115a7929228c3eaa99273"
    osv_url="https://github.com/google/osv-scanner/releases/download/v${OSV_VERSION}/osv-scanner_linux_amd64"
    osv_sha="8cdb138b36cdb9c99c455cafb32a1c83e1823448dc00e1c8ab9afe474a5e93f0"
    ;;
  darwin-arm64)
    gitleaks_url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_darwin_arm64.tar.gz"
    gitleaks_sha="d942f3ad147250c9edbaab3fed9e482f98d3b59ba10ae97b8d75647e3ade492c"
    opengrep_url="https://github.com/opengrep/opengrep/releases/download/v${OPENGREP_VERSION}/opengrep_osx_arm64"
    opengrep_sha="192331175e2fa4d426268a0e8dc942f98ce28abfcb86443ae6926c92c5e89cf4"
    osv_url="https://github.com/google/osv-scanner/releases/download/v${OSV_VERSION}/osv-scanner_darwin_arm64"
    osv_sha="2df8fe87db40ec268884bc2bfc984d2ea4f0de528a0c54803d20f7dccf307002"
    ;;
  darwin-x86_64)
    gitleaks_url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_darwin_x64.tar.gz"
    gitleaks_sha="edf5a507008b0d2ef4959575772772770586409c1f6f74dabf19cbe7ec341ced"
    opengrep_url="https://github.com/opengrep/opengrep/releases/download/v${OPENGREP_VERSION}/opengrep_osx_x86"
    opengrep_sha="f3e6674b271b8052f54c482c05bda88a979c2734d9e5f55d5f8a2128c89e90ff"
    osv_url="https://github.com/google/osv-scanner/releases/download/v${OSV_VERSION}/osv-scanner_darwin_amd64"
    osv_sha="f20893dffc30411babf816e7799265c671ef6a5c8907408c889f00cd26d13a38"
    ;;
  linux-arm64)
    # gitleaks and osv-scanner publish this platform; opengrep publishes
    # manylinux_aarch64, which is not pinned here because nothing this project
    # runs on uses it. Left unset deliberately rather than guessed: an unverified
    # checksum is worse than an absent tool, which the analyzer handles.
    gitleaks_url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_arm64.tar.gz"
    gitleaks_sha="eff65261156100e5d94a6b3dec313d532fddfe19ae1590bf7a2b4f2699128356"
    osv_url="https://github.com/google/osv-scanner/releases/download/v${OSV_VERSION}/osv-scanner_linux_arm64"
    osv_sha="7033a49a9566da169529b40bb12e90752b1498151d9e1927506781d36019f689"
    ;;
  *) die "no pinned binaries for $platform" ;;
esac

destdir="${1:-}"
if [ -z "$destdir" ]; then
  for candidate in "$(go env GOPATH 2>/dev/null)/bin" /usr/local/bin ./bin; do
    case "$candidate" in /bin) continue ;; esac
    if mkdir -p "$candidate" 2>/dev/null && [ -w "$candidate" ]; then destdir="$candidate"; break; fi
  done
fi
[ -n "$destdir" ] || die "no writable install directory; pass one as an argument"
mkdir -p "$destdir"

verify() { # file expected-sha name
  local actual
  actual="$(sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$1" | cut -d' ' -f1)"
  if [ "$actual" != "$2" ]; then
    die "checksum mismatch for $3
  expected $2
  got      $actual
Refusing to install. This binary decides what a review reports."
  fi
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fetch() { curl --fail --silent --show-error --location --retry 3 --output "$1" "$2"; }

install_binary() { # name url sha
  local name="$1" url="$2" sha="$3"
  if [ -z "$url" ]; then
    log "$name: not published for $platform; skipping. The analyzer reports itself unavailable."
    return 0
  fi
  log "$name: downloading"
  fetch "$work/$name" "$url"
  verify "$work/$name" "$sha" "$name"
  install -m 0755 "$work/$name" "$destdir/$name"
  log "$name: installed to $destdir/$name"
}

install_tarball() { # name url sha member
  local name="$1" url="$2" sha="$3" member="$4"
  log "$name: downloading"
  fetch "$work/$name.tar.gz" "$url"
  verify "$work/$name.tar.gz" "$sha" "$name"
  tar -xzf "$work/$name.tar.gz" -C "$work" "$member"
  install -m 0755 "$work/$member" "$destdir/$name"
  log "$name: installed to $destdir/$name"
}

install_tarball gitleaks "$gitleaks_url" "$gitleaks_sha" gitleaks
install_binary  opengrep "$opengrep_url" "$opengrep_sha"
install_binary  osv-scanner "$osv_url" "$osv_sha"

log "done. Add $destdir to PATH if it is not already there."
