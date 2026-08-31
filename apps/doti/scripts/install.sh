#!/usr/bin/env bash
#
# Put doti on a new machine, then hand over to it.
#
#   curl -fsSL https://raw.githubusercontent.com/riptone/tone.rip/main/apps/doti/scripts/install.sh | bash
#
# This is the only thing that cannot be a Go binary, because it is what
# fetches the Go binary. It does the least it can get away with:
#
#   1. works out this machine's OS and architecture
#   2. makes sure git exists, which needs a package manager and sometimes sudo
#   3. resolves the newest `doti/v*` release and downloads the matching binary
#   4. verifies it against the release's SHA256SUMS
#   5. installs it somewhere on PATH, preferring a directory that needs no sudo
#   6. runs `doti install`, which clones the configs and sets the machine up
#
# Everything after step 5 is doti's job, including the clone - so this script
# never needs to know the repository layout, and does not change when it moves.
#
# IT MUST CONTAIN NO INSTALLER LOGIC, and neither must install.ps1. That is
# the whole point of the Go binary: the manifest parsing, the package lists,
# the linking and the health checks exist once, in one language. These two
# files are allowed to be two files only because they hold nothing that could
# drift - no manifest, no package names, no paths inside the repository. If a
# change here would need the same change there, it belongs in doti instead.
#
# --base-url points at a mirror, or at a local server for the test that proves
# the checksum step refuses a tampered binary.
#
# On trust: this is `curl | bash`. The checksums come from the same release as
# the binary, so they catch a truncated download, not a compromised
# repository. Pass --version to pin a release rather than take the newest.
set -euo pipefail

REPO="${DOTI_REPO:-riptone/tone.rip}"
TAG_PREFIX="doti/v"
VERSION="${DOTI_VERSION:-}"
RUN_INSTALL=true
# Where the release assets come from. Overridable for a mirror, and for the
# test that exercises the checksum verification against a local server -
# which is the only way to prove that step refuses a tampered binary rather
# than merely appearing to check. install.ps1 has the same seam.
BASE_URL="${DOTI_BASE_URL:-}"

while [[ $# -gt 0 ]]; do
  case "${1}" in
    --version) VERSION="${2}"; shift 2 ;;
    --base-url) BASE_URL="${2}"; shift 2 ;;
    --no-install) RUN_INSTALL=false; shift ;;
    -h|--help)
      sed -n '2,22p' "${0}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown option: ${1}" >&2; exit 2 ;;
  esac
done

say()  { printf '==> %s\n' "$*"; }
warn() { printf '!   %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- 1. platform -------------------------------------------------------------
#
# Go's own names, because they are what the release assets are called.
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  MINGW*|MSYS*|CYGWIN*)
    die "on Windows use PowerShell: irm https://raw.githubusercontent.com/${REPO}/main/apps/doti/scripts/install.ps1 | iex" ;;
  *) die "unsupported operating system: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac
say "platform: ${os}/${arch}"

# --- 2. git ------------------------------------------------------------------
#
# doti clones the configs itself but deliberately will not install git: that
# needs a package manager and sudo, and a binary that escalates on its own is
# a worse trade than one sentence of instruction. This script is already a
# `curl | bash` the operator opted into, so it is the right place for it.
if ! command -v git >/dev/null 2>&1; then
  say "installing git"
  case "${os}" in
    darwin)
      xcode-select --install 2>/dev/null || true
      die "finish the Xcode Command Line Tools install, then re-run this" ;;
    linux)
      if   command -v apt-get >/dev/null 2>&1; then sudo apt-get update && sudo apt-get install -y git
      elif command -v dnf     >/dev/null 2>&1; then sudo dnf install -y git
      elif command -v pacman  >/dev/null 2>&1; then sudo pacman -S --noconfirm git
      else die "no known package manager - install git, then re-run this"
      fi ;;
    *) die "cannot install git on ${os}" ;;
  esac
fi

# --- 3. resolve the release --------------------------------------------------
#
# Tags are namespaced by app, which is what makes "the newest doti release" a
# different question from "the newest release" in a repository that also
# releases ssh-cv.
api="https://api.github.com/repos/${REPO}/releases"
if [[ -z "${VERSION}" ]]; then
  say "resolving the newest ${TAG_PREFIX}* release"
  VERSION=$(curl -fsSL "${api}?per_page=100" \
    | grep -o "\"tag_name\": *\"${TAG_PREFIX}[^\"]*\"" \
    | head -1 | sed 's/.*"\(.*\)"/\1/' | sed "s|^${TAG_PREFIX}|v|")
  [[ -n "${VERSION}" ]] || die "found no ${TAG_PREFIX}* release in ${REPO}"
fi
tag="${TAG_PREFIX}${VERSION#v}"
say "installing doti ${VERSION}"

# --- 4. download and verify --------------------------------------------------
asset="doti_${os}_${arch}"
base="${BASE_URL:-https://github.com/${REPO}/releases/download/${tag}}"
tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT

say "downloading ${asset}"
curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}" \
  || die "no asset ${asset} in release ${tag}"
curl -fsSL "${base}/SHA256SUMS" -o "${tmp}/SHA256SUMS" \
  || die "release ${tag} has no SHA256SUMS"

say "verifying the checksum"
(
  cd "${tmp}"
  # Only this asset's line: the file covers every platform, and every other
  # line names a file that was never downloaded.
  grep " ${asset}\$" SHA256SUMS > wanted.txt || exit 1
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c wanted.txt
  else shasum -a 256 -c wanted.txt
  fi
) >/dev/null || die "checksum mismatch for ${asset} - refusing to install"

chmod +x "${tmp}/${asset}"
# Ask the downloaded binary its own version before it goes anywhere near
# PATH, so a truncated file or the wrong architecture fails while it is still
# a temp file.
got=$("${tmp}/${asset}" version 2>/dev/null) || die "the downloaded binary does not run"
say "downloaded binary reports ${got}"

# --- 5. install onto PATH ----------------------------------------------------
#
# ~/.local/bin first: it needs no sudo and is on PATH by default on most
# distributions and in this repo's own .zprofile. /usr/local/bin is the
# fallback for a machine where it is not.
on_path() {
  case ":${PATH}:" in
    *":${1}:"*) return 0 ;;
    *) return 1 ;;
  esac
}

if [[ -n "${DOTI_BIN_DIR:-}" ]]; then
  bindir="${DOTI_BIN_DIR}"
else
  bindir="${HOME}/.local/bin"
  on_path "${bindir}" || warn "${bindir} is not on PATH - add it to your shell profile"
fi
mkdir -p "${bindir}"
install -m 0755 "${tmp}/${asset}" "${bindir}/doti"
say "installed ${bindir}/doti"

# --- 6. hand over ------------------------------------------------------------
if ! ${RUN_INSTALL}; then
  say "done - run \`doti\` for the menu, or \`doti install\` for everything"
  exit 0
fi
say "running doti install"

# Piped into bash, stdin is the exhausted download rather than the terminal,
# so anything doti wants to ask - the vault password above all - has nothing
# to read from: that is how the first release reached `bw unlock` and had it
# crash on a closed pipe. Hand doti the terminal when there is one. When there
# is not (CI, a container), it sees a non-interactive stdin and defers the
# prompt rather than hanging on it.
#
# The open is attempted rather than tested with -r, because a session with no
# controlling terminal has a /dev/tty that passes -r and still fails open()
# with ENXIO - and learning that from a failed exec would kill the installer
# after it had already put the binary in place.
if (: </dev/tty) 2>/dev/null; then
  exec "${bindir}/doti" install </dev/tty
fi
exec "${bindir}/doti" install
