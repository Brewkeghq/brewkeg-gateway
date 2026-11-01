# Brewkeg CLI + Gateway installer for Windows.
#  irm https://brewkeg.dev/install-app.ps1 | iex
#
# Both, not just the app: install.sh installs the CLI and the app on macOS and
# Linux, and this is the only installer Windows has, so it has to do the same
# two things or the one-liner means something different per platform.
#
# Why a script instead of a download link: the browser sets Zone.Identifier on
# everything it fetches and SmartScreen then blocks the .exe with "Windows
# protected your PC". Invoke-WebRequest does not, so the app opens with no
# prompt and no code-signing certificate.

$ErrorActionPreference = 'Stop'

$Repo = if ($env:BREWKEG_REPO) { $env:BREWKEG_REPO } else { 'brewkeghq/brewkeg-gateway' }
$AppName = 'brewkeg-gateway'

function Say($m) { Write-Host $m }
function Die($m) { Write-Host "error: $m" -ForegroundColor Red; exit 1 }

if (-not ([System.Environment]::Is64BitOperatingSystem)) {
  Die '32-bit Windows is not supported — no 32-bit build is published'
}

try {
  $rel = Invoke-RestMethod -Headers @{ 'User-Agent' = 'brewkeg-installer' } `
                           -Uri "https://api.github.com/repos/$Repo/releases/latest"
} catch {
  Die "could not read the latest release from GitHub: $($_.Exception.Message)"
}

$tag = $rel.tag_name
if (-not $tag) { Die 'the release carries no tag' }
$asset = $rel.assets | Where-Object { $_.name -eq "${AppName}_${tag}_windows.zip" } | Select-Object -First 1
if (-not $asset) { Die "no Windows asset on release $tag" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("brewkeg-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
$zip = Join-Path $tmp "$AppName.zip"

Say "→ downloading Brewkeg Gateway $tag for windows/amd64"
try {
  # -OutFile on IWR does not set Zone.Identifier, which is the whole point here.
  Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip
} catch {
  Die "download failed: $($_.Exception.Message)"
}

Say '→ unpacking'
Expand-Archive -Path $zip -DestinationPath $tmp -Force
$exe = Join-Path $tmp "$AppName.exe"
if (-not (Test-Path $exe)) { Die "unexpected archive contents: no $AppName.exe found" }

# Per-user install. Writing to Program Files needs elevation, and this app has
# no business demanding admin to paste an API key.
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\brewkeg-gateway'
$target = Join-Path $installDir "$AppName.exe"

Say "→ installing to $installDir"
New-Item -ItemType Directory -Path $installDir -Force | Out-Null
if (Test-Path $target) {
  Say '→ replacing the existing copy'
  Remove-Item $target -Force
}
Move-Item $exe $target

# Clear Zone.Identifier explicitly too: a machine that already had the app, or
# a re-zip of an older download, can carry it in from somewhere else.
Unblock-File $target -ErrorAction SilentlyContinue

# The CLI. Matched by pattern rather than an exact name because the asset
# carries the GoReleaser version (`brewkeg_0.3.1_windows_amd64.zip`) while the
# tag it came from is `v0.3.1` — plain semver, never a `cli-` prefix.
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$cliAsset = $rel.assets |
  Where-Object { $_.name -match "^brewkeg_.*_windows_$arch\.zip$" } |
  Select-Object -First 1

if (-not $cliAsset) {
  Say "→ no CLI build for windows/$arch on $tag, installing the app only"
} else {
  $cliZip = Join-Path $tmp 'brewkeg-cli.zip'
  Say "→ downloading brewkeg CLI $tag for windows/$arch"
  try {
    Invoke-WebRequest -Uri $cliAsset.browser_download_url -OutFile $cliZip
  } catch {
    Die "CLI download failed: $($_.Exception.Message)"
  }

  $cliTmp = Join-Path $tmp 'cli'
  Expand-Archive -Path $cliZip -DestinationPath $cliTmp -Force
  $cliExe = Join-Path $cliTmp 'brewkeg.exe'
  if (-not (Test-Path $cliExe)) { Die 'unexpected CLI archive contents: no brewkeg.exe' }

  # Per-user, same reason as the app: no elevation.
  $cliDir = Join-Path $env:LOCALAPPDATA 'Programs\brewkeg'
  New-Item -ItemType Directory -Path $cliDir -Force | Out-Null
  Copy-Item $cliExe (Join-Path $cliDir 'brewkeg.exe') -Force
  Unblock-File (Join-Path $cliDir 'brewkeg.exe') -ErrorAction SilentlyContinue

  # Append to the USER Path only. The machine Path needs admin, and prepending
  # would let a stale copy shadow a newer one someone installed deliberately.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $cliDir) {
    $joined = (($userPath, $cliDir) -join ';').TrimStart(';')
    [Environment]::SetEnvironmentVariable('Path', $joined, 'User')
    $env:Path = "$env:Path;$cliDir"
    Say "→ added $cliDir to your PATH (open a new terminal to use brewkeg)"
  }
  Say "→ installed brewkeg CLI to $cliDir"
}

Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue

Say '→ starting Brewkeg Gateway'
Start-Process $target

$base = if ($env:BREWKEG_BASE_URL) { $env:BREWKEG_BASE_URL } else { 'https://brewkeg.dev' }
Say ''
Say "  Installed Brewkeg Gateway $tag"
if ($cliDir) { Say "  Installed brewkeg CLI to $cliDir" }
Say "  Paste your key at $base/dashboard/keys"
Say ''
Say '  Windows may ask permission to edit files in your profile folder the first'
Say '  time you flip a switch. That is the app doing its job.'
Say ''
Say "  To uninstall: Remove-Item -Recurse -Force `"$installDir`""
if ($cliDir) { Say "               Remove-Item -Recurse -Force `"$cliDir`"" }