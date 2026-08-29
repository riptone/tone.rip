#!/usr/bin/env bash
#
# Install, update or check ssh-cv on the box that serves it.
#
#   curl -fsSL https://raw.githubusercontent.com/riptone/tone.rip/main/apps/ssh-cv/scripts/install.sh | sudo bash
#
# This exists because apps/ssh-cv is the one app in this repo that `main`
# cannot deploy for itself. The three Workers get pushed by CI; this one runs
# on a box in Oracle Cloud, and before this script the only way to update it
# was to cross-compile locally, scp the binary up and remember to restart the
# service - a sequence with no version number anywhere in it, so "is the box
# running the current CV?" was unanswerable without reading a diff.
#
# What it does, in the order it matters:
#
#   1. resolves a version - the newest `ssh-cv/v*` release, or --version
#   2. compares it against what the installed binary reports, and stops if
#      they match, so the daily timer costs one API call on most days
#   3. downloads the binary for this architecture and verifies it against the
#      release's SHA256SUMS
#   4. runs the *downloaded* binary's --version before touching the live one,
#      so a truncated download or the wrong architecture is caught while it
#      is still a temp file
#   5. swaps it in by rename, restarts the service, and rolls back to the
#      previous binary if the service does not come up
#
# On trust: this is `curl | sudo bash`, and the checksums come from the same
# release as the binary, so they protect against a corrupted download and not
# against a compromised repository. What limits the blast radius is step (5)
# of the timer setup: the copy of this script that runs unattended is pinned
# to the release it was installed with and is only replaced when a human runs
# this again. Nothing on the box follows `main`.
#
# Exit codes: 0 done or already current, 10 update available (--check only),
# anything else is a failure.

set -euo pipefail

# --- configuration -----------------------------------------------------------
#
# Three sources, in this order of authority: a command-line flag, then the
# environment, then /etc/default/ssh-cv-update. Whatever the operator typed
# most recently and most explicitly wins, and the file is only ever a default
# - which is what makes it safe for the timer to read the same file.
#
# The repository is configurable to support forks or future renames.
# GitHub redirects a renamed owner and repository indefinitely, so a box
# installed before a rename keeps updating without anyone noticing.
#
# The default is the current name rather than a redirect, so a fresh install
# does not depend on GitHub keeping those redirects forever. `--repo` exists
# for the next move, and for anyone running a fork.
DEFAULT_REPO="riptone/tone.rip"

env_repo="${SSH_CV_REPO:-}"
env_notify="${SSH_CV_NOTIFY_URL:-}"
env_mode="${SSH_CV_UPDATE_MODE:-}"

# Release tags are namespaced by app. This prefix is what makes "the newest
# ssh-cv release" a different question from "the newest release", which in a
# monorepo it has to be.
TAG_PREFIX="ssh-cv/"

BIN_PATH="${SSH_CV_BIN:-/usr/local/bin/ssh-cv}"
UPDATER_PATH="/usr/local/bin/ssh-cv-update"
SERVICE="ssh-cv"
UPDATE_UNIT="ssh-cv-update"
DEFAULTS_FILE="/etc/default/ssh-cv-update"
UNIT_DIR="/etc/systemd/system"

want_version=""
flag_repo=""
force=0
check_only=0
install_timer=0
quiet=0

# --- output ------------------------------------------------------------------

log() { [[ "${quiet}" -eq 1 ]] || printf 'ssh-cv-install: %s\n' "$*"; }
warn() { printf 'ssh-cv-install: %s\n' "$*" >&2; }
die() {
  warn "$*"
  exit 1
}

usage() {
  cat <<'EOF'
Install, update or check ssh-cv.

  --version vX.Y.Z   install this release instead of the newest one
  --check            report whether an update exists; change nothing
                     (exit 0 = current, 10 = update available)
  --force            reinstall even if the version already matches
  --install-timer    also install the daily auto-update timer
  --repo owner/name  pull releases from this repository
  --quiet            only speak when something happens or breaks
  --help             this

Environment: SSH_CV_REPO, SSH_CV_BIN, SSH_CV_NOTIFY_URL, SSH_CV_UPDATE_MODE.
EOF
}

# --- argument parsing --------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      want_version="${2:-}"
      [[ -n "${want_version}" ]] || die "--version needs a value, e.g. --version v1.4.0"
      shift 2
      ;;
    --version=*)
      want_version="${1#*=}"
      shift
      ;;
    --repo)
      flag_repo="${2:-}"
      [[ -n "${flag_repo}" ]] || die "--repo needs a value, e.g. --repo riptone/tone.rip"
      shift 2
      ;;
    --repo=*)
      flag_repo="${1#*=}"
      shift
      ;;
    --check)
      check_only=1
      shift
      ;;
    --force)
      force=1
      shift
      ;;
    --install-timer)
      install_timer=1
      shift
      ;;
    --quiet)
      quiet=1
      shift
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

# The file is the weakest source, so the variables the environment supplied
# are put aside and the file's own values are read from a clean slate. Without
# the unset, a file that does not mention a setting would appear to set it to
# whatever the environment already held, and "file loses to environment" would
# be impossible to tell from "file agrees with environment".
if [[ -r "${DEFAULTS_FILE}" ]]; then
  unset SSH_CV_REPO SSH_CV_NOTIFY_URL SSH_CV_UPDATE_MODE
  # shellcheck disable=SC1090
  . "${DEFAULTS_FILE}"
fi

REPO="${flag_repo:-${env_repo:-${SSH_CV_REPO:-${DEFAULT_REPO}}}}"

# Any URL that takes a plain-text POST body; the title rides in a header,
# which is ntfy.sh's shape. A notification never decides the outcome - if it
# fails, the update still happened and the failure is logged.
NOTIFY_URL="${env_notify:-${SSH_CV_NOTIFY_URL:-}}"

# apply = install what is newest. check = say so and change nothing.
UPDATE_MODE="${env_mode:-${SSH_CV_UPDATE_MODE:-apply}}"

# One way only: the file or the environment can ask for check mode, and the
# --check flag can too, but neither can turn a --check off.
if [[ "${UPDATE_MODE}" = "check" ]]; then
  check_only=1
fi

# --- helpers -----------------------------------------------------------------

notify() {
  [[ -n "${NOTIFY_URL}" ]] || return 0
  curl -fsS --max-time 10 \
    -H "Title: ssh-cv on $(uname -n)" \
    -d "$1" \
    "${NOTIFY_URL}" >/dev/null 2>&1 ||
    warn "could not send notification to ${NOTIFY_URL}"
}

# Root is what the default paths need, not what the script needs. Writing the
# systemd units is genuinely root-only; installing the binary only needs write
# access to wherever it is going, which is why that has its own check - it
# lets SSH_CV_BIN=~/.local/bin/ssh-cv work without sudo, and it lets this be
# tested without it.
need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "must run as root (try: curl … | sudo bash)"
}

need_writable() {
  local dir
  dir="$(dirname "${BIN_PATH}")"
  [[ -d "${dir}" ]] || die "no such directory: ${dir}"
  [[ -w "${dir}" ]] || die "cannot write ${dir} (try: curl … | sudo bash)"
}

need_tools() {
  for tool in curl sha256sum uname install mv; do
    command -v "${tool}" >/dev/null 2>&1 || die "missing required tool: ${tool}"
  done
}

# The name the release assets use for this box.
detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) die "unsupported architecture: $(uname -m) (releases cover amd64 and arm64)" ;;
  esac
}

# The newest release tag whose name starts with the app prefix.
#
# Parsed with grep rather than jq because jq is not on a minimal Oracle Linux
# or Ubuntu image and one dependency for one field is a poor trade. The API
# returns releases newest first, so the first match is the answer.
#
# This does not distinguish a prerelease from a release - doing that with grep
# would mean tracking JSON object boundaries. So: do not publish an ssh-cv
# prerelease unless you want boxes to take it, and pin with --version if you
# ever need to.
latest_tag() {
  local body
  body="$(curl -fsSL --max-time 30 \
    -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${REPO}/releases?per_page=100")" ||
    die "could not reach the GitHub API for ${REPO}"

  # `|| true` because "no match" is an answer, not a failure, and under
  # `set -euo pipefail` it would otherwise be fatal: grep exits 1 when it
  # finds nothing (and head closing the pipe early can hand it a SIGPIPE),
  # the pipeline inherits that, and the caller dies before it can say which
  # repository had no releases in it. Emptiness is checked at the call site.
  printf '%s' "${body}" |
    grep -o "\"tag_name\"[[:space:]]*:[[:space:]]*\"${TAG_PREFIX}v[^\"]*\"" |
    head -n 1 |
    sed 's/.*"\([^"]*\)"$/\1/' || true
}

# What the installed binary says it is, or "" if there is nothing to ask.
#
# Errors are swallowed on purpose: a binary for the wrong architecture, or a
# half-written file from an interrupted install, cannot answer, and the right
# response to that is to install over it rather than to stop.
installed_version() {
  [[ -x "${BIN_PATH}" ]] || return 0
  "${BIN_PATH}" --version 2>/dev/null || true
}

# `systemctl cat` is the cheap existence test: it fails for a unit systemd has
# never heard of, and succeeds whether the unit is running, stopped or masked.
service_exists() {
  command -v systemctl >/dev/null 2>&1 &&
    systemctl cat "${SERVICE}.service" >/dev/null 2>&1
}

# --- the timer ---------------------------------------------------------------

# write_timer <tag> - install the unattended updater and its timer.
#
# Takes the tag rather than reading the caller's variable, because what it
# pins the updater to is the one decision in here worth being explicit about.
write_timer() {
  need_root

  local pin="$1"
  # The unattended copy is pinned to the release being installed, not to
  # `main`. A repository compromise then does not become root on this box
  # until a human runs the installer again - which is the only meaningful
  # limit available to something that installs from the internet.
  local script_url tmp
  tmp="$(mktemp)"
  script_url="https://raw.githubusercontent.com/${REPO}/${pin}/apps/ssh-cv/scripts/install.sh"
  if ! curl -fsSL --max-time 30 "${script_url}" -o "${tmp}" ||
    ! head -n 1 "${tmp}" | grep -q '^#!'; then
    warn "could not fetch the updater from ${pin}; falling back to main"
    script_url="https://raw.githubusercontent.com/${REPO}/main/apps/ssh-cv/scripts/install.sh"
    curl -fsSL --max-time 30 "${script_url}" -o "${tmp}" ||
      die "could not fetch the updater script"
    head -n 1 "${tmp}" | grep -q '^#!' ||
      die "the updater script does not look like a script"
  fi
  install -m 755 "${tmp}" "${UPDATER_PATH}"
  rm -f "${tmp}"
  log "updater installed at ${UPDATER_PATH} (from ${script_url})"

  if [[ ! -e "${DEFAULTS_FILE}" ]]; then
    cat >"${DEFAULTS_FILE}" <<'EOF'
# Options for the ssh-cv auto-updater (ssh-cv-update.timer).

# apply = install the newest release. check = only report that one exists.
SSH_CV_UPDATE_MODE=apply

# Uncomment to be told when a version lands, or when one is available in
# check mode. Any URL that accepts a plain-text POST body works; ntfy.sh
# needs nothing but a topic name you have not told anyone.
#SSH_CV_NOTIFY_URL=https://ntfy.sh/change-me-to-something-unguessable

# Uncomment while this repo's GitHub organisation rename is outstanding.
#SSH_CV_REPO=riptone/tone.rip
EOF
    chmod 644 "${DEFAULTS_FILE}"
    log "wrote ${DEFAULTS_FILE}"
  fi

  # Not as locked down as ssh-cv.service, and it cannot be: its whole job is
  # to write a binary into /usr/local/bin and restart another unit. What it
  # does get is everything that does not conflict with that.
  cat >"${UNIT_DIR}/${UPDATE_UNIT}.service" <<EOF
[Unit]
Description=Update ssh-cv to the newest release
Documentation=https://github.com/${REPO}/blob/main/docs/ssh-cv-deployment.md
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=-${DEFAULTS_FILE}
ExecStart=${UPDATER_PATH} --quiet

NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=full
ReadWritePaths=/usr/local/bin
ProtectKernelTunables=yes
ProtectKernelModules=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=yes
LockPersonality=yes
EOF

  # Daily, but not at the same second as every other box on the internet:
  # RandomizedDelaySec spreads the API calls, and Persistent catches up after
  # the box has been off rather than skipping the window silently.
  cat >"${UNIT_DIR}/${UPDATE_UNIT}.timer" <<EOF
[Unit]
Description=Check for a new ssh-cv release daily

[Timer]
OnCalendar=daily
RandomizedDelaySec=4h
Persistent=yes

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable --now "${UPDATE_UNIT}.timer"
  log "timer enabled; next run: $(systemctl show -p NextElapseUSecRealtime --value "${UPDATE_UNIT}.timer" 2>/dev/null || echo unknown)"
}

# --- main --------------------------------------------------------------------

need_tools

arch="$(detect_arch)"

if [[ -n "${want_version}" ]]; then
  # Accept "1.4.0" as well as "v1.4.0"; the tag is the canonical form.
  case "${want_version}" in
    v*) : ;;
    *) want_version="v${want_version}" ;;
  esac
  tag="${TAG_PREFIX}${want_version}"
  target="${want_version}"
else
  tag="$(latest_tag)"
  [[ -n "${tag}" ]] || die "no ${TAG_PREFIX}v* release found in ${REPO}"
  target="${tag#"${TAG_PREFIX}"}"
fi

current="$(installed_version)"

log "installed: ${current:-none}   available: ${target}   arch: ${arch}"

if [[ "${check_only}" -eq 1 ]]; then
  if [[ "${current}" = "${target}" ]]; then
    log "up to date"
    exit 0
  fi
  # Deliberately loud even under --quiet: this is the whole output of a check
  # run that found something, and it is what lands in the journal.
  printf 'ssh-cv-install: update available: %s -> %s\n' "${current:-none}" "${target}"
  notify "Update available: ${current:-none} -> ${target}. Not installed (check mode)."
  exit 10
fi

if [[ "${current}" = "${target}" ]] && [[ "${force}" -eq 0 ]]; then
  log "already at ${target}; nothing to do"
  # Still honour --install-timer, so re-running the installer to add the
  # timer to an already-current box does what it looks like it does.
  if [[ "${install_timer}" -eq 1 ]]; then
    write_timer "${tag}"
  fi
  exit 0
fi

need_writable

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

asset="ssh-cv_linux_${arch}"
base="https://github.com/${REPO}/releases/download/${tag}"

log "downloading ${asset} from ${tag}"
curl -fsSL --max-time 300 "${base}/${asset}" -o "${tmpdir}/${asset}" ||
  die "could not download ${base}/${asset}"
curl -fsSL --max-time 60 "${base}/SHA256SUMS" -o "${tmpdir}/SHA256SUMS" ||
  die "could not download ${base}/SHA256SUMS"

# Only this architecture's line, or sha256sum fails on the assets that were
# never downloaded.
(
  cd "${tmpdir}"
  grep " ${asset}\$" SHA256SUMS >expected.sha256 ||
    die "SHA256SUMS in ${tag} has no line for ${asset}"
  sha256sum -c expected.sha256 >/dev/null ||
    die "checksum mismatch for ${asset}"
) || die "refusing to install ${asset} from ${tag}"
log "checksum ok"

chmod 755 "${tmpdir}/${asset}"

# The last check that can be made while this is still a temp file. A binary
# for the wrong architecture cannot execute at all, and a truncated one
# cannot answer, so both end here rather than at ExecStart.
reported="$("${tmpdir}/${asset}" --version 2>/dev/null || true)"
[[ "${reported}" = "${target}" ]] ||
  die "downloaded binary reports '${reported:-nothing}', expected '${target}'"
log "binary self-reports ${reported}"

# Whether the service was running decides whether it needs restarting, and it
# has to be asked before the binary moves under it.
was_active=0
if service_exists && systemctl is-active --quiet "${SERVICE}"; then
  was_active=1
fi

# Keep the outgoing binary. `cp` of a running executable is fine (reading is
# allowed); it is writing to one that gives ETXTBSY, which is exactly why the
# swap below is a rename and not a copy.
backup=""
if [[ -e "${BIN_PATH}" ]]; then
  backup="${BIN_PATH}.previous"
  cp -p "${BIN_PATH}" "${backup}"
fi

# Same directory, therefore same filesystem, therefore mv is a rename: the
# path never exists in a half-written state, and the running process keeps
# the old inode until it restarts.
staged="$(dirname "${BIN_PATH}")/.ssh-cv.new.$$"
mv "${tmpdir}/${asset}" "${staged}"
chown 0:0 "${staged}" 2>/dev/null || true
chmod 755 "${staged}"
mv "${staged}" "${BIN_PATH}"
log "installed ${target} to ${BIN_PATH}"

rollback() {
  [[ -n "${backup}" ]] || die "$1 (no previous binary to roll back to)"
  warn "$1 - rolling back to ${current:-the previous binary}"
  mv "${backup}" "${BIN_PATH}"
  if [[ "${was_active}" -eq 1 ]]; then
    systemctl restart "${SERVICE}" ||
      warn "rollback restart also failed - ${SERVICE} is down, look at it now"
  fi
  notify "Update to ${target} FAILED and was rolled back to ${current:-unknown}."
  exit 1
}

if [[ "${was_active}" -eq 1 ]]; then
  log "restarting ${SERVICE}"
  systemctl restart "${SERVICE}" || rollback "restart failed"
  # Restart=always means a crash-looping unit still reports activating for a
  # moment, so give it long enough to fail properly before believing it.
  sleep 3
  systemctl is-active --quiet "${SERVICE}" || rollback "${SERVICE} did not stay up"
  log "${SERVICE} is up"
elif service_exists; then
  log "${SERVICE} is installed but was not running; not starting it"
else
  log "no ${SERVICE}.service yet - install the unit (see docs/ssh-cv-deployment.md)"
fi

if [[ "${install_timer}" -eq 1 ]]; then
  write_timer "${tag}"
fi

log "done: ${current:-none} -> ${target}"
notify "Updated ${current:-none} -> ${target}."
