<#
.SYNOPSIS
  Put doti on a new Windows machine, then hand over to it.

.DESCRIPTION
  irm https://raw.githubusercontent.com/riptone/tone.rip/main/apps/doti/scripts/install.ps1 | iex

  This is the Windows half of the bootstrap, and it is deliberately the same
  six steps as install.sh:

    1. work out the architecture
    2. make sure git exists            (winget, which may prompt)
    3. resolve the newest `doti/v*` release
    4. download the binary, verify SHA256SUMS, then ask *the download* its
       own version
    5. install it somewhere on PATH
    6. run `doti install`

  IT MUST CONTAIN NO INSTALLER LOGIC. That is the whole point of the Go
  binary: the manifest parsing, the package lists, the linking and the health
  checks exist once, in one language. This script and install.sh are allowed
  to be two files only because they hold nothing that could drift - no
  manifest, no package names, no paths inside the repository. If a change
  here would need the same change in install.sh, it belongs in doti instead.

.PARAMETER Version
  Install a specific release (e.g. v1.0.0) rather than the newest.

.PARAMETER NoInstall
  Fetch and install the binary, but do not run `doti install`.

.PARAMETER BaseUrl
  Download the release assets from here instead of GitHub. For a mirror, and
  for the test that exercises the checksum verification against a local
  server - which is the only way to prove that step actually refuses a
  tampered binary rather than merely appearing to check.
#>
# Write-Host is the right call here and PSScriptAnalyzer's objection is dated:
# since PowerShell 5.0 it writes to the information stream, so the rule's own
# stated reason ("cannot be suppressed") no longer holds. What this script
# emits is progress for a human who typed `irm | iex` - it is not data, and
# putting it on the pipeline would mean `iex` echoing it back as output.
[Diagnostics.CodeAnalysis.SuppressMessageAttribute(
  'PSAvoidUsingWriteHost', '',
  Justification = 'Interactive installer progress belongs on the host stream, not the pipeline.')]
[CmdletBinding()]
param(
  [string]$Version,
  [switch]$NoInstall,
  [string]$BaseUrl
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Initialised because StrictMode makes *reading* an unset variable an error,
# and $LASTEXITCODE does not exist until a native command has run. Without
# this, a native command that fails to launch at all produced "The variable
# '$LASTEXITCODE' cannot be retrieved because it has not been set" instead of
# the message below it - the same trap that made the "found no release" error
# unreachable.
$LASTEXITCODE = 0

$Repo = if ($env:DOTI_REPO) { $env:DOTI_REPO } else { 'riptone/tone.rip' }
$TagPrefix = 'doti/v'

function Say  { param($m) Write-Host "==> $m" }
function Warn { param($m) Write-Host "!   $m" -ForegroundColor Yellow }

# --- 1. platform -------------------------------------------------------------
# Go's own names, because they are what the release assets are called.
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)" }
}
Say "platform: windows/$arch"

# Symlinks are what doti creates, and Windows refuses them to an unprivileged
# process unless Developer Mode is on. Said now rather than discovered halfway
# through linking.
$devMode = $false
try {
  $key = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock'
  $devMode = (Get-ItemProperty -Path $key -Name AllowDevelopmentWithoutDevLicense -ErrorAction Stop).AllowDevelopmentWithoutDevLicense -eq 1
} catch { $devMode = $false }
if (-not $devMode) {
  Warn 'Developer Mode is off, so symlinks will need an elevated shell.'
  Warn 'Settings > Privacy & security > For developers > Developer Mode.'
}

# --- 2. git ------------------------------------------------------------------
# doti clones the configs itself but will not install git: that needs a
# package manager and possibly elevation, and a binary which escalates on its
# own is a worse trade than one sentence of instruction.
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  Say 'installing git'
  if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    throw 'winget is not available - install Git for Windows manually, then re-run'
  }
  winget install --id Git.Git --accept-package-agreements --accept-source-agreements
  if ($LASTEXITCODE -ne 0) { throw 'git install failed' }
}

# --- 3. resolve the release --------------------------------------------------
# Tags are namespaced by app, which is what makes "the newest doti release" a
# different question from "the newest release" in a repo that also releases
# ssh-cv.
if (-not $Version) {
  Say "resolving the newest $TagPrefix* release"
  $releases = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=100" -Headers @{ 'User-Agent' = 'doti-installer' }
  # Bound to a variable before the property is read, rather than
  # `(...).tag_name` inline. Under Set-StrictMode -Version Latest,
  # dereferencing a property on $null throws - so with no matching
  # release the inline form died with "The property 'tag_name' cannot be
  # found on this object" and the message below was unreachable. That is
  # precisely the case somebody hits before the first doti/v* tag exists.
  $match = $releases | Where-Object { $_.tag_name -like "$TagPrefix*" } | Select-Object -First 1
  if (-not $match) { throw "found no $TagPrefix* release in $Repo" }
  $Version = $match.tag_name -replace [regex]::Escape($TagPrefix), 'v'
}
$tag = "$TagPrefix$($Version -replace '^v','')"
Say "installing doti $Version"

# --- 4. download and verify --------------------------------------------------
$asset = "doti_windows_$arch.exe"
$base = if ($BaseUrl) { $BaseUrl } else { "https://github.com/$Repo/releases/download/$tag" }
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("doti-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  Say "downloading $asset"
  Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
  Invoke-WebRequest -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')

  Say 'verifying the checksum'
  # Only this asset's line: the file covers every platform, and every other
  # line names a file that was never downloaded.
  $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
  if (-not $line) { throw "SHA256SUMS has no entry for $asset" }
  $expected = ($line -split '\s+')[0]
  $actual = (Get-FileHash -Path (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
  if ($actual -ne $expected.ToLower()) {
    throw "checksum mismatch for $asset - refusing to install"
  }

  # Ask the downloaded binary its own version before it goes anywhere near
  # PATH, so a truncated file or the wrong architecture fails while it is
  # still a temp file.
  #
  # try/catch as well as the exit code: a binary for the wrong architecture
  # does not fail, it fails to *launch*, which is a terminating error rather
  # than a non-zero exit.
  $reported = $null
  try {
    $reported = & (Join-Path $tmp $asset) version
  } catch {
    throw "the downloaded binary does not run: $($_.Exception.Message)"
  }
  if ($LASTEXITCODE -ne 0 -or -not $reported) {
    throw 'the downloaded binary does not report a version'
  }
  Say "downloaded binary reports $reported"

  # --- 5. install onto PATH -------------------------------------------------
  $bindir = if ($env:DOTI_BIN_DIR) { $env:DOTI_BIN_DIR } else { Join-Path $env:USERPROFILE '.local\bin' }
  New-Item -ItemType Directory -Path $bindir -Force | Out-Null
  $target = Join-Path $bindir 'doti.exe'
  Copy-Item -Path (Join-Path $tmp $asset) -Destination $target -Force
  Say "installed $target"

  # Persist it for future sessions, and add it to this one so step 6 works.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$bindir*") {
    [Environment]::SetEnvironmentVariable('Path', "$bindir;$userPath", 'User')
    Say "added $bindir to your user PATH"
  }
  if ($env:Path -notlike "*$bindir*") { $env:Path = "$bindir;$env:Path" }

  # --- 6. hand over ---------------------------------------------------------
  if ($NoInstall) {
    Say 'done - run `doti` for the menu, or `doti install` for everything'
    return
  }
  # Bare `doti` when there is a console: on a machine with no checkout the
  # window opens on the install screen and waits for enter, so the last thing
  # this script does is offer a choice rather than start a clone nobody named.
  # `irm | iex` is consent to install the binary, not to link over $HOME
  # unseen. Without a console there is nobody to ask, and `install` is the
  # thing they came for.
  if ([Environment]::UserInteractive) {
    Say 'opening doti'
    & $target
  } else {
    Say 'running doti install'
    & $target install
  }
  exit $LASTEXITCODE
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
