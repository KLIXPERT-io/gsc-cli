# install.ps1 — install the gsc CLI on Windows (PowerShell 5.1+).
#
# Usage:
#   irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/refs/heads/main/install.ps1 | iex
#
# Env vars:
#   $env:GSC_VERSION   pin a release tag (e.g. v1.2.3); default: latest
#   $env:INSTALL_DIR   target directory; default: $env:LOCALAPPDATA\Programs\gsc

$ErrorActionPreference = 'Stop'

$Repo = 'KLIXPERT-io/gsc-cli'
$Bin  = 'gsc'

# --- resolve version ---
$version = $env:GSC_VERSION
if (-not $version) {
    $latest  = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
    $version = $latest.tag_name
    if (-not $version) { throw "could not resolve latest release tag" }
}
$verNoV  = $version.TrimStart('v')
$archive = "${Bin}_${verNoV}_windows_amd64.zip"
$baseUrl = "https://github.com/$Repo/releases/download/$version"

# --- target directory ---
$targetDir = $env:INSTALL_DIR
if (-not $targetDir) {
    $targetDir = Join-Path $env:LOCALAPPDATA "Programs\gsc"
}
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

# --- download + verify + extract ---
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("gsc-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    Write-Host "Downloading $archive ..."
    Invoke-WebRequest -Uri "$baseUrl/$archive"      -OutFile (Join-Path $tmp $archive)      -UseBasicParsing
    Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    Write-Host "Verifying checksum ..."
    $expectedLine = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" }
    if (-not $expectedLine) { throw "no checksum entry for $archive" }
    $expected = ($expectedLine -split '\s+')[0].ToLower()
    $actual   = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash.ToLower()
    if ($expected -ne $actual) { throw "checksum mismatch for $archive" }

    Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    $exe = Join-Path $tmp "$Bin.exe"
    if (-not (Test-Path $exe)) { throw "binary $Bin.exe not found inside archive" }

    $installPath = Join-Path $targetDir "$Bin.exe"
    Move-Item -Force -Path $exe -Destination $installPath

    Write-Host ""
    Write-Host "Installed $version to $installPath"
    & $installPath --version

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not ($userPath -split ';' | Where-Object { $_ -ieq $targetDir })) {
        Write-Host ""
        Write-Host "Note: $targetDir is not in your user PATH. Add it with:"
        Write-Host "  setx PATH `"$targetDir;`$env:PATH`""
    }
}
finally {
    Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
}
