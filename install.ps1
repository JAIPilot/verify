param(
  [string]$Version = $env:VERIFY_VERSION,
  [string]$InstallDir = $(if ($env:VERIFY_INSTALL_DIR) { $env:VERIFY_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'JAIPilot\bin' })
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$repository = 'JAIPilot/verify'

$machineArchitecture = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$architecture = switch ($machineArchitecture) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "Unsupported Windows architecture: $machineArchitecture" }
}
if (-not $Version) {
  $Version = (Invoke-RestMethod "https://api.github.com/repos/$repository/releases/latest").tag_name
}
if (-not $Version.StartsWith('v')) { $Version = "v$Version" }
if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'Could not determine a stable Verify release.' }

$asset = "verify_${Version}_windows_${architecture}.zip"
$base = "https://github.com/$repository/releases/download/$Version"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$temporary = Join-Path $InstallDir ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
  $archive = Join-Path $temporary $asset
  $checksums = Join-Path $temporary 'checksums.txt'
  Write-Host "Installing Verify $Version for windows/$architecture..."
  Invoke-WebRequest "$base/$asset" -OutFile $archive
  Invoke-WebRequest "$base/checksums.txt" -OutFile $checksums

  $matchingLines = @(Get-Content $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+$([regex]::Escape($asset))$" })
  if ($matchingLines.Count -ne 1) { throw 'Release checksum is missing or invalid. Existing installation unchanged.' }
  $expected = ($matchingLines[0] -split '\s+')[0]
  $actual = (Get-FileHash $archive -Algorithm SHA256).Hash
  if ($actual -ine $expected) { throw 'Checksum mismatch. Existing installation unchanged.' }

  Expand-Archive -Path $archive -DestinationPath $temporary -Force
  $binary = Join-Path $temporary 'verify.exe'
  $reportedVersion = (& $binary --version | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or $reportedVersion -ne $Version.Substring(1)) {
    throw 'Downloaded executable failed its version check. Existing installation unchanged.'
  }
  Move-Item -Force $binary (Join-Path $InstallDir 'verify.exe')
} finally {
  Remove-Item -Recurse -Force $temporary
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $InstallDir) {
  [Environment]::SetEnvironmentVariable('Path', "$InstallDir;$userPath", 'User')
}
if (($env:Path -split ';') -notcontains $InstallDir) { $env:Path = "$InstallDir;$env:Path" }
Write-Host "Installed Verify $Version. Open a new terminal and run verify in a Git repository with a change. Your first run opens JAIPilot sign-in."
