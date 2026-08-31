#!/usr/bin/env bash
#
# Cross-compile every released doti binary, and checksum them.
#
# One list of targets, called by two things: the release workflow, and
# `bun run release` for anybody who wants to see what a release would produce
# without tagging one. It lived in the workflow, where it could not be run
# locally and could not be linted.
#
# usage: scripts/build-release.sh [VERSION]
#
# VERSION is stamped into the binary and is what `doti version` prints. Omitted,
# it is derived from git - a local build is never mistaken for a release.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Six binaries, because a dotfiles installer that only covers the machine its
# author happens to own is the problem it exists to solve.
TARGETS=(
  darwin/amd64 darwin/arm64
  linux/amd64 linux/arm64
  windows/amd64 windows/arm64
)

version="${1:-}"
if [[ -z "${version}" ]]; then
  # A local build says so in its own version string, which is also what stops
  # the update check from offering to replace a working copy.
  described=$(git describe --tags --match 'doti/v*' --abbrev=0 2>/dev/null || echo "doti/v0.0.0")
  version="${described#doti/}+dev.$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
fi

# sha256sum is coreutils and shasum is what macOS ships. The workflow runs on
# Linux and a developer probably does not, so both.
checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

rm -rf dist
mkdir -p dist

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [[ "${os}" == windows ]] && ext=".exe"

  # CGO_ENABLED=0 is explicit rather than implied: doti shells out to git, brew,
  # winget and bw and links nothing, so a static binary runs on any libc - which
  # matters for the Linux boxes it will be curl-installed on.
  #
  # -trimpath so the binary carries no build-machine paths, and -s -w because
  # nothing here is debugged from a core dump.
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${version}" \
    -o "dist/doti_${os}_${arch}${ext}" ./cmd/doti

  printf '  built dist/doti_%s_%s%s\n' "${os}" "${arch}" "${ext}"
done

cd dist
checksum doti_* > SHA256SUMS
printf '\n%s\n' "$(cat SHA256SUMS)"
printf '\nstamped %s\n' "${version}"
