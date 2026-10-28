<#
  Reproducible Windows release build.
  Usage:  scripts\build.ps1 [-Version 1.0.0] [-SkipTests] [-SkipInstaller]
  Output: release\  (installer, portable zip, SHA256SUMS.txt); intermediates in dist\
  Requires: Go (see go.mod), Node.js + npm, Inno Setup 6 (for the installer).
  The Go build is pure Go (CGO_ENABLED=0): no C compiler is needed.
#>
param(
    [string]$Version = '',
    [switch]$SkipTests,
    [switch]$SkipInstaller
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Find-Tool([string]$name, [string[]]$extra) {
    $c = Get-Command $name -ErrorAction SilentlyContinue
    if ($c) { return $c.Source }
    foreach ($p in $extra) { if (Test-Path $p) { return $p } }
    return $null
}
function Step([string]$m) { Write-Host "`n==> $m" -ForegroundColor Cyan }
function Run([scriptblock]$b) { & $b; if ($LASTEXITCODE -ne 0) { throw "command failed with exit code $LASTEXITCODE" } }

Step 'Verify toolchain'
$go = Find-Tool 'go' @('C:\Program Files\Go\bin\go.exe')
$npm = Find-Tool 'npm' @()
if (-not $go) { throw 'Go not found (https://go.dev/dl/)' }
if (-not $npm) { throw 'Node.js/npm not found (https://nodejs.org/)' }
$iscc = Find-Tool 'ISCC' @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe', 'C:\Program Files\Inno Setup 6\ISCC.exe')
if (-not $iscc -and -not $SkipInstaller) { throw 'Inno Setup 6 not found (winget install JRSoftware.InnoSetup); use -SkipInstaller to skip' }
& $go version
if (-not $Version) {
    $Version = (git describe --tags --abbrev=0 2>$null)
    if (-not $Version) { $Version = '0.0.0-dev' }
}
$Version = $Version.TrimStart('v')
Write-Host "Version: $Version"

Step 'Frontend: install, lint, test, build'
Push-Location web
try {
    Run { & $npm ci --no-audit --no-fund }
    Run { & $npm run lint }
    if (-not $SkipTests) { Run { & $npm test } }
    Run { & $npm run build }
} finally { Pop-Location }
if (-not (Test-Path web\dist\index.html)) { throw 'frontend build produced no index.html' }

if (-not $SkipTests) {
    Step 'Go: format check, vet, tests'
    $unformatted = & (Join-Path (Split-Path $go) 'gofmt.exe') -l cmd internal web
    if ($unformatted) { throw "gofmt needed: $unformatted" }
    Run { & $go vet ./... }
    if (Find-Tool 'gcc' @()) { $env:CGO_ENABLED = '1'; Run { & $go test -race -count=1 ./... } }
    else { Write-Warning 'gcc not found: running tests WITHOUT the race detector'; Run { & $go test -count=1 ./... } }
}

Step 'Go: build windows/amd64 binaries (pure Go, embedded UI)'
$env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
New-Item -ItemType Directory -Force dist, release | Out-Null
$ld = "-s -w -X main.version=$Version"
Run { & $go build -trimpath -ldflags $ld -o dist\homeserver.exe .\cmd\homeserver }
Run { & $go build -trimpath -ldflags "$ld -H=windowsgui" -o dist\homeserver-tray.exe .\cmd\homeserver-tray }
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue

Step 'Smoke test the built server'
& "$PSScriptRoot\smoke.ps1" -Exe dist\homeserver.exe

Step 'Package'
$zip = "release\HomeServer-$Version-windows-amd64.zip"
$stage = Join-Path $root 'dist\portable'
New-Item -ItemType Directory -Force $stage | Out-Null
Copy-Item dist\homeserver.exe, dist\homeserver-tray.exe, README.md, LICENSE $stage -Force
Compress-Archive -Path "$stage\*" -DestinationPath $zip -Force
if (-not $SkipInstaller) {
    Run { & $iscc "/DAppVersion=$Version" installer\homeserver.iss }
}

Step 'Checksums'
$sums = Get-ChildItem release -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' } | ForEach-Object {
    "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
Set-Content release\SHA256SUMS.txt -Value $sums -Encoding ascii
$sums
Write-Host "`nRelease artifacts are in $root\release" -ForegroundColor Green
